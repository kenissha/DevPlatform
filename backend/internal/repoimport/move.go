package repoimport

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Moving the finished clone into place is the last step and, on the
// deployment machine, the one that fails.
//
// Windows reports two completely different problems as the same
// "Access is denied" on a directory rename:
//
//   - the destination already exists (os.Rename will not replace a
//     directory), and
//   - something still holds a handle inside the source — which, here, is
//     the on-access virus scanner working through the several thousand
//     files git just wrote.
//
// internal/atomicfile already exists for the second cause, but its eight
// 15ms attempts are tuned for replacing one small JSON file. A freshly
// cloned repository keeps a scanner busy for a great deal longer, so this
// waits on a scale that matches what it is waiting for — which is still
// nothing next to the minutes the clone itself took.

const (
	// moveTimeout bounds the wait. Long enough for a scanner to work
	// through a large repository, short enough that a genuine permission
	// problem is reported while somebody is still watching.
	moveTimeout = 90 * time.Second
	// moveInitialDelay backs off from here, doubling up to moveMaxDelay.
	// Starting short keeps the common case (no lock at all, or one that
	// clears immediately) imperceptible.
	moveInitialDelay = 100 * time.Millisecond
	moveMaxDelay     = 3 * time.Second
)

// ErrMoveDenied reports a staged clone that could not be put in place.
// Its message names both causes, because the operating system's does not.
var ErrMoveDenied = errors.New("repoimport: staged repository could not be moved into place")

// moveIntoPlace moves the completed clone from staging to its final path.
//
// Returns ErrAlreadyExists when the destination turned up in the meantime.
// Store.Start checks this before cloning, but minutes pass in between —
// somebody can create the repository from the panel while the clone runs,
// and finding out now is better than overwriting their work.
func moveIntoPlace(id, from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return ErrAlreadyExists
	}

	deadline := time.Now().Add(moveTimeout)
	delay := moveInitialDelay
	var lastErr error

	// Every attempt is logged, not just the last. If the error changes
	// between attempts — a lock clearing, a different failure appearing —
	// that sequence is the evidence, and summarising it away is how two
	// rounds of diagnosis went to the wrong cause.
	for attempt := 1; ; attempt++ {
		if lastErr = os.Rename(from, to); lastErr == nil {
			if attempt > 1 {
				log.Printf("repoimport[%s]: taşıma %d. denemede başarılı", id, attempt)
			}
			return nil
		}
		log.Printf("repoimport[%s]: taşıma denemesi %d başarısız: %v", id, attempt, lastErr)

		// Re-checked every round rather than once: a racing creation is
		// not something waiting will fix, and retrying for 90 seconds
		// against it wastes a minute and a half to reach a worse message.
		if _, err := os.Stat(to); err == nil {
			return ErrAlreadyExists
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %v", ErrMoveDenied, lastErr)
		}
		time.Sleep(delay)
		if delay < moveMaxDelay {
			delay *= 2
		}
	}
}

// describeMoveFailure turns a move failure into something a person can
// act on, and says where the downloaded repository is.
//
// The staged clone is deliberately kept when this happens. It is the
// expensive part — potentially hundreds of megabytes already pulled over
// the network — and throwing it away would turn a rename somebody can fix
// with one command into a full re-download. It is inert where it sits:
// the directory has no ".git" suffix, so repostore.List does not see it.
func describeMoveFailure(err error, staging, target string) string {
	if errors.Is(err, ErrAlreadyExists) {
		return fmt.Sprintf(
			"bu adda bir depo zaten var: %s. Kopyalanan depo %s dizininde duruyor; "+
				"başka bir adla tekrar deneyebilir ya da bu dizini silebilirsin.",
			target, staging)
	}
	return fmt.Sprintf(
		"depo yerine taşınamadı (%v). Depo indirildi ve %s dizininde duruyor — "+
			"kaybolmadı; sunucuda bu dizini elle %s adına taşımak yeterli.\n\n%s",
		err, staging, target, diagnoseRename(filepath.Dir(staging)))
}

// diagnoseRename finds out what this process can actually do in rootDir.
//
// Written after guessing twice and being wrong twice. "Access is denied"
// on a directory rename has several possible causes and Windows names
// none of them, so the two that matter are separated here by trying them:
//
//   - if creating a directory works but renaming it does not, the service
//     account can add to this folder but not delete from it. That is an
//     NTFS permission (rename needs DELETE on the object, which "Write"
//     grants and "Modify" is usually needed for), and no amount of
//     retrying will ever fix it.
//   - if the probe renames cleanly, the problem is specific to the staged
//     clone — something holding a handle inside it — and retrying or
//     moving it by hand will work.
//
// The probe is tiny and cleans up after itself. It runs only on the
// failure path, so it costs nothing when imports work.
func diagnoseRename(rootDir string) string {
	probe := filepath.Join(rootDir, ".probe-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	moved := probe + "-moved"

	if err := os.Mkdir(probe, 0o750); err != nil {
		return fmt.Sprintf(
			"Tanı: bu dizinde klasör bile oluşturulamıyor (%v). Uygulama havuzunun "+
				"kimliğine %s üzerinde yazma yetkisi verilmeli.", err, rootDir)
	}

	if err := os.Rename(probe, moved); err != nil {
		_ = os.Remove(probe)
		return fmt.Sprintf(
			"Tanı: bu dizinde klasör oluşturulabiliyor ama yeniden adlandırılamıyor (%v). "+
				"Bu bir NTFS yetki sorunu — uygulama havuzunun kimliğine %s üzerinde "+
				"\"Modify\" (Değiştir) yetkisi ver; sadece \"Write\" yetmiyor, yeniden "+
				"adlandırma silme yetkisi istiyor. Virüs taramasıyla ilgisi yok.",
			err, rootDir)
	}

	if err := os.Remove(moved); err != nil {
		return fmt.Sprintf(
			"Tanı: klasör oluşturulup adlandırılabiliyor ama silinemiyor (%v). "+
				"Yetkiler büyük ölçüde doğru; sorun kopyalanan depoya özgü görünüyor.", err)
	}

	return "Tanı: bu dizinde klasör oluşturmak, adlandırmak ve silmek çalışıyor — " +
		"yani yetkiler doğru ve sorun kopyalanan depoya özgü. Büyük ihtimalle bir " +
		"program o dizindeki dosyaları açık tutuyor; elle taşımak çalışacaktır."
}
