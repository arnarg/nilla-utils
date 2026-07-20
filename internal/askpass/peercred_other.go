//go:build !linux && !darwin

package askpass

import "net"

// verifyPeer is a no-op on unsupported platforms. Peer credential verification
// via socket options is not available, so we rely on the per-session token
// (checked separately in handleConn) plus the 0600 filesystem permissions on
// the socket for protection.
func verifyPeer(conn net.Conn) (pid int, uid int, ok bool) {
	return 0, 0, true
}
