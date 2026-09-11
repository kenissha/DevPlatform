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

	if err := moveIntoPlace(from, to); err != nil {
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

	err := moveIntoPlace(from, to)
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
	go func() { done <- moveIntoPlace(from, to) }()

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

	denied := describeMoveFailure(ErrMoveDenied, staging, target)
	for _, want := range []string{staging, target, "kaybolmadı", "virüs"} {
		if !strings.Contains(denied, want) {
			t.Errorf("mesajda %q geçmiyor: %s", want, denied)
		}
	}
}
