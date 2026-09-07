// Command seeddemo fills a LOCAL development data directory with
// believable content — repositories with real git history, branches,
// review requests, tasks spread across the board — so the panel's screens
// can be looked at with something in them. An empty platform shows every
// screen in its least informative state, which is exactly the state you
// cannot design against.
//
// It is a development tool, not part of the server: nothing in
// cmd/devplatform imports it, and it writes only to the data directory it
// is pointed at.
//
// It refuses to touch a data directory that already holds repositories
// unless -force is given. Getting this pointed at a real deployment would
// mean seeding a production platform with made-up projects, and the guard
// costs one flag.
//
// Usage, from the backend directory with the dev server stopped:
//
//	go run ./cmd/seeddemo -data ./data
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
	"github.com/kenissha/DevPlatform/backend/internal/repodesc"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/taskboard"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

// person is one of the fictional colleagues the demo data is attributed
// to. Subject is what the platform stores; Name and Email are what git
// stamps on a commit.
type person struct {
	Subject string
	Name    string
	Email   string
}

// The first entry is the account a local developer logs in as (see
// frontend/src/auth/devToken.ts), so their own dashboard and contribution
// graph have something in them rather than being the one empty screen.
var (
	me    = person{Subject: "dev", Name: "Rifat Öztürk", Email: "dev@localhost"}
	ahmet = person{Subject: "ahmet", Name: "Ahmet Yılmaz", Email: "ahmet@localhost"}
	elif  = person{Subject: "elif", Name: "Elif Kaya", Email: "elif@localhost"}
)

type demoRepo struct {
	Name        string
	Description string
	Commits     int
}

var demoRepos = []demoRepo{
	{"deneme", "Deneme deposu — panelin kendi denemeleri için", 14},
	{"oasrapor-frontend", "Hakem raporu ekranlarının React arayüzü", 22},
	{"intranet-servis", "AD entegrasyonu ve ortak servis katmanı", 9},
}

// Branches every demo repo gets, each one commit ahead of main so the
// branch pages and review requests have a real diff to show.
var demoBranches = []struct {
	Name    string
	File    string
	Content string
	Message string
}{
	{"feature/hakem-raporlari", "rapor.md", "hakem raporu taslağı\n", "feat: hakem raporu ekranının ilk hâli"},
	{"feature/pdf-disa-aktarim", "pdf.md", "pdf çıktısı\n", "feat: raporu PDF olarak dışa aktar"},
	{"bugfix/oturum-zaman-asimi", "config.md", "timeout: 60s\n", "fix: giriş zaman aşımını 60sn'ye çek"},
}

var commitMessages = []string{
	"feat: rapor listesine tarih filtresi ekle",
	"fix: boş sonuçta tablo başlığı kayboluyordu",
	"refactor: rapor servisini ayrı modüle taşı",
	"feat: dosya yükleme boyut sınırı",
	"fix: Türkçe karakterler PDF'te bozuluyordu",
	"chore: bağımlılıkları güncelle",
	"feat: hakem atama ekranı",
	"test: rapor servisi için birim testleri",
	"fix: sayfalama son sayfada takılıyordu",
	"docs: kurulum adımlarını güncelle",
	"feat: excel dışa aktarım",
	"perf: liste sorgusunu indeksle",
}

type demoTask struct {
	Repo        string
	Title       string
	Description string
	Author      person
	AssignedTo  person
	Status      taskboard.Status
	Urgent      bool
}

