//go:build linux

package askpass

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// verifyPeer authenticates an incoming connection using SO_PEERCRED, which
// authoritatively reports the PID and UID of the process that called connect()
// on the socket. The check is two-fold:
//
//  1. The peer UID must match ours (cross-user protection, redundant with the
//     0600 socket permissions but more reliable).
//  2. The peer PID must be a descendant of this process. This is what closes
//     the same-UID attack vector: an unrelated process running as our user
//     cannot satisfy it, while every nix/ssh child nilla-os spawns can.
func verifyPeer(conn net.Conn) (pid int, uid int, ok bool) {
	uc, alright := conn.(*net.UnixConn)
	if !alright {
		return 0, 0, false
	}
	var peerPid, peerUid int
	rawConn, err := uc.SyscallConn()
	if err != nil {
		return 0, 0, false
	}
	ctrlErr := rawConn.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil {
			return
		}
		peerPid = int(cred.Pid)
		peerUid = int(cred.Uid)
	})
	if ctrlErr != nil {
		return peerPid, peerUid, false
	}
	if peerUid != os.Getuid() {
		return peerPid, peerUid, false
	}
	if !isDescendant(peerPid, os.Getpid()) {
		return peerPid, peerUid, false
	}
	return peerPid, peerUid, true
}

// isDescendant reports whether peerPID's process-tree ancestry includes
// ourPID, by walking /proc/<pid>/status PPid lines. A peer equal to ourPID
// is accepted (the server process itself, e.g. in tests).
//
// There is an inherent PID-reuse race between accept and the /proc walk, but
// the window is microseconds and the attacker would need to race a dying
// legitimate child of ours; this is acceptable for the threat model.
func isDescendant(peerPID, ourPID int) bool {
	if peerPID <= 0 {
		return false
	}
	if peerPID == ourPID {
		return true
	}
	cur := peerPID
	for i := 0; i < 1024 && cur > 1; i++ {
		ppid, err := readPPid(cur)
		if err != nil {
			return false
		}
		if ppid == ourPID {
			return true
		}
		cur = ppid
	}
	return false
}

func readPPid(pid int) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strconv.Atoi(fields[1])
			}
		}
	}
	return 0, fmt.Errorf("PPid not found for pid %d", pid)
}
