package askpass

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
)

func startTestServer(t *testing.T, cache *PasswordCache) (*Server, func()) {
	t.Helper()
	srv, cleanup, err := NewServer(cache)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Serve(ctx)
	origCleanup := cleanup
	return srv, func() {
		cancel()
		origCleanup()
	}
}

// dialRaw sends the raw three-line protocol and returns the response.
func dialRaw(t *testing.T, socketPath, host, commandID, token string) (string, error) {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(host + "\n" + commandID + "\n" + token + "\n")); err != nil {
		return "", err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	return strings.TrimRight(line, "\n"), err
}

func TestServer_TokenGenerated(t *testing.T) {
	srv, cleanup := startTestServer(t, NewPasswordCache())
	defer cleanup()

	tok := srv.Token()
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
	if len(tok) != 64 {
		t.Fatalf("expected 64-char hex token (32 bytes), got %d chars", len(tok))
	}
}

func TestServer_WrongTokenRejected(t *testing.T) {
	cache := NewPasswordCache()
	cache.Set("user@host", "secret")
	srv, cleanup := startTestServer(t, cache)
	defer cleanup()

	resp, err := dialRaw(t, srv.SocketPath(), "user@host", "remote-build", "wrong-token")
	if err == nil && resp == "secret" {
		t.Fatal("server leaked password despite wrong token")
	}
}

func TestServer_CorrectTokenCacheHit(t *testing.T) {
	cache := NewPasswordCache()
	cache.Set("user@host", "secret")
	srv, cleanup := startTestServer(t, cache)
	defer cleanup()

	resp, err := dialRaw(t, srv.SocketPath(), "user@host", "remote-build", srv.Token())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "secret" {
		t.Fatalf("expected cached password, got %q", resp)
	}
}

func TestServer_GetPasswordEndToEnd(t *testing.T) {
	cache := NewPasswordCache()
	cache.Set("user@host", "secret")
	srv, cleanup := startTestServer(t, cache)
	defer cleanup()

	pw, err := GetPassword(srv.SocketPath(), "user@host", "remote-build", srv.Token())
	if err != nil {
		t.Fatalf("GetPassword: %v", err)
	}
	if pw != "secret" {
		t.Fatalf("expected 'secret', got %q", pw)
	}
}
