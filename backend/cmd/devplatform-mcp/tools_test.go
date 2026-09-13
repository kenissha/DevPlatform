package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAPI stands in for DevPlatform. Each handler is keyed by
// "METHOD /path" so a test declares only the calls it expects; anything
// else is a 404 the test will notice.
type fakeAPI struct {
	t        *testing.T
	server   *httptest.Server
	handlers map[string]func(body map[string]any) (int, any)
	seen     []string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t, handlers: map[string]func(map[string]any) (int, any){}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, token, ok := r.BasicAuth()
		if !ok || subject != "dev-1" || token != "tok" {
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}
		key := r.Method + " " + r.URL.Path
		f.seen = append(f.seen, key)

		var body map[string]any
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		handler, ok := f.handlers[key]
		if !ok {
			http.Error(w, "404 "+key, http.StatusNotFound)
			return
		}
		status, payload := handler(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAPI) on(key string, handler func(body map[string]any) (int, any)) {
	f.handlers[key] = handler
}

func (f *fakeAPI) tools() *toolset {
	return buildTools(newAPIClient(f.server.URL, "dev-1", "tok"))
}

func sampleTasks() []map[string]any {
	return []map[string]any{
		{"id": "aaaa111122223333", "key": "DEN-1", "repo": "deneme", "title": "Tarih filtresi",
			"status": "in_progress", "priority": "high", "assignedTo": "dev-1",
			"dueDate": "2020-01-01", "createdAt": "2026-01-01T10:00:00Z"},
		{"id": "bbbb111122223333", "key": "DEN-2", "repo": "deneme", "title": "Excel disa aktarim",
			"status": "todo", "priority": "low", "createdAt": "2026-01-02T10:00:00Z"},
		{"id": "cccc111122223333", "key": "DEN-3", "repo": "deneme", "title": "Giris zaman asimi",
			"status": "done", "priority": "normal", "assignedTo": "ahmet",
			"createdAt": "2026-01-03T10:00:00Z"},
	}
}

func callTool(t *testing.T, tools *toolset, name string, args map[string]any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	result := tools.call(raw)
	isError, _ := result["isError"].(bool)
	content, _ := result["content"].([]map[string]any)
	if len(content) == 0 {
		t.Fatalf("%s: icerik yok: %+v", name, result)
	}
	text, _ := content[0]["text"].(string)
	return text, isError
}

func TestListTasksRendersTheBoard(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("GET /api/users", func(map[string]any) (int, any) {
		return 200, []map[string]any{{"subject": "dev-1", "displayName": "Rifat Ozturk"}}
	})

	text, isErr := callTool(t, api.tools(), "gorev_listele", nil)
	if isErr {
		t.Fatalf("hata: %s", text)
	}
	for _, want := range []string{"DEN-1", "DEN-2", "DEN-3", "Rifat Ozturk", "GECİKTİ"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q yok:\n%s", want, text)
		}
	}
	// Highest priority first, matching the board's own order.
	if strings.Index(text, "DEN-1") > strings.Index(text, "DEN-2") {
		t.Errorf("oncelik sirasi yanlis:\n%s", text)
	}
}

// "ben" has to resolve to the logged-in person — it is how somebody
// actually asks, and the model cannot know the subject otherwise.
func TestListTasksResolvesBenToTheLoggedInPerson(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("GET /api/users", func(map[string]any) (int, any) { return 200, []map[string]any{} })

	text, _ := callTool(t, api.tools(), "gorev_listele", map[string]any{"atanan": "ben"})
	if !strings.Contains(text, "DEN-1") {
		t.Errorf("kendi gorevi gelmedi:\n%s", text)
	}
	if strings.Contains(text, "DEN-3") {
		t.Errorf("baskasinin gorevi geldi:\n%s", text)
	}
}

func TestListTasksFiltersOverdue(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("GET /api/users", func(map[string]any) (int, any) { return 200, []map[string]any{} })

	text, _ := callTool(t, api.tools(), "gorev_listele", map[string]any{"sadece_gecikenler": true})
	if !strings.Contains(text, "DEN-1") || strings.Contains(text, "DEN-2") {
		t.Errorf("geciken filtresi yanlis:\n%s", text)
	}
}

