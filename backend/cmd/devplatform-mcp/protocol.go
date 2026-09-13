package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// The Model Context Protocol, as far as a tools-only server needs it.
//
// MCP is JSON-RPC 2.0 over stdin/stdout, one JSON object per line. A
// server that offers nothing but tools has to answer exactly three
// methods — initialize, tools/list, tools/call — and ignore the rest.
//
// That is written by hand here rather than taken from an SDK. The
// protocol surface below is about a hundred lines and has a written
// specification; an SDK would be a dependency to track for no reduction
// in the code that matters. This codebase already carries the cost of
// that trade going the other way: go-git v6 is an alpha, and its
// PackfileWriter fast path silently bypassing a decorator is the kind of
// bug you inherit rather than write (see internal/gitserver).

const (
	// protocolVersion is what this server speaks. The client sends its
	// own in initialize; the reply below echoes the client's back when it
	// is one we understand, which is how MCP negotiates.
	protocolVersion = "2024-11-05"

	// jsonRPCVersion is fixed by JSON-RPC 2.0 and appears on every
	// message in both directions.
	jsonRPCVersion = "2.0"
)

// JSON-RPC error codes used here. The first three are defined by JSON-RPC
// itself; anything a tool does wrong is reported as a tool result with
// isError set, not as a protocol error — see callTool.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
)

// request is an incoming JSON-RPC message.
//
// ID is json.RawMessage because JSON-RPC allows a string, a number, or
// (for notifications) nothing at all, and a reply must echo the id back
// byte-identically. Decoding it into any Go type would mean re-encoding
// it, and re-encoding 1 as 1.0 is the kind of difference a strict client
// notices.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the message expects no reply. JSON-RPC
// notifications carry no id — answering one is a protocol violation, and
// a client that receives a response it never asked for may well close the
// connection.
func (r request) isNotification() bool { return len(r.ID) == 0 }

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// server reads requests from in and writes replies to out.
type server struct {
	in  io.Reader
	out io.Writer

	// mu serialises writes. Nothing here is concurrent today — the loop
	// handles one message at a time — but a half-written JSON line is an
	// unrecoverable protocol desync, so the guarantee is worth stating in
	// the type rather than in a comment that a later goroutine ignores.
	mu sync.Mutex

	tools *toolset
}

// serve runs until stdin closes, which is how an MCP client shuts a
// server down: Claude Code starts this process when a session opens and
// closes its stdin when the session ends.
func (s *server) serve() error {
	scanner := bufio.NewScanner(s.in)
	// A tool result can carry a whole task with its comments, so the
	// default 64 KiB line limit is not enough for the replies this server
	// sends — and the same buffer bounds what it can read.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			// No id could be recovered, so this reply names none. A
			// client that cannot match it still learns the stream went
			// wrong, which is better than silence.
			s.write(response{JSONRPC: jsonRPCVersion, Error: &rpcError{codeParseError, "malformed JSON"}})
			continue
		}

		s.handle(req)
	}
	return scanner.Err()
}

func (s *server) handle(req request) {
	if req.JSONRPC != jsonRPCVersion {
		s.fail(req, codeInvalidRequest, "jsonrpc must be "+jsonRPCVersion)
		return
	}

	switch req.Method {
	case "initialize":
		s.reply(req, s.initialize(req.Params))

	case "tools/list":
		s.reply(req, map[string]any{"tools": s.tools.describe()})

	case "tools/call":
		s.reply(req, s.tools.call(req.Params))

	default:
		// Notifications are the normal case here: a client sends
		// notifications/initialized, notifications/cancelled and others
		// that a tools-only server has nothing to do about. Silence is
		// the correct response to a notification; an unknown *request*
		// still gets an error so a client is never left waiting.
		if req.isNotification() {
			return
		}
		s.fail(req, codeMethodNotFound, "unsupported method: "+req.Method)
	}
}

// initializeParams is the part of the client's handshake this server
// reads. Everything else it sends is ignored on purpose — a server that
// fails on an unrecognised field breaks the first time the protocol
// grows one.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

func (s *server) initialize(raw json.RawMessage) map[string]any {
	version := protocolVersion
	var p initializeParams
	if err := json.Unmarshal(raw, &p); err == nil && p.ProtocolVersion != "" {
		// Echo the client's version back when it asks for one. MCP's
		// negotiation is "answer with a version you both speak"; a server
		// that always announces its own would refuse a client that is
		// merely newer.
		version = p.ProtocolVersion
	}

	return map[string]any{
		"protocolVersion": version,
		// Tools only. Declaring a capability this server does not
		// implement would invite calls it cannot answer.
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "devplatform",
			"version": buildVersion,
		},
	}
}

func (s *server) reply(req request, result any) {
	if req.isNotification() {
		return
	}
	s.write(response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: result})
}

func (s *server) fail(req request, code int, message string) {
	if req.isNotification() {
		return
	}
	s.write(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: &rpcError{code, message}})
}

func (s *server) write(resp response) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(resp)
	if err != nil {
		// Marshalling a reply cannot be recovered from here — the
		// alternative is writing nothing and leaving the client waiting
		// forever, so send a protocol error in its place.
		data, _ = json.Marshal(response{
			JSONRPC: jsonRPCVersion,
			ID:      resp.ID,
			Error:   &rpcError{codeInvalidRequest, "response could not be encoded"},
		})
	}
	// One object per line: the framing the stdio transport is defined in
	// terms of. Marshal never emits a newline, so this is the only place
	// a message boundary is written.
	fmt.Fprintf(s.out, "%s\n", data)
}
