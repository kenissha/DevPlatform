package iishelper

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

var errExecFailed = errors.New("test: simulated execution failure")

// listen opens a loopback TCP listener for the test — the Server's own
// logic (validate, then execute, then respond) doesn't depend on named
// pipes at all, so a plain TCP listener exercises exactly the same code
// path without requiring a real Windows named pipe in tests.
func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open test listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func roundTrip(t *testing.T, addr string, req Request) Response {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to dial test server: %v", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	return resp
}

func TestServer_ExecutesAValidatedRequestAndReturnsItsOutput(t *testing.T) {
	ln := listen(t)
	var gotName string
	var gotArgs []string
	srv := &Server{
		AppcmdPath:   testAppcmdPath,
		AllowedSites: testAllowedSites(),
		Execute: func(name string, args ...string) ([]byte, error) {
			gotName, gotArgs = name, args
			return []byte("ok"), nil
		},
	}
	go srv.Serve(ln)

	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	resp := roundTrip(t, ln.Addr().String(), req)

	if resp.Error != "" {
		t.Fatalf("expected no error, got: %s", resp.Error)
	}
	if string(resp.Output) != "ok" {
		t.Fatalf("expected output %q, got %q", "ok", resp.Output)
	}
	if gotName != testAppcmdPath || len(gotArgs) != 4 {
		t.Fatalf("Execute was not called with the validated request: name=%q args=%v", gotName, gotArgs)
	}
}

func TestServer_RejectsAnInvalidRequestWithoutCallingExecute(t *testing.T) {
	ln := listen(t)
	executed := false
	srv := &Server{
		AppcmdPath:   testAppcmdPath,
		AllowedSites: testAllowedSites(),
		Execute: func(name string, args ...string) ([]byte, error) {
			executed = true
			return nil, nil
		},
	}
	go srv.Serve(ln)

	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "Some Unlisted Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	resp := roundTrip(t, ln.Addr().String(), req)

	if resp.Error == "" {
		t.Fatal("expected a non-empty error for an invalid request")
	}
	if executed {
		t.Fatal("Execute must never be called for a request that fails validation")
	}
}

func TestServer_ReturnsOutputAlongsideAnExecutionError(t *testing.T) {
	ln := listen(t)
	srv := &Server{
		AppcmdPath:   testAppcmdPath,
		AllowedSites: testAllowedSites(),
		Execute: func(name string, args ...string) ([]byte, error) {
			return []byte("appcmd exited 5: access denied"), errExecFailed
		},
	}
	go srv.Serve(ln)

	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	resp := roundTrip(t, ln.Addr().String(), req)

	if resp.Error == "" {
		t.Fatal("expected the execution error to be reported")
	}
	if string(resp.Output) != "appcmd exited 5: access denied" {
		t.Fatalf("expected the execution output to be preserved for diagnostics, got %q", resp.Output)
	}
}

func TestServer_HandlesMultipleSequentialConnections(t *testing.T) {
	ln := listen(t)
	calls := 0
	srv := &Server{
		AppcmdPath:   testAppcmdPath,
		AllowedSites: testAllowedSites(),
		Execute: func(name string, args ...string) ([]byte, error) {
			calls++
			return []byte("ok"), nil
		},
	}
	go srv.Serve(ln)

	req := Request{
		Name: testAppcmdPath,
		Args: []string{"set", "vdir", "DevPlatform Test Site/", `/physicalPath:C:\inetpub\devplatform-test\releases\5`},
	}
	roundTrip(t, ln.Addr().String(), req)
	roundTrip(t, ln.Addr().String(), req)

	if calls != 2 {
		t.Fatalf("expected 2 executions across 2 connections, got %d", calls)
	}
}