// A finished task is never overdue — chasing delivered work is noise.
func TestDoneTaskIsNotOverdue(t *testing.T) {
	if overdue(task{DueDate: "2020-01-01", Status: "done"}, "2026-01-01") {
		t.Fatal("bitmis gorev geciken sayildi")
	}
}

// Keys are what people say and what commit messages carry; the API
// addresses tasks by opaque id. Every write resolves one to the other.
func TestUpdateResolvesTheKeyCaseInsensitively(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/repos/deneme/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("PATCH /api/repos/deneme/tasks/aaaa111122223333", func(body map[string]any) (int, any) {
		if body["status"] != "done" {
			t.Errorf("status gonderilmedi: %+v", body)
		}
		if _, touched := body["priority"]; touched {
			t.Errorf("istenmeyen alan gonderildi: %+v", body)
		}
		return 200, map[string]any{"key": "DEN-1", "status": "done", "priority": "high"}
	})

	text, isErr := callTool(t, api.tools(), "gorev_guncelle",
		map[string]any{"repo": "deneme", "anahtar": "den-1", "durum": "done"})
	if isErr {
		t.Fatalf("hata: %s", text)
	}
	if !strings.Contains(text, "DEN-1") {
		t.Errorf("beklenmeyen yanit: %s", text)
	}
}

// An update must never clobber a field it was not asked about — including
// one a person changed in the panel while the session was open.
func TestUpdateSendsOnlyTheFieldsItWasGiven(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/repos/deneme/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("PATCH /api/repos/deneme/tasks/aaaa111122223333", func(body map[string]any) (int, any) {
		if len(body) != 1 {
			t.Errorf("%d alan gonderildi, 1 bekleniyordu: %+v", len(body), body)
		}
		return 200, map[string]any{"key": "DEN-1", "status": "in_progress", "priority": "critical"}
	})

	if _, isErr := callTool(t, api.tools(), "gorev_guncelle",
		map[string]any{"repo": "deneme", "anahtar": "DEN-1", "oncelik": "critical"}); isErr {
		t.Fatal("guncelleme basarisiz")
	}
}

// Clearing a due date and leaving it alone are different requests, and
// the API reads an empty string as the former — so the difference has to
// survive from the model's arguments all the way to the wire.
func TestEmptyStringClearsButAbsenceLeavesAlone(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/repos/deneme/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("PATCH /api/repos/deneme/tasks/aaaa111122223333", func(body map[string]any) (int, any) {
		due, present := body["dueDate"]
		if !present {
			t.Error("dueDate gonderilmedi")
		}
		if due != "" {
			t.Errorf("dueDate = %v, bos bekleniyordu", due)
		}
		return 200, map[string]any{"key": "DEN-1", "status": "todo", "priority": "normal"}
	})

	if _, isErr := callTool(t, api.tools(), "gorev_guncelle",
		map[string]any{"repo": "deneme", "anahtar": "DEN-1", "bitis": ""}); isErr {
		t.Fatal("guncelleme basarisiz")
	}
}

func TestUpdateWithNoFieldsIsRefused(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/repos/deneme/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })

	text, isErr := callTool(t, api.tools(), "gorev_guncelle",
		map[string]any{"repo": "deneme", "anahtar": "DEN-1"})
	if !isErr {
		t.Fatalf("hata bekleniyordu, alinan: %s", text)
	}
}

func TestUnknownKeyReportsWhereItLooked(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/repos/deneme/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })

	text, isErr := callTool(t, api.tools(), "gorev_detay",
		map[string]any{"repo": "deneme", "anahtar": "DEN-999"})
	if !isErr {
		t.Fatal("hata bekleniyordu")
	}
	if !strings.Contains(text, "deneme") || !strings.Contains(text, "DEN-999") {
		t.Errorf("hata mesaji yetersiz: %s", text)
	}
}

