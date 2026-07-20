//go:build darwin

package askpass

import (
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// verifyPeer authenticates an incoming connection using LOCAL_PEERCRED, which
// reports the UID of the connecting process. On macOS this is all the kernel
// exposes via this socket option (no peer PID), so the descendant check that
// closes the same-UID attack on Linux is not possible without cgo.
//
// The same-UID threat on macOS is therefore handled by the per-session token
// (checked separately in handleConn), matching the posture of ssh-agent and
// gpg-agent on this platform. Cross-user protection is enforced here and is
// also backed by the 0600 socket permissions.
func verifyPeer(conn net.Conn) (pid int, uid int, ok bool) {
	uc, alright := conn.(*net.UnixConn)
	if !alright {
		return 0, 0, false
	}
	var peerUid uint32
	rawConn, err := uc.SyscallConn()
	if err != nil {
		return 0, 0, false
	}
	ctrlErr := rawConn.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if e != nil || cred == nil {
			return
		}
		peerUid = cred.Uid
	})
	if ctrlErr != nil {
		return 0, int(peerUid), false
	}
	if peerUid != uint32(os.Getuid()) {
		return 0, int(peerUid), false
	}
	return 0, int(peerUid), true
}
