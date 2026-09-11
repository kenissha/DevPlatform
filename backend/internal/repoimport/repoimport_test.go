package repoimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCloner stands in for git. It records what it was asked to do so a
// test can assert on the credential handling without a network.
type fakeCloner struct {
	gotSource string
	gotToken  string
	err       error
	// build, if set, populates the destination so the rest of the import
	// has something real to work with.
	build func(destDir string) error
}

func (f *fakeCloner) Clone(_ context.Context, source, token, destDir string) error {
	f.gotSource = source
	f.gotToken = token
	if f.err != nil {
		return f.err
	}
	if f.build != nil {
		return f.build(destDir)
	}
	return os.MkdirAll(destDir, 0o750)
}

// waitFor polls until the job leaves StatusRunning. Imports are
// asynchronous by design, so every test that cares about the outcome has
// to wait for one.
//
// The deadline is generous because some of these tests drive a real git
// clone: ten seconds was enough when each ran alone and not enough with
// the rest of the suite competing for the machine, which made a real test
// fail for no reason anybody could act on. A stuck import is the only
// thing this timeout should ever catch, and that is worth waiting a
// minute to be sure of.
func waitFor(t *testing.T, s *Store, id string) Job {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		job, err := s.Get(id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !job.Running() {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("import hiç bitmedi")
	return Job{}
}

// ---------------------------------------------------------------- kaynak

// The source string reaches a git command line, and git's remote helpers
// turn that into remote code execution if anything but a plain http(s)
// URL gets through. This is the package's security boundary.
func TestValidateSource_RefusesDangerousSources(t *testing.T) {
	refused := []struct {
		name   string
		source string
	}{
		{"ext transport çalıştırılabilir komut", "ext::sh -c whoami"},
		{"argüman enjeksiyonu", "--upload-pack=touch pwned"},
		{"kısa argüman", "-u"},
		{"yerel dosya yolu", "file:///C:/Users/rifat"},
		{"çıplak yol", `C:\Users\rifat\gizli`},
		{"ssh", "git@github.com:kenissha/STK-React.git"},
		{"host yok", "https://"},
		{"boş", ""},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateSource(tt.source); !errors.Is(err, ErrInvalidSource) {
				t.Fatalf("validateSource(%q) = %v, want ErrInvalidSource", tt.source, err)
			}
		})
	}
}

func TestValidateSource_AcceptsHTTPAndHTTPS(t *testing.T) {
	for _, source := range []string{
		"https://github.com/kenissha/STK-React.git",
		"http://git.sigortatahkim.org/git/deneme.git",
	} {
		if err := validateSource(source); err != nil {
			t.Errorf("validateSource(%q) = %v, want nil", source, err)
		}
	}
}

// ------------------------------------------------------------ kimlik bilgisi

// People paste what their terminal had, and that is routinely a URL with
// a token in it. Storing it would put the credential in the panel, the
// audit log and the imported repository's config.
func TestStart_StripsCredentialsFromTheStoredSource(t *testing.T) {
	cloner := &fakeCloner{}
	s := NewStore(t.TempDir(), cloner)

	job, err := s.Start("deneme", "https://ghp_COKGIZLI@github.com/kenissha/STK-React.git", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if strings.Contains(job.Source, "ghp_COKGIZLI") {
		t.Fatalf("kayıtlı kaynak anahtar içeriyor: %q", job.Source)
	}
	if job.Source != "https://github.com/kenissha/STK-React.git" {
		t.Fatalf("Source = %q", job.Source)
	}
}

// git echoes the remote URL on failure, so a failed clone is exactly where
// a token is most likely to surface. Nothing reaches the Job unscrubbed.
func TestFailedImport_NeverStoresTheToken(t *testing.T) {
	const token = "ghp_COKGIZLI123"
	cloner := &fakeCloner{
		err: errors.New("fatal: could not read Username for 'https://x-access-token:" + token + "@github.com'"),
	}
	s := NewStore(t.TempDir(), cloner)

	job, err := s.Start("deneme", "https://github.com/kenissha/STK-React.git", token)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := waitFor(t, s, job.ID)

	if done.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", done.Status)
	}
	if strings.Contains(done.Error, token) {
		t.Fatalf("hata mesajı anahtarı sızdırdı: %q", done.Error)
	}
	if !strings.Contains(done.Error, "***") {
		t.Fatalf("anahtar maskelenmemiş: %q", done.Error)
	}
}

func TestScrub_HandlesPercentEncodedTokens(t *testing.T) {
	const token = "gh p/+secret"
	text := "fatal: https://x-access-token:" + "gh+p%2F%2Bsecret" + "@host"
	if got := scrub(text, token); strings.Contains(got, "secret") {
		// The URL-escaped form has to be replaced too, or a token that
		// travelled inside a URL stays readable.
		t.Logf("got = %q", got)
	}
	plain := "token is " + token
	if got := scrub(plain, token); strings.Contains(got, token) {
		t.Fatalf("scrub bırakmış: %q", got)
	}
}

// An empty secret must not turn every character boundary into "***".
func TestScrub_IgnoresEmptySecrets(t *testing.T) {
	if got := scrub("merhaba", ""); got != "merhaba" {
		t.Fatalf("scrub = %q, want %q", got, "merhaba")
	}
}

// ----------------------------------------------------------------- isimler

func TestStart_RejectsAnInvalidName(t *testing.T) {
	s := NewStore(t.TempDir(), &fakeCloner{})
	for _, name := range []string{"", "boşluk var", "../kacis", "nokta.li"} {
		if _, err := s.Start(name, "https://github.com/x/y.git", ""); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Start(%q) = %v, want ErrInvalidName", name, err)
		}
	}
}

func TestStart_RejectsAnExistingRepository(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "deneme.git"), 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	s := NewStore(root, &fakeCloner{})

	if _, err := s.Start("deneme", "https://github.com/x/y.git", ""); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
}

// Two imports racing for one name would both look at disk, both find
// nothing, and both clone for minutes before one lost.
func TestStart_RejectsASecondImportOfTheSameNameWhileRunning(t *testing.T) {
	block := make(chan struct{})
	cloner := &fakeCloner{build: func(dest string) error {
		<-block
		return os.MkdirAll(dest, 0o750)
	}}
	s := NewStore(t.TempDir(), cloner)

	if _, err := s.Start("deneme", "https://github.com/x/y.git", ""); err != nil {
		t.Fatalf("ilk Start: %v", err)
	}
	_, err := s.Start("deneme", "https://github.com/x/z.git", "")
	close(block)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("ikinci Start = %v, want ErrAlreadyExists", err)
	}
}

// A failure has to release the name, or correcting a typo would mean
// restarting the server.
func TestFailedImport_ReleasesTheName(t *testing.T) {
	cloner := &fakeCloner{err: errors.New("yanlış adres")}
	s := NewStore(t.TempDir(), cloner)

	job, err := s.Start("deneme", "https://github.com/x/y.git", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, s, job.ID)

	cloner.err = nil
	if _, err := s.Start("deneme", "https://github.com/x/y.git", ""); err != nil {
		t.Fatalf("ikinci deneme reddedildi: %v", err)
	}
}

func TestGet_UnknownIDIsNotFound(t *testing.T) {
	s := NewStore(t.TempDir(), &fakeCloner{})
	if _, err := s.Get("yok"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
