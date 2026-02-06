package microvm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"time"
)

var (
	spawnedRegex = regexp.MustCompile(`spawned: '([^']+)'`)
	runningRegex = regexp.MustCompile(`success: ([^ ]+) entered RUNNING state`)
	exitedRegex  = regexp.MustCompile(`exited: ([^ ]+) \((exit status [^;]+)`)
	gaveUpRegex  = regexp.MustCompile(`gave up: ([^ ]+) entered FATAL state`)
)

// VirtiofsdRunner manages the lifecycle of virtiofsd-run and monitors
// supervisord log output to track process startup.
type VirtiofsdRunner interface {
	// Start launches virtiofsd-run and begins log streaming.
	// Returns immediately after process starts.
	Start(ctx context.Context) error

	// Ready returns a channel that either:
	//   - receives an error if startup failed
	//   - is closed when all processes are running
	Ready() <-chan error

	// Running returns a channel that receives process names
	// as each enters RUNNING state. Closed when Ready() would close.
	Running() <-chan string

	// Stop terminates the process and cleans up resources.
	Stop() error
}

type virtiofsdRunner struct {
	virtiofsdRunPath string
	tempDir          string
	logPath          string
	readyTimeout     time.Duration

	cmd        *exec.Cmd
	logFile    *os.File
	logWriter  *io.PipeWriter
	logReader  *io.PipeReader
	readyChan  chan error
	runningChan chan string
}

// NewVirtiofsdRunner creates a new VirtiofsdRunner.
func NewVirtiofsdRunner(virtiofsdRunPath, tempDir, logPath string) VirtiofsdRunner {
	return &virtiofsdRunner{
		virtiofsdRunPath: virtiofsdRunPath,
		tempDir:          tempDir,
		logPath:          logPath,
		readyTimeout:     30 * time.Second,
		readyChan:        make(chan error, 1),
		runningChan:      make(chan string, 10),
	}
}

// Start launches virtiofsd-run and begins monitoring the log.
func (r *virtiofsdRunner) Start(ctx context.Context) error {
	// Open log file
	logFile, err := os.Create(r.logPath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	r.logFile = logFile

	// Create pipe for log streaming
	r.logReader, r.logWriter = io.Pipe()

	// MultiWriter sends output to both file and pipe
	logOutput := io.MultiWriter(logFile, r.logWriter)

	// Start virtiofsd-run
	r.cmd = exec.CommandContext(ctx, r.virtiofsdRunPath)
	r.cmd.Dir = r.tempDir
	r.cmd.Stdout = logOutput
	r.cmd.Stderr = logOutput

	if err := r.cmd.Start(); err != nil {
		r.logWriter.Close()
		logFile.Close()
		return fmt.Errorf("failed to start virtiofsd-run: %w", err)
	}

	// Start goroutine to wait for process exit and close pipe
	go func() {
		r.cmd.Wait()
		r.logWriter.Close()
	}()

	// Start log monitoring goroutine
	go r.monitorLog(ctx)

	return nil
}

// Ready returns a channel that signals when all processes are running or an error occurs.
func (r *virtiofsdRunner) Ready() <-chan error {
	return r.readyChan
}

// Running returns a channel that receives process names as they enter RUNNING state.
func (r *virtiofsdRunner) Running() <-chan string {
	return r.runningChan
}

// Stop terminates the process and cleans up resources.
func (r *virtiofsdRunner) Stop() error {
	if r.cmd != nil && r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
	if r.logWriter != nil {
		r.logWriter.Close()
	}
	if r.logReader != nil {
		r.logReader.Close()
	}
	if r.logFile != nil {
		r.logFile.Close()
	}
	return nil
}

// monitorLog parses the supervisord log and tracks process states.
func (r *virtiofsdRunner) monitorLog(ctx context.Context) {
	defer close(r.readyChan)
	defer close(r.runningChan)

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, r.readyTimeout)
	defer cancel()

	spawned := make(map[string]bool)
	running := make(map[string]bool)

	scanner := bufio.NewScanner(r.logReader)

	for scanner.Scan() {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			r.readyChan <- fmt.Errorf("timeout waiting for supervisord processes: %w", ctx.Err())
			return
		default:
		}

		line := scanner.Text()

		// Check for gave up (fatal state)
		if match := gaveUpRegex.FindStringSubmatch(line); match != nil {
			procName := match[1]
			r.readyChan <- fmt.Errorf("process '%s' failed: too many start retries", procName)
			return
		}

		// Check for spawned process
		if match := spawnedRegex.FindStringSubmatch(line); match != nil {
			procName := match[1]
			spawned[procName] = true
		}

		// Check for running process
		if match := runningRegex.FindStringSubmatch(line); match != nil {
			procName := match[1]
			running[procName] = true

			// Send to running channel (non-blocking)
			select {
			case r.runningChan <- procName:
			default:
			}

			// Check if all spawned processes are running
			if len(spawned) > 0 && len(running) == len(spawned) {
				// Signal ready by closing channel
				return
			}
		}

		// Check for exited process (error condition)
		if match := exitedRegex.FindStringSubmatch(line); match != nil {
			procName := match[1]
			exitStatus := match[2]
			r.readyChan <- fmt.Errorf("process '%s' failed: %s", procName, exitStatus)
			return
		}
	}

	if err := scanner.Err(); err != nil {
		r.readyChan <- fmt.Errorf("error reading log: %w", err)
		return
	}

	r.readyChan <- fmt.Errorf("log stream closed unexpectedly")
}
