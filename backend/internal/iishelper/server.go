package iishelper

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"time"
)

// Executor actually runs a request that has already passed
// ValidateRequest. Production wiring (cmd/iishelper) passes
// deploy.RealCommandRunner{}.Run; tests pass a fake that records calls
// without ever touching a real appcmd.exe.
type Executor func(name string, args ...string) ([]byte, error)

// Server is the transport-agnostic core of iishelper: given any
// net.Listener, it accepts connections, validates each request against
// AppcmdPath/AllowedSites, and only calls Execute for requests that pass.
// Deliberately independent of the Windows-specific named-pipe setup (see
// cmd/iishelper) so this logic — the actual security boundary — is
// testable with a plain loopback TCP listener, no real named pipe or
// Windows Service required.
type Server struct {
	AppcmdPath   string
	AllowedSites map[string]bool
	// ReleasesRoot is the one directory tree a request's physical path is
	// ever allowed to point into (see ValidateRequest's doc comment for
	// why this matters even though devplatform.exe is the only intended
	// caller).
	ReleasesRoot string
	Execute      Executor
}

// Serve accepts connections from ln until Accept returns an error (e.g.
// ln was closed by the caller during shutdown). Each connection carries
// exactly one request/response pair.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	var req Request
	if err := json.NewDecoder(io.LimitReader(conn, 64*1024)).Decode(&req); err != nil {
		log.Printf("iishelper: failed to decode request: %v", err)
		return
	}

	resp := s.process(req)
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		log.Printf("iishelper: failed to encode response: %v", err)
	}
}

func (s *Server) process(req Request) Response {
	if err := ValidateRequest(req, s.AppcmdPath, s.AllowedSites, s.ReleasesRoot); err != nil {
		log.Printf("iishelper: rejected request: %v", err)
		return Response{Error: err.Error()}
	}

	out, err := s.Execute(req.Name, req.Args...)
	if err != nil {
		return Response{Output: out, Error: err.Error()}
	}
	return Response{Output: out}
}
