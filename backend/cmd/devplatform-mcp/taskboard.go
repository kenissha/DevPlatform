package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The task-board tools themselves, plus the rendering that turns API JSON
// into something worth spending context on.
//
// Every tool returns text, not JSON. A tool result is read by a model,
// and a rendered board costs a fraction of the tokens the same data costs
// as JSON — the field names alone repeat on every record. Where an exact
// value matters (a key, a date) it is in the text; where it does not (an
// internal id), it is left out.

type task struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Repo        string    `json:"repo"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	AssignedTo  string    `json:"assignedTo"`
	Author      string    `json:"author"`
	Status      string    `json:"status"`
	Priority    string    `json:"priority"`
	DueDate     string    `json:"dueDate"`
	Labels      []string  `json:"labels"`
	Subtasks    []subtask `json:"subtasks"`
	CreatedAt   time.Time `json:"createdAt"`
}

type subtask struct {
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

type comment struct {
	Author string    `json:"author"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

type taskCommit struct {
	Hash    string    `json:"hash"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	At      time.Time `json:"at"`
}

type person struct {
	Subject     string `json:"subject"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

var statusNames = map[string]string{
	"todo":          "Yapılacak",
	"in_progress":   "Yapılıyor",
	"awaiting_test": "Test bekliyor",
	"done":          "Bitti",
}

var priorityNames = map[string]string{
	"low": "düşük", "normal": "normal", "high": "yüksek", "critical": "kritik",
}

func buildTools(api *apiClient) *toolset {
	t := &toolset{api: api}
	t.tools = []tool{
		{
			name: "gorev_listele",
			description: "DevPlatform görev panosundaki işleri listeler. " +
				"Bir repoda ne olduğunu, kimin neyle uğraştığını ya da neyin geciktiğini öğrenmek için kullan. " +
				"repo verilmezse erişilebilen bütün repolardaki görevler gelir.",
			schema: schema(nil, map[string]any{
				"repo":   prop("string", "Repo adı. Verilmezse bütün repolar."),
				"durum":  enumProp("Yalnızca bu durumdakiler.", "todo", "in_progress", "awaiting_test", "done"),
				"atanan": prop("string", "Kişinin kullanıcı adı. 'ben' yazarsan giriş yapmış kişi."),
				"sadece_gecikenler": map[string]any{
					"type":        "boolean",
					"description": "Yalnızca bitiş tarihi geçmiş ve bitmemiş görevler.",
				},
			}),
			run: t.listTasks,
		},
		{
			name: "gorev_detay",
			description: "Tek bir görevin tamamını getirir: açıklama, alt görevler, yorumlar ve o görevi anan commitler. " +
				"Bir görev üzerinde çalışmaya başlamadan önce ya da neyin konuşulduğunu görmek için kullan.",
			schema: schema([]string{"repo", "anahtar"}, map[string]any{
				"repo":    prop("string", "Repo adı."),
				"anahtar": prop("string", "Görev anahtarı, örneğin DEN-14."),
			}),
			run: t.taskDetail,
		},
		{
			name: "gorev_ac",
			description: "Yeni görev açar. Konuşma sırasında ortaya çıkan bir işi panoya yazmak için kullan — " +
				"kullanıcı 'şunu da yapmak lazım' dediğinde, elle girmesini bekleme.",
			schema: schema([]string{"repo", "baslik"}, map[string]any{
				"repo":     prop("string", "Repo adı."),
				"baslik":   prop("string", "Görevin başlığı — tek satır, ne yapılacağını söylesin."),
				"aciklama": prop("string", "İsteğe bağlı ayrıntı."),
				"atanan":   prop("string", "Kişinin kullanıcı adı. Boş bırakılırsa atanmamış."),
				"oncelik":  enumProp("Öncelik. Verilmezse normal.", "low", "normal", "high", "critical"),
				"bitis":    prop("string", "Bitiş tarihi, YYYY-AA-GG."),
				"etiketler": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string", "enum": []string{"hata", "ozellik", "iyilestirme", "teknik-borc", "dokuman", "arastirma"}},
					"description": "Etiketler.",
				},
			}),
			run: t.createTask,
		},
		{
			name: "gorev_guncelle",
			description: "Var olan bir görevi değiştirir: durum, öncelik, atanan, etiket, bitiş tarihi, başlık, açıklama. " +
				"Bir iş bittiğinde durumu 'done' yapmak için de bunu kullan. " +
				"Yalnızca verdiğin alanlar değişir, ötekilere dokunulmaz.",
			schema: schema([]string{"repo", "anahtar"}, map[string]any{
				"repo":     prop("string", "Repo adı."),
				"anahtar":  prop("string", "Görev anahtarı, örneğin DEN-14."),
				"durum":    enumProp("Yeni durum.", "todo", "in_progress", "awaiting_test", "done"),
				"oncelik":  enumProp("Yeni öncelik.", "low", "normal", "high", "critical"),
				"atanan":   prop("string", "Yeni atanan. Boş string atamayı kaldırır."),
				"baslik":   prop("string", "Yeni başlık."),
				"aciklama": prop("string", "Yeni açıklama."),
				"bitis":    prop("string", "Yeni bitiş tarihi YYYY-AA-GG. Boş string tarihi kaldırır."),
				"etiketler": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string", "enum": []string{"hata", "ozellik", "iyilestirme", "teknik-borc", "dokuman", "arastirma"}},
					"description": "Etiket kümesinin tamamı — verdiğin liste eskisinin yerine geçer. Boş liste etiketleri kaldırır.",
				},
			}),
			run: t.updateTask,
		},
		{
			name: "gorev_yorum",
			description: "Göreve yorum ekler. Ne yapıldığını, neden öyle yapıldığını ya da takılınan yeri " +
				"görevin altına yazmak için kullan — iş bittiğinde neyin nasıl çözüldüğü orada dursun.",
			schema: schema([]string{"repo", "anahtar", "yorum"}, map[string]any{
				"repo":    prop("string", "Repo adı."),
				"anahtar": prop("string", "Görev anahtarı."),
				"yorum":   prop("string", "Yorum metni."),
			}),
			run: t.addComment,
		},
		{
			name: "rapor",
			description: "Bir dönemin ham özetini çıkarır: hangi görevler bitti, kim neyle uğraşıyor, ne gecikti. " +
				"Yöneticiye sunulacak özet istendiğinde bunu çağır ve dönen veriyi istenen biçimde sun — " +
				"bu araç tablo ya da metin üretmez, veriyi verir.",
			schema: schema(nil, map[string]any{
				"gun":  prop("number", "Kaç günlük dönem. Verilmezse 7."),
				"repo": prop("string", "Tek bir repoyla sınırla. Verilmezse hepsi."),
			}),
			run: t.report,
		},
	}
	return t
}