// Priority and due date are not accepted at creation — the API fixes both
// so every task starts in the same place — so the tool applies them as an
// edit, which is also what the panel does.
func TestCreateAppliesPriorityAsAFollowUpEdit(t *testing.T) {
	api := newFakeAPI(t)
	api.on("POST /api/repos/deneme/tasks", func(body map[string]any) (int, any) {
		if body["title"] != "Yeni is" {
			t.Errorf("title = %v", body["title"])
		}
		if _, sent := body["priority"]; sent {
			t.Errorf("olusturmada oncelik gonderildi: %+v", body)
		}
		return 201, map[string]any{"id": "dddd111122223333", "key": "DEN-4", "title": "Yeni is"}
	})
	patched := false
	api.on("PATCH /api/repos/deneme/tasks/dddd111122223333", func(body map[string]any) (int, any) {
		patched = true
		if body["priority"] != "critical" {
			t.Errorf("priority = %v", body["priority"])
		}
		return 200, map[string]any{"key": "DEN-4"}
	})

	text, isErr := callTool(t, api.tools(), "gorev_ac",
		map[string]any{"repo": "deneme", "baslik": "Yeni is", "oncelik": "critical"})
	if isErr {
		t.Fatalf("hata: %s", text)
	}
	if !patched {
		t.Error("oncelik uygulanmadi")
	}
	if !strings.Contains(text, "DEN-4") {
		t.Errorf("anahtar bildirilmedi: %s", text)
	}
}

// A half-succeeded create must report the task that does exist rather
// than failing the whole call — the work is on the board either way.
func TestCreateReportsTheTaskEvenIfTheFollowUpEditFails(t *testing.T) {
	api := newFakeAPI(t)
	api.on("POST /api/repos/deneme/tasks", func(map[string]any) (int, any) {
		return 201, map[string]any{"id": "dddd111122223333", "key": "DEN-4", "title": "Yeni is"}
	})
	api.on("PATCH /api/repos/deneme/tasks/dddd111122223333", func(map[string]any) (int, any) {
		return 400, nil
	})

	text, isErr := callTool(t, api.tools(), "gorev_ac",
		map[string]any{"repo": "deneme", "baslik": "Yeni is", "bitis": "2026-12-31"})
	if isErr {
		t.Fatalf("cagri tumden basarisiz oldu: %s", text)
	}
	if !strings.Contains(text, "DEN-4") || !strings.Contains(text, "uyarı") {
		t.Errorf("yarim basari bildirilmedi: %s", text)
	}
}

func TestCreateRequiresRepoAndTitle(t *testing.T) {
	api := newFakeAPI(t)
	if _, isErr := callTool(t, api.tools(), "gorev_ac", map[string]any{"repo": "deneme"}); !isErr {
		t.Error("basliksiz cagri kabul edildi")
	}
	if _, isErr := callTool(t, api.tools(), "gorev_ac", map[string]any{"baslik": "x"}); !isErr {
		t.Error("reposuz cagri kabul edildi")
	}
}

func TestCommentIsPosted(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/repos/deneme/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("POST /api/repos/deneme/tasks/aaaa111122223333/comments", func(body map[string]any) (int, any) {
		if body["body"] != "akasama biter" {
			t.Errorf("body = %v", body["body"])
		}
		return 201, map[string]any{}
	})

	text, isErr := callTool(t, api.tools(), "gorev_yorum",
		map[string]any{"repo": "deneme", "anahtar": "DEN-1", "yorum": "akasama biter"})
	if isErr {
		t.Fatalf("hata: %s", text)
	}
	if !strings.Contains(text, "DEN-1") {
		t.Errorf("beklenmeyen yanit: %s", text)
	}
}

func TestEmptyCommentIsRefusedWithoutCallingTheAPI(t *testing.T) {
	api := newFakeAPI(t)
	if _, isErr := callTool(t, api.tools(), "gorev_yorum",
		map[string]any{"repo": "deneme", "anahtar": "DEN-1", "yorum": "   "}); !isErr {
		t.Fatal("bos yorum kabul edildi")
	}
	if len(api.seen) != 0 {
		t.Errorf("API cagrildi: %v", api.seen)
	}
}

