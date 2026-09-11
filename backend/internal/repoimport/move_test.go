package repoimport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMoveIntoPlace_MovesTheDirectory(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, ".import-abc")
	to := filepath.Join(root, "depo.git")
	if err := os.MkdirAll(filepath.Join(from, "objects"), 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := moveIntoPlace("test", from, to); err != nil {
		t.Fatalf("moveIntoPlace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, "objects")); err != nil {
		t.Fatalf("taşınmadı: %v", err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("kaynak duruyor")
	}
}

// Windows reports an existing destination as "Access is denied", the same
// as a file still held open — so the two have to be told apart here, or
// the person gets a message about virus scanners when the real answer is
// that the name is taken.
func TestMoveIntoPlace_ExistingDestinationIsReportedAsSuch(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, ".import-abc")
	to := filepath.Join(root, "depo.git")
	if err := os.MkdirAll(from, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(to, 0o750); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := moveIntoPlace("test", from, to)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
}

// An existing destination must not be waited on: retrying for 90 seconds
// against a name that is taken burns a minute and a half to arrive at a
// worse message.
func TestMoveIntoPlace_DoesNotWaitOnAnExistingDestination(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, ".import-abc")
	to := filepath.Join(root, "depo.git")
	_ = os.MkdirAll(from, 0o750)
	_ = os.MkdirAll(to, 0o750)

	done := make(chan error, 1)
	go func() { done <- moveIntoPlace("test", from, to) }()

	select {
	case err := <-done:
		if !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("var olan hedefte beklemeye girdi")
	}
}

// The message has to name both causes and say where the downloaded copy
// is — the clone is the expensive part and it is deliberately kept.
func TestDescribeMoveFailure_SaysWhereTheCloneIsAndWhatToDo(t *testing.T) {
	staging := `D:\inetpub\wwwroot\DevPlatform\data\.import-f7e6`
	target := `D:\inetpub\wwwroot\DevPlatform\data\OASRapor-GO.git`

	exists := describeMoveFailure(ErrAlreadyExists, staging, target)
	if !strings.Contains(exists, "zaten var") || !strings.Contains(exists, staging) {
		t.Errorf("var olan hedef mesajı: %s", exists)
	}

	// The message no longer guesses at a cause — it carries the probe's
	// answer instead, which is the whole point of diagnoseRename.
	denied := describeMoveFailure(ErrMoveDenied, staging, target)
	for _, want := range []string{staging, target, "kaybolmadı", "Tanı:"} {
		if !strings.Contains(denied, want) {
			t.Errorf("mesajda %q geçmiyor: %s", want, denied)
		}
	}
}

// The diagnostic has to distinguish a permission problem from a held
// handle, because those need opposite responses: one is fixed by an
// administrator changing an ACL, the other by moving a directory.
func TestDiagnoseRename_ReportsAWorkingDirectoryAsWorking(t *testing.T) {
	msg := diagnoseRename(t.TempDir())

	if !strings.Contains(msg, "çalışıyor") {
		t.Fatalf("çalışan dizin sorunlu bildirildi: %s", msg)
	}
	// Naming virus scanning here would send somebody after the wrong
	// thing — the point of the probe is that it already ruled that in or
	// out.
	if strings.Contains(msg, "NTFS") {
		t.Fatalf("çalışan dizin için yetki hatası bildirildi: %s", msg)
	}
}

// The probe must not leave anything behind — it runs in the directory
// every repository lives in.
func TestDiagnoseRename_CleansUpAfterItself(t *testing.T) {
	root := t.TempDir()
	diagnoseRename(root)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		t.Errorf("geride kaldı: %s", e.Name())
	}
}

func TestDiagnoseRename_MissingDirectoryIsReportedNotPanicked(t *testing.T) {
	msg := diagnoseRename(filepath.Join(t.TempDir(), "olmayan"))

	if !strings.Contains(msg, "Tanı:") {
		t.Fatalf("tanı üretmedi: %s", msg)
	}
}