// ------------------------------------------------------------- listing

func (t *toolset) listTasks(args map[string]any) (string, error) {
	tasks, err := t.fetchTasks(argString(args, "repo"))
	if err != nil {
		return "", err
	}

	assignee := argString(args, "atanan")
	if assignee == "ben" || assignee == "bana" {
		assignee = t.api.subject
	}
	status := argString(args, "durum")
	onlyOverdue, _ := args["sadece_gecikenler"].(bool)
	today := time.Now().Format("2006-01-02")

	filtered := tasks[:0:0]
	for _, task := range tasks {
		if status != "" && task.Status != status {
			continue
		}
		if assignee != "" && task.AssignedTo != assignee {
			continue
		}
		if onlyOverdue && !overdue(task, today) {
			continue
		}
		filtered = append(filtered, task)
	}

	if len(filtered) == 0 {
		return "Bu koşullara uyan görev yok.", nil
	}

	names := t.peopleNames()
	sortTasks(filtered)

	var b strings.Builder
	fmt.Fprintf(&b, "%d görev\n\n", len(filtered))
	for _, task := range filtered {
		fmt.Fprintf(&b, "%s  %s\n", task.Key, task.Title)
		fmt.Fprintf(&b, "    %s · %s · %s", task.Repo, statusName(task.Status), priorityName(task.Priority))
		if task.AssignedTo != "" {
			fmt.Fprintf(&b, " · %s", names.of(task.AssignedTo))
		} else {
			b.WriteString(" · atanmamış")
		}
		if task.DueDate != "" {
			if overdue(task, today) {
				fmt.Fprintf(&b, " · bitiş %s (GECİKTİ)", task.DueDate)
			} else {
				fmt.Fprintf(&b, " · bitiş %s", task.DueDate)
			}
		}
		if len(task.Labels) > 0 {
			fmt.Fprintf(&b, " · %s", strings.Join(task.Labels, ", "))
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func (t *toolset) taskDetail(args map[string]any) (string, error) {
	repo := argString(args, "repo")
	found, err := t.findByKey(repo, argString(args, "anahtar"))
	if err != nil {
		return "", err
	}
	names := t.peopleNames()

	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", found.Key, found.Title)
	fmt.Fprintf(&b, "%s · %s · %s\n", found.Repo, statusName(found.Status), priorityName(found.Priority))
	fmt.Fprintf(&b, "açan: %s", names.of(found.Author))
	if found.AssignedTo != "" {
		fmt.Fprintf(&b, " · atanan: %s", names.of(found.AssignedTo))
	}
	if found.DueDate != "" {
		fmt.Fprintf(&b, " · bitiş: %s", found.DueDate)
	}
	if len(found.Labels) > 0 {
		fmt.Fprintf(&b, " · etiket: %s", strings.Join(found.Labels, ", "))
	}
	b.WriteString("\n")

	if found.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", found.Description)
	}

	if len(found.Subtasks) > 0 {
		b.WriteString("\nAlt görevler:\n")
		for _, st := range found.Subtasks {
			mark := " "
			if st.Done {
				mark = "x"
			}
			fmt.Fprintf(&b, "  [%s] %s\n", mark, st.Title)
		}
	}

	// Comments and commits are context, not the task. A failure to read
	// them must not lose the task itself, so each is best-effort.
	var comments []comment
	if err := t.api.get(repoPath(found.Repo, "tasks", found.ID, "comments"), &comments); err == nil && len(comments) > 0 {
		b.WriteString("\nYorumlar:\n")
		for _, c := range comments {
			fmt.Fprintf(&b, "  %s (%s): %s\n", names.of(c.Author), c.At.Local().Format("02.01 15:04"), c.Body)
		}
	}

	var commits []taskCommit
	if err := t.api.get(repoPath(found.Repo, "tasks", found.ID, "commits"), &commits); err == nil && len(commits) > 0 {
		b.WriteString("\nCommitler:\n")
		for _, c := range commits {
			fmt.Fprintf(&b, "  %s %s (%s)\n", shortHash(c.Hash), c.Subject, c.Author)
		}
	}

	return b.String(), nil
}

// -------------------------------------------------------------- writes

func (t *toolset) createTask(args map[string]any) (string, error) {
	repo := argString(args, "repo")
	title := argString(args, "baslik")
	if repo == "" || title == "" {
		return "", fmt.Errorf("repo ve baslik zorunlu")
	}

	body := map[string]any{
		"title":       title,
		"description": argString(args, "aciklama"),
		"assignedTo":  argString(args, "atanan"),
	}
	if labels := argStrings(args, "etiketler"); len(labels) > 0 {
		body["labels"] = labels
	}

	var created task
	if err := t.api.do("POST", repoPath(repo, "tasks"), body, &created); err != nil {
		return "", err
	}

	// Priority and due date are not accepted at creation — the API fixes
	// both so that every task starts in the same place — so anything the
	// model asked for is applied as an edit, which is also what the panel
	// does and what puts it in the task's history.
	changes := map[string]any{}
	if p := argString(args, "oncelik"); p != "" && p != "normal" {
		changes["priority"] = p
	}
	if due := argString(args, "bitis"); due != "" {
		changes["dueDate"] = due
	}
	if len(changes) > 0 {
		if err := t.api.do("PATCH", repoPath(repo, "tasks", created.ID), changes, nil); err != nil {
			// The task exists; report it and say what did not stick,
			// rather than failing a call that half-succeeded.
			return fmt.Sprintf("%s açıldı: %s\n(uyarı: öncelik/tarih ayarlanamadı: %v)", created.Key, created.Title, err), nil
		}
	}

	return fmt.Sprintf("%s açıldı: %s", created.Key, created.Title), nil
}

func (t *toolset) updateTask(args map[string]any) (string, error) {
	repo := argString(args, "repo")
	found, err := t.findByKey(repo, argString(args, "anahtar"))
	if err != nil {
		return "", err
	}

	changes := map[string]any{}
	// Each field is sent only when the model mentioned it, so an update
	// never clobbers a value it was not asked about — including one a
	// person changed in the panel while this session was open.
	for arg, field := range map[string]string{
		"durum":    "status",
		"oncelik":  "priority",
		"atanan":   "assignedTo",
		"baslik":   "title",
		"aciklama": "description",
		"bitis":    "dueDate",
	} {
		if hasKey(args, arg) {
			changes[field] = argString(args, arg)
		}
	}
	if hasKey(args, "etiketler") {
		labels := argStrings(args, "etiketler")
		if labels == nil {
			labels = []string{}
		}
		changes["labels"] = labels
	}

	if len(changes) == 0 {
		return "", fmt.Errorf("değiştirilecek bir alan verilmedi")
	}

	var updated task
	if err := t.api.do("PATCH", repoPath(found.Repo, "tasks", found.ID), changes, &updated); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s güncellendi — %s · %s", updated.Key, statusName(updated.Status), priorityName(updated.Priority)), nil
}

func (t *toolset) addComment(args map[string]any) (string, error) {
	body := argString(args, "yorum")
	if body == "" {
		return "", fmt.Errorf("yorum boş olamaz")
	}
	found, err := t.findByKey(argString(args, "repo"), argString(args, "anahtar"))
	if err != nil {
		return "", err
	}
	if err := t.api.do("POST", repoPath(found.Repo, "tasks", found.ID, "comments"), map[string]any{"body": body}, nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s görevine yorum eklendi", found.Key), nil
}

// -------------------------------------------------------------- report

func (t *toolset) report(args map[string]any) (string, error) {
	days := 7
	if raw, ok := args["gun"].(float64); ok && raw > 0 {
		days = int(raw)
	}
	tasks, err := t.fetchTasks(argString(args, "repo"))
	if err != nil {
		return "", err
	}

	names := t.peopleNames()
	since := time.Now().AddDate(0, 0, -days)
	today := time.Now().Format("2006-01-02")

	var done, active, overdueList []task
	byPerson := map[string]int{}

	for _, task := range tasks {
		switch {
		case task.Status == "done":
			// Only tasks created within the window count as "this
			// period's work". The board does not record when a task was
			// closed — that lives in the audit log — so this is an
			// approximation, and it is labelled as one below rather than
			// presented as a completion date.
			if task.CreatedAt.After(since) {
				done = append(done, task)
			}
		default:
			active = append(active, task)
			if task.AssignedTo != "" {
				byPerson[task.AssignedTo]++
			}
			if overdue(task, today) {
				overdueList = append(overdueList, task)
			}
		}
	}

	sortTasks(done)
	sortTasks(active)
	sortTasks(overdueList)

	var b strings.Builder
	fmt.Fprintf(&b, "Son %d gün — ham veri (sunum bu aracın işi değil)\n\n", days)

	fmt.Fprintf(&b, "BU DÖNEMDE AÇILIP BİTEN (%d)\n", len(done))
	for _, task := range done {
		fmt.Fprintf(&b, "  %s  %s  [%s]  %s\n", task.Key, task.Title, task.Repo, names.of(task.AssignedTo))
	}
	if len(done) == 0 {
		b.WriteString("  yok\n")
	}
	b.WriteString("  not: pano görevin ne zaman kapandığını tutmuyor, bu liste dönem içinde AÇILMIŞ ve şu an bitmiş olanlar\n")

	fmt.Fprintf(&b, "\nDEVAM EDEN (%d)\n", len(active))
	for _, task := range active {
		fmt.Fprintf(&b, "  %s  %s  [%s]  %s  %s\n",
			task.Key, task.Title, task.Repo, statusName(task.Status), names.of(task.AssignedTo))
	}
	if len(active) == 0 {
		b.WriteString("  yok\n")
	}

	fmt.Fprintf(&b, "\nGECİKEN (%d)\n", len(overdueList))
	for _, task := range overdueList {
		fmt.Fprintf(&b, "  %s  %s  bitiş %s  %s\n", task.Key, task.Title, task.DueDate, names.of(task.AssignedTo))
	}
	if len(overdueList) == 0 {
		b.WriteString("  yok\n")
	}

	if len(byPerson) > 0 {
		b.WriteString("\nKİŞİ BAŞI AÇIK İŞ\n")
		people := make([]string, 0, len(byPerson))
		for subject := range byPerson {
			people = append(people, subject)
		}
		sort.Slice(people, func(i, j int) bool { return byPerson[people[i]] > byPerson[people[j]] })
		for _, subject := range people {
			fmt.Fprintf(&b, "  %-24s %d\n", names.of(subject), byPerson[subject])
		}
	}

	return b.String(), nil
}

// --------------------------------------------------------------- plumbing

func (t *toolset) fetchTasks(repo string) ([]task, error) {
	var tasks []task
	path := "/api/tasks"
	if repo != "" {
		path = repoPath(repo, "tasks")
	}
	if err := t.api.get(path, &tasks); err != nil {
		return nil, err
	}
	// The per-repo endpoint omits the repo on each task (it is in the
	// URL), while the cross-repo one includes it. Filling it in here
	// means every renderer below can print it unconditionally.
	if repo != "" {
		for i := range tasks {
			if tasks[i].Repo == "" {
				tasks[i].Repo = repo
			}
		}
	}
	return tasks, nil
}

// findByKey resolves a human key ("DEN-14") to the task behind it.
//
// The API addresses tasks by opaque id; keys are what people say out
// loud and what commit messages carry, so every write tool takes a key
// and resolves it here. Matching is case-insensitive because nobody
// types DEN-14 the same way twice.
func (t *toolset) findByKey(repo, key string) (task, error) {
	if key == "" {
		return task{}, fmt.Errorf("görev anahtarı gerekli, örneğin DEN-14")
	}
	tasks, err := t.fetchTasks(repo)
	if err != nil {
		return task{}, err
	}
	for _, candidate := range tasks {
		if strings.EqualFold(candidate.Key, key) {
			return candidate, nil
		}
	}
	where := "erişebildiğin repolarda"
	if repo != "" {
		where = repo + " reposunda"
	}
	return task{}, fmt.Errorf("%s %s bulunamadı", where, key)
}

// nameTable turns subjects into readable names.
type nameTable map[string]string

func (n nameTable) of(subject string) string {
	if subject == "" {
		return "atanmamış"
	}
	if name, ok := n[subject]; ok && name != "" {
		return name
	}
	return subject
}

// peopleNames is best-effort: a report with raw subjects in it is worth
// more than no report, so a failure here degrades rather than propagates.
func (t *toolset) peopleNames() nameTable {
	var people []person
	if err := t.api.get("/api/users", &people); err != nil {
		return nameTable{}
	}
	table := make(nameTable, len(people))
	for _, p := range people {
		switch {
		case p.DisplayName != "":
			table[p.Subject] = p.DisplayName
		case p.Email != "":
			table[p.Subject] = p.Email
		}
	}
	return table
}

// sortTasks puts the urgent end first and, among equals, the thing that
// has waited longest — the same order the board itself uses.
func sortTasks(tasks []task) {
	rank := map[string]int{"critical": 0, "high": 1, "normal": 2, "low": 3}
	sort.SliceStable(tasks, func(i, j int) bool {
		if rank[tasks[i].Priority] != rank[tasks[j].Priority] {
			return rank[tasks[i].Priority] < rank[tasks[j].Priority]
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
}

// overdue mirrors taskboard.Task.Overdue: finished work is never overdue,
// because chasing something already delivered is noise.
func overdue(t task, today string) bool {
	return t.DueDate != "" && t.Status != "done" && t.DueDate < today
}

func statusName(status string) string {
	if name, ok := statusNames[status]; ok {
		return name
	}
	return status
}

func priorityName(priority string) string {
	if name, ok := priorityNames[priority]; ok {
		return name
	}
	if priority == "" {
		return "normal"
	}
	return priority
}

func shortHash(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}
