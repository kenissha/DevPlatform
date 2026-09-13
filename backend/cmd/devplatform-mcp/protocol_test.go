package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// exchange runs the server over a canned input and returns one decoded
// reply per line it wrote.
func exchange(t *testing.T, tools *toolset, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	srv := &server{in: strings.NewReader(strings.Join(lines, "\n") + "\n"), out: &out, tools: tools}
	if err := srv.serve(); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var reply map[string]any
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("yanit cozulemedi %q: %v", line, err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func emptyTools() *toolset { return &toolset{} }

func TestInitializeAnswersTheHandshake(t *testing.T) {
	replies := exchange(t, emptyTools(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)

	if len(replies) != 1 {
		t.Fatalf("%d yanit, 1 bekleniyordu", len(replies))
	}
	result, ok := replies[0]["result"].(map[string]any)
	if !ok {
		t.Fatalf("result yok: %+v", replies[0])
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v", result["protocolVersion"])
	}
	caps, _ := result["capabilities"].(map[string]any)
	if _, hasTools := caps["tools"]; !hasTools {
		t.Errorf("tools yetenegi bildirilmemis: %+v", caps)
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "devplatform" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}
}

// MCP negotiates by answering with a version both sides speak. A server
// that always announced its own would refuse a merely newer client.
func TestInitializeEchoesTheClientsVersion(t *testing.T) {
	replies := exchange(t, emptyTools(),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)

	result := replies[0]["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion = %v, istemcinin surumu bekleniyordu", result["protocolVersion"])
	}
}

// Answering a notification is a protocol violation — a client that gets a
// response it never asked for may close the connection.
func TestNotificationsGetNoReply(t *testing.T) {
	replies := exchange(t, emptyTools(),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{}}`)

	if len(replies) != 0 {
		t.Fatalf("%d yanit yazildi, hicbiri beklenmiyordu: %+v", len(replies), replies)
	}
}

// An unknown *request* still gets an error, or the client waits forever.
func TestUnknownMethodIsAnError(t *testing.T) {
	replies := exchange(t, emptyTools(),
		`{"jsonrpc":"2.0","id":7,"method":"resources/list"}`)

	rpcErr, ok := replies[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("hata bekleniyordu: %+v", replies[0])
	}
	if int(rpcErr["code"].(float64)) != codeMethodNotFound {
		t.Errorf("code = %v", rpcErr["code"])
	}
}

// The id must come back byte-identically: a client matches replies by it,
// and 1 re-encoded as 1.0 is a different id.
func TestIDIsEchoedVerbatim(t *testing.T) {
	replies := exchange(t, emptyTools(),
		`{"jsonrpc":"2.0","id":"abc-1","method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":42,"method":"tools/list"}`)

	if replies[0]["id"] != "abc-1" {
		t.Errorf("id = %#v, want \"abc-1\"", replies[0]["id"])
	}
	if n, ok := replies[1]["id"].(float64); !ok || n != 42 {
		t.Errorf("id = %#v, want 42", replies[1]["id"])
	}
}

func TestMalformedJSONGetsAParseError(t *testing.T) {
	replies := exchange(t, emptyTools(), `{bu json degil`)

	rpcErr, ok := replies[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("hata bekleniyordu: %+v", replies[0])
	}
	if int(rpcErr["code"].(float64)) != codeParseError {
		t.Errorf("code = %v", rpcErr["code"])
	}
}

// One line in, one line out — the framing the stdio transport is defined
// in terms of. A reply split across lines desynchronises the client.
func TestEachReplyIsExactlyOneLine(t *testing.T) {
	var out bytes.Buffer
	srv := &server{
		in:    strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"),
		out:   &out,
		tools: buildTools(newAPIClient("http://example", "dev", "tok")),
	}
	if err := srv.serve(); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if got := strings.Count(out.String(), "\n"); got != 1 {
		t.Fatalf("%d satir yazildi, 1 bekleniyordu", got)
	}
}

func TestBadJSONRPCVersionIsRejected(t *testing.T) {
	replies := exchange(t, emptyTools(),
		`{"jsonrpc":"1.0","id":1,"method":"tools/list"}`)

	rpcErr, ok := replies[0]["error"].(map[string]any)
	if !ok {
		t.Fatalf("hata bekleniyordu: %+v", replies[0])
	}
	if int(rpcErr["code"].(float64)) != codeInvalidRequest {
		t.Errorf("code = %v", rpcErr["code"])
	}
}

// Every tool must present a schema a client can validate against, and a
// description that says when to call it — the model has nothing else to
// go on.
func TestEveryToolIsFullyDescribed(t *testing.T) {
	tools := buildTools(newAPIClient("http://example", "dev", "tok")).describe()
	if len(tools) == 0 {
		t.Fatal("hic arac yok")
	}
	for _, tl := range tools {
		name, _ := tl["name"].(string)
		if name == "" {
			t.Errorf("adsiz arac: %+v", tl)
		}
		if desc, _ := tl["description"].(string); len(desc) < 40 {
			t.Errorf("%s: aciklama cok kisa (%q)", name, desc)
		}
		sch, ok := tl["inputSchema"].(map[string]any)
		if !ok || sch["type"] != "object" {
			t.Errorf("%s: inputSchema yok ya da object degil", name)
		}
		if _, ok := sch["properties"].(map[string]any); !ok {
			t.Errorf("%s: properties yok", name)
		}
		if _, ok := sch["required"].([]string); !ok {
			t.Errorf("%s: required yok", name)
		}
	}
}

// Nothing deletes. A task removed by mistake takes its comments and its
// history with it, and writing work down does not require it.
func TestNoToolDeletes(t *testing.T) {
	for _, tl := range buildTools(newAPIClient("http://example", "dev", "tok")).describe() {
		name := tl["name"].(string)
		if strings.Contains(name, "sil") || strings.Contains(strings.ToLower(name), "delete") {
			t.Errorf("silme araci var: %s", name)
		}
	}
}