// Deliberately lopsided: more waiting than finished, a couple unassigned,
// two urgent. A board with one card per column looks designed rather than
// used, and tells you nothing about how it holds up when it fills.
var demoTasks = []demoTask{
	{"deneme", "Rapor filtrelerini tasarla", "Tarih aralığı, hakem ve durum filtresi.", me, me, taskboard.StatusTodo, false},
	{"deneme", "Excel dışa aktarım", "Liste ekranındaki veriyi xlsx olarak indir.", ahmet, ahmet, taskboard.StatusTodo, false},
	{"deneme", "PDF şablonunu güncelle", "Yeni antet ve imza alanı eklenecek.", me, elif, taskboard.StatusInProgress, false},
	{"deneme", "Hakem atama ekranı", "Toplu atama da olmalı.", ahmet, ahmet, taskboard.StatusAwaitingTest, false},
	{"deneme", "Giriş zaman aşımı", "VPN üzerinden AD 22 saniye sürüyor.", me, me, taskboard.StatusDone, false},

	{"oasrapor-frontend", "Mobil görünüm", "Tablo dar ekranda taşıyor.", me, elif, taskboard.StatusTodo, false},
	{"oasrapor-frontend", "Yükleme göstergesi", "Uzun sorgularda boş ekran görünüyor.", me, person{}, taskboard.StatusTodo, false},
	{"oasrapor-frontend", "Üretimde 500 hatası", "Rapor detayında aralıklı olarak patlıyor.", elif, elif, taskboard.StatusInProgress, true},
	{"oasrapor-frontend", "Karanlık mod", "Panel ile aynı token seti kullanılacak.", me, me, taskboard.StatusInProgress, false},
	{"oasrapor-frontend", "Erişilebilirlik taraması", "Klavye ile gezinme çalışmıyor.", elif, elif, taskboard.StatusAwaitingTest, false},
	{"oasrapor-frontend", "Bağımlılık güncellemesi", "Vite 8 geçişi.", me, me, taskboard.StatusDone, false},

	{"intranet-servis", "AD bağlantı havuzu", "Her istekte yeni bind açılıyor.", me, ahmet, taskboard.StatusInProgress, true},
	{"intranet-servis", "Servis sağlık ucu", "/healthz eklenecek.", ahmet, ahmet, taskboard.StatusTodo, false},
	{"intranet-servis", "Log formatını birleştir", "JSON satır formatı.", me, me, taskboard.StatusDone, false},
}

var demoRequests = []struct {
	Repo   string
	Title  string
	Source string
	Author person
}{
	{"deneme", "Hakem raporu ekranı bitti", "feature/hakem-raporlari", ahmet},
	{"oasrapor-frontend", "PDF dışa aktarım hazır", "feature/pdf-disa-aktarim", elif},
	{"oasrapor-frontend", "Oturum zaman aşımı düzeltmesi", "bugfix/oturum-zaman-asimi", ahmet},
}

func main() {
	dataDir := flag.String("data", "./data", "development data directory to fill")
	force := flag.Bool("force", false, "seed even if the data directory already holds repositories")
	flag.Parse()

	if _, err := exec.LookPath("git"); err != nil {
		log.Fatal("git bulunamadı — bu araç commit geçmişini gerçek git ile oluşturuyor")
	}

	// Resolved up front because seedHistory pushes from a scratch worktree
	// in the temp directory: a relative data dir ("./data") would be
	// resolved against *that* directory, and git would report the bare
	// repository as not existing.
	root, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("veri klasörü yolu çözülemedi: %v", err)
	}
	dataDir = &root

	repos := repostore.New(*dataDir)
	existing, err := repos.List()
	if err != nil {
		log.Fatalf("veri klasörü okunamadı: %v", err)
	}
	if len(existing) > 0 && !*force {
		log.Fatalf("%q zaten %d repo içeriyor. Yanlış klasöre demo verisi yazmamak için duruyorum; "+
			"gerçekten istiyorsan -force ekle.", *dataDir, len(existing))
	}

	descriptions := repodesc.NewStore(filepath.Join(*dataDir, "repo-descriptions.json"))
	tasks := taskboard.NewStore(filepath.Join(*dataDir, "tasks"))
	requests := mergerequest.NewStore(filepath.Join(*dataDir, "merge-requests"))
	registry := users.NewStore(filepath.Join(*dataDir, "users.json"))

	// The assignee picker reads this registry, and it is normally filled
	// just-in-time as people log in. Nobody is going to log in as these
	// three, so they are recorded here or every demo task would be
	// assigned to a name the picker has never heard of.
	for _, p := range []person{me, ahmet, elif} {
		role := "developer"
		if p.Subject == me.Subject {
			role = "admin"
		}
		if _, err := registry.Upsert(p.Subject, p.Email, role); err != nil {
			log.Fatalf("%s kaydedilemedi: %v", p.Subject, err)
		}
	}

	for ri, r := range demoRepos {
		path, err := repos.Create(r.Name)
		if err != nil {
			log.Fatalf("%s oluşturulamadı: %v", r.Name, err)
		}
		if err := descriptions.Set(r.Name, r.Description); err != nil {
			log.Fatalf("%s açıklaması yazılamadı: %v", r.Name, err)
		}
		if err := seedHistory(path, r.Commits, ri); err != nil {
			log.Fatalf("%s geçmişi oluşturulamadı: %v", r.Name, err)
		}
		fmt.Printf("  %-20s %d commit, %d branch\n", r.Name, r.Commits, len(demoBranches)+1)
	}

	for _, t := range demoTasks {
		task, err := tasks.Create(t.Repo, t.Title, t.Description, t.AssignedTo.Subject, t.Author.Subject)
		if err != nil {
			log.Fatalf("görev oluşturulamadı (%s): %v", t.Title, err)
		}
		// Create always starts a task in StatusTodo, so anything further
		// along the board is moved here rather than written directly —
		// same path the panel takes.
		if t.Status != taskboard.StatusTodo || t.Urgent {
			status, urgent := t.Status, t.Urgent
			if _, err := tasks.Update(t.Repo, task.ID, taskboard.Changes{Status: &status, Urgent: &urgent}); err != nil {
				log.Fatalf("görev güncellenemedi (%s): %v", t.Title, err)
			}
		}
	}
	fmt.Printf("  %d görev\n", len(demoTasks))

	for _, r := range demoRequests {
		if _, err := requests.Create(r.Repo, r.Title, r.Source, "main", r.Author.Subject); err != nil {
			log.Fatalf("inceleme isteği oluşturulamadı (%s): %v", r.Title, err)
		}
	}
	fmt.Printf("  %d inceleme isteği\n", len(demoRequests))

	fmt.Println("\nBitti. Sunucuyu başlatıp panele bakabilirsin.")
}

