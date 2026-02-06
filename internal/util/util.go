package util

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

const (
	_ = 1 << (10 * iota)
	KiB
	MiB
	GiB
	TiB
)

const (
	BytesUnitBytes = "B"
	BytesUnitKiB   = "KiB"
	BytesUnitMiB   = "MiB"
	BytesUnitGiB   = "GiB"
	BytesUnitTiB   = "TiB"
)

func ConvertBytes(b int64) (float64, string) {
	if b >= TiB {
		return float64(b) / TiB, BytesUnitTiB
	} else if b >= GiB {
		return float64(b) / GiB, BytesUnitGiB
	} else if b >= MiB {
		return float64(b) / MiB, BytesUnitMiB
	} else if b >= KiB {
		return float64(b) / KiB, BytesUnitKiB
	}
	return float64(b), BytesUnitBytes
}

func ConvertBytesToUnit(b int64, unit string) float64 {
	switch unit {
	case BytesUnitTiB:
		return float64(b) / TiB
	case BytesUnitGiB:
		return float64(b) / GiB
	case BytesUnitMiB:
		return float64(b) / MiB
	case BytesUnitKiB:
		return float64(b) / KiB
	default:
		return float64(b)
	}
}

func DiffBytes(from, to int64) (float64, bool, string) {
	diff := to - from

	real := diff
	if real < 0 {
		real = -real
	}

	b, unit := ConvertBytes(real)

	return b, diff < 0, unit
}

func GetUser() string {
	if user, err := user.Current(); err == nil {
		return user.Username
	}
	return os.Getenv("USER")
}

func GetHomeDir() string {
	if user, err := user.Current(); err == nil && user.HomeDir != "" {
		return user.HomeDir
	}
	return os.Getenv("HOME")
}

func IsRoot() bool {
	user, err := user.Current()
	if err != nil {
		return false
	}

	uid, err := strconv.Atoi(user.Uid)
	if err != nil {
		return false
	}

	return uid == 0
}

// IsInGroup checks if the current user is a member of the specified group.
func IsInGroup(groupName string) bool {
	currentUser, err := user.Current()
	if err != nil {
		return false
	}

	// Check primary group
	group, err := user.LookupGroupId(currentUser.Gid)
	if err == nil && group.Name == groupName {
		return true
	}

	// Check supplementary groups
	groupIds, err := currentUser.GroupIds()
	if err != nil {
		return false
	}

	for _, id := range groupIds {
		if id == currentUser.Gid {
			continue // Already checked primary group
		}
		g, err := user.LookupGroupId(id)
		if err == nil && g.Name == groupName {
			return true
		}
	}

	return false
}

func SelfElevate() error {
	args := append([]string{"sudo"}, os.Args...)

	spath, err := exec.LookPath("sudo")
	if err != nil {
		return err
	}

	return syscall.Exec(spath, args, os.Environ())
}

var verbosityLevel int

func InitLogger(verboseCount int) {
	verbosityLevel = verboseCount
	// Disable timestamp
	log.SetReportTimestamp(false)

	// Default to info
	log.SetLevel(log.InfoLevel)
	if verboseCount > 0 {
		log.SetLevel(log.DebugLevel)
	}

	// Set styles
	styles := log.DefaultStyles()
	styles.Levels[log.ErrorLevel] = levelStyle(log.ErrorLevel, lipgloss.Color("1"))
	styles.Levels[log.DebugLevel] = levelStyle(log.DebugLevel, lipgloss.Color("5"))
	styles.Levels[log.WarnLevel] = levelStyle(log.WarnLevel, lipgloss.Color("3"))
	styles.Levels[log.InfoLevel] = levelStyle(log.InfoLevel, lipgloss.Color("4"))
	log.SetStyles(styles)
}

// IsSSHDebugEnabled returns true if verbosity is 2 or higher (-vv)
func IsSSHDebugEnabled() bool {
	return verbosityLevel >= 2
}

func levelStyle(level log.Level, color lipgloss.TerminalColor) lipgloss.Style {
	return lipgloss.NewStyle().
		SetString(strings.ToUpper(level.String())).
		Bold(true).
		MaxWidth(4).
		Foreground(color)
}

// ParseTarget parses a target string in the format "user@host" or "host" and returns
// the user and hostname. If no user is specified, it returns an empty string for user.
func ParseTarget(target string) (user, hostname string) {
	parts := strings.Split(target, "@")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", target
}

// BuildStoreAddress constructs an ssh-ng:// store address from user and hostname.
// If user is empty, it uses the current local user as default (matching SSH executor behavior).
func BuildStoreAddress(user, hostname string) string {
	if user == "" {
		user = GetUser()
	}
	return fmt.Sprintf("ssh-ng://%s@%s", user, hostname)
}