// Comments and commits are context, not the task. Losing them must not
// lose the task itself.
func TestDetailSurvivesMissingCommentsAndCommits(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/repos/deneme/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("GET /api/users", func(map[string]any) (int, any) { return 200, []map[string]any{} })
	// comments and commits deliberately unregistered -> 404

	text, isErr := callTool(t, api.tools(), "gorev_detay",
		map[string]any{"repo": "deneme", "anahtar": "DEN-1"})
	if isErr {
		t.Fatalf("hata: %s", text)
	}
	if !strings.Contains(text, "Tarih filtresi") {
		t.Errorf("gorev kayboldu: %s", text)
	}
}

// A report with raw subjects is worth more than no report.
func TestReportSurvivesAMissingPeopleList(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })

	text, isErr := callTool(t, api.tools(), "rapor", map[string]any{"gun": float64(9999)})
	if isErr {
		t.Fatalf("hata: %s", text)
	}
	for _, want := range []string{"DEVAM EDEN", "GECİKEN", "DEN-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q yok:\n%s", want, text)
		}
	}
}

// The report must say that "done in this period" is an approximation —
// the board records when a task was created, not when it was closed.
func TestReportLabelsTheCompletionApproximation(t *testing.T) {
	api := newFakeAPI(t)
	api.on("GET /api/tasks", func(map[string]any) (int, any) { return 200, sampleTasks() })
	api.on("GET /api/users", func(map[string]any) (int, any) { return 200, []map[string]any{} })

	text, _ := callTool(t, api.tools(), "rapor", map[string]any{"gun": float64(9999)})
	if !strings.Contains(text, "not:") {
		t.Errorf("yaklasiklik belirtilmemis:\n%s", text)
	}
}

// A 401 means the cached login expired. "401 Unauthorized" is useless to
// the reader; the fix is what matters.
func TestExpiredLoginSaysWhatToRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	tools := buildTools(newAPIClient(server.URL, "dev-1", "eski"))
	text, isErr := callTool(t, tools, "gorev_listele", nil)
	if !isErr {
		t.Fatal("hata bekleniyordu")
	}
	if !strings.Contains(text, "devplatform-login") {
		t.Errorf("ne yapilacagi soylenmemis: %s", text)
	}
}

// A tool failure is an error RESULT, not a JSON-RPC error: the model can
// read it and act on it, where a protocol error just says the server
// broke.
func TestToolFailureIsAResultNotAProtocolError(t *testing.T) {
	api := newFakeAPI(t)
	raw, _ := json.Marshal(map[string]any{"name": "gorev_ac", "arguments": map[string]any{}})
	result := api.tools().call(raw)

	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("isError yok: %+v", result)
	}
	if _, hasContent := result["content"]; !hasContent {
		t.Fatalf("content yok: %+v", result)
	}
}

func TestUnknownToolIsReported(t *testing.T) {
	api := newFakeAPI(t)
	raw, _ := json.Marshal(map[string]any{"name": "gorev_sil", "arguments": map[string]any{}})
	result := api.tools().call(raw)

	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatal("bilinmeyen arac kabul edildi")
	}
}

// A repo name reaches here from the model. One containing a slash would
// otherwise address a different endpoint entirely.
func TestRepoNameIsEscapedIntoThePath(t *testing.T) {
	if got := repoPath("../gizli", "tasks"); strings.Contains(got, "../") {
		t.Fatalf("kacis kacirildi: %s", got)
	}
}

// A number arriving where a string was declared must not fail the call —
// that spends a turn teaching the model JSON instead of doing the work.
func TestArgumentsAreReadLeniently(t *testing.T) {
	args := map[string]any{"a": "  bosluk  ", "b": float64(14), "c": true}
	if got := argString(args, "a"); got != "bosluk" {
		t.Errorf("a = %q", got)
	}
	if got := argString(args, "b"); got != "14" {
		t.Errorf("b = %q", got)
	}
	if got := argString(args, "c"); got != "true" {
		t.Errorf("c = %q", got)
	}
	if got := argString(args, "yok"); got != "" {
		t.Errorf("eksik alan = %q", got)
	}
	if got := argString(nil, "a"); got != "" {
		t.Errorf("nil args = %q", got)
	}
}
