package iishelper

import (
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

// fakeHelperServer accepts exactly one connection, decodes one Request,
// and replies with a canned Response — enough to exercise
// HelperCommandRunner's wire format without a real named pipe.
func fakeHelperServer(t *testing.T, resp Response) (addr string, gotReq chan Request) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open fake server listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	gotReq = make(chan Request, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		var req Request
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			return
		}
		gotReq <- req
		json.NewEncoder(conn).Encode(resp)
	}()

	return ln.Addr().String(), gotReq
}

func TestHelperCommandRunner_SendsTheRequestAndReturnsSuccessfulOutput(t *testing.T) {
	addr, gotReq := fakeHelperServer(t, Response{Output: []byte("ok")})
	runner := &HelperCommandRunner{Dial: func() (net.Conn, error) { return net.Dial("tcp", addr) }}

	out, err := runner.Run(testAppcmdPath, "set", "vdir", "DevPlatform Test Site/", "/physicalPath:C:\\releases\\5")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if string(out) != "ok" {
		t.Fatalf("expected output %q, got %q", "ok", out)
	}

	req := <-gotReq
	if req.Name != testAppcmdPath || len(req.Args) != 4 {
		t.Fatalf("unexpected request sent over the wire: %+v", req)
	}
}

func TestHelperCommandRunner_TurnsAResponseErrorIntoAGoError(t *testing.T) {
	addr, _ := fakeHelperServer(t, Response{Error: "iishelper: rejected request"})
	runner := &HelperCommandRunner{Dial: func() (net.Conn, error) { return net.Dial("tcp", addr) }}

	_, err := runner.Run(testAppcmdPath, "set", "vdir", "DevPlatform Test Site/", "/physicalPath:C:\\releases\\5")
	if err == nil {
		t.Fatal("expected an error when the response carries one")
	}
}

func TestHelperCommandRunner_ReturnsAClearErrorWhenTheHelperIsUnreachable(t *testing.T) {
	runner := &HelperCommandRunner{Dial: func() (net.Conn, error) {
		return nil, errors.New("test: connection refused")
	}}

	_, err := runner.Run(testAppcmdPath, "set", "vdir", "DevPlatform Test Site/", "/physicalPath:C:\\releases\\5")
	if err == nil {
		t.Fatal("expected an error when Dial fails")
	}
}

// TestHelperCommandRunner_TimesOutWhenDialHangs proves Run's dialTimeout
// budget covers the connect phase itself, not just the post-connect
// send/receive — a Dial implementation that never returns must still
// cause Run to return an error promptly rather than block forever.
func TestHelperCommandRunner_TimesOutWhenDialHangs(t *testing.T) {
	original := dialTimeout
	dialTimeout = 20 * time.Millisecond
	t.Cleanup(func() { dialTimeout = original })

	// unblock lets the goroutine inside dial() eventually finish (and
	// write into its buffered result channel) once the test is done,
	// instead of leaking blocked forever for the life of the test binary.
	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })

	runner := &HelperCommandRunner{Dial: func() (net.Conn, error) {
		<-unblock
		return nil, errors.New("test: dial finally returned after the test's timeout fired")
	}}

	start := time.Now()
	_, err := runner.Run(testAppcmdPath, "set", "vdir", "DevPlatform Test Site/", "/physicalPath:C:\\releases\\5")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when Dial hangs past dialTimeout")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run took %v to return after a hung Dial; expected it to respect the overridden dialTimeout (20ms)", elapsed)
	}
}