// seedHistory builds real commit history in a scratch worktree and pushes
// it into the bare repository at repoPath.
//
// Over a filesystem remote, not HTTP: that skips the credential helper and
// — deliberately — main's branch protection, which exists to gate human
// pushes, not the fixture that creates the branch in the first place.
func seedHistory(repoPath string, commits, offset int) error {
	work, err := os.MkdirTemp("", "seeddemo-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	if err := git(work, "init", "-q", "-b", "main"); err != nil {
		return err
	}

	for i := 1; i <= commits; i++ {
		author := authorFor(i)
		line := fmt.Sprintf("%d. %s\n", i, commitMessages[(i-1)%len(commitMessages)])
		if err := appendTo(filepath.Join(work, "CHANGELOG.md"), line); err != nil {
			return err
		}
		if err := git(work, "add", "-A"); err != nil {
			return err
		}
		if err := commitAs(work, author, commitDate(i, offset), commitMessages[(i-1)%len(commitMessages)]); err != nil {
			return err
		}
	}

	for bi, b := range demoBranches {
		if err := git(work, "checkout", "-q", "-b", b.Name, "main"); err != nil {
			return err
		}
		if err := appendTo(filepath.Join(work, b.File), b.Content); err != nil {
			return err
		}
		if err := git(work, "add", "-A"); err != nil {
			return err
		}
		if err := commitAs(work, authorFor(bi+offset), commitDate(bi+1, offset), b.Message); err != nil {
			return err
		}
	}

	if err := git(work, "checkout", "-q", "main"); err != nil {
		return err
	}
	if err := git(work, "remote", "add", "origin", repoPath); err != nil {
		return err
	}
	return git(work, "push", "-q", "--all", "origin")
}

// commitDate spreads history backwards over roughly nine weeks.
//
// Two things matter here, and both are about the contribution heatmap,
// which shades each day against the busiest one. The stride is coprime
// with the range so commits scatter across days instead of stacking on a
// handful; and `offset` differs per repository so the repos don't land on
// exactly the same days — with a shared schedule every populated day held
// precisely one commit per repo, which rendered as a single flat shade and
// showed nothing about how the graph reads when activity varies.
func commitDate(i, offset int) time.Time {
	const spread = 63
	days := (i*7 + offset*3) % spread
	return time.Now().AddDate(0, 0, -days).Add(-time.Duration(i%9) * time.Hour)
}

// authorFor rotates through the three colleagues so the contributors list
// and the "who did what" panels have more than one name in them.
func authorFor(i int) person {
	switch i % 5 {
	case 0:
		return ahmet
	case 3:
		return elif
	default:
		return me
	}
}

func appendTo(path, content string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// commitAs sets author and committer identity and date through the
// environment rather than `git config`, so each commit can carry a
// different person and timestamp without rewriting the repo's config
// between every one.
func commitAs(dir string, p person, when time.Time, message string) error {
	stamp := when.Format(time.RFC3339)
	cmd := exec.Command("git", "-C", dir, "commit", "-q", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME="+p.Name,
		"GIT_AUTHOR_EMAIL="+p.Email,
		"GIT_AUTHOR_DATE="+stamp,
		"GIT_COMMITTER_NAME="+p.Name,
		"GIT_COMMITTER_EMAIL="+p.Email,
		"GIT_COMMITTER_DATE="+stamp,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %v: %s", err, out)
	}
	return nil
}

func git(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %v: %s", args, err, out)
	}
	return nil
}
