// Package notify persists per-user notifications and provides a minimal
// email-sending interface for future wiring. It groups records by
// recipient subject the same way internal/taskboard groups records by
// repository — one JSON file per notification under a per-recipient
// subdirectory.
package notify

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var (
	ErrInvalidRecipient = errors.New("notify: invalid recipient")
	ErrInvalidID        = errors.New("notify: invalid notification id")
	ErrNotFound         = errors.New("notify: not found")
)

// validRecipientChars mirrors taskboard's validRepoName-style defensive
// validation. Recipients are opaque subjects from a JWT's "sub" claim
// (attacker-influenced input) that end up in a filesystem path, so they're
// validated the same way a repo name is before ever being joined into one.
// Unlike taskboard's repo-name charset, this one includes "." and "@" so
// email-shaped subjects are accepted — which is exactly why "." and ".."
// need the explicit reject below: both are made entirely of characters this
// class allows, but filepath.Join treats them as "this directory" and
// "parent directory" rather than as literal filenames.
var validRecipientChars = regexp.MustCompile(`^[a-zA-Z0-9_.@-]+$`)

// idPattern matches only IDs this package itself generates (see newID), so
// an ID coming from a URL path parameter can be validated before it is
// ever joined into a filesystem path.
var idPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// validRecipient reports whether recipient is safe to join into a
// filesystem path: it must match validRecipientChars, and it must not be
// "." or ".." — both pass the character-class check on their own but are
// special path segments (current/parent directory) that would otherwise let
// recipient="." or ".." escape the intended per-recipient subdirectory
// entirely, independent of whatever id is joined after it.
func validRecipient(recipient string) bool {
	if !validRecipientChars.MatchString(recipient) {
		return false
	}
	return recipient != "." && recipient != ".."
}

// Notification is a single per-user notification.
type Notification struct {
	ID        string    `json:"id"`
	Recipient string    `json:"recipient"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	Link      string    `json:"link"`
	Read      bool      `json:"read"`
	CreatedAt time.Time `json:"createdAt"`
}

// Store persists notifications as one JSON file per notification under
// rootDir, grouped in a per-recipient subdirectory — the same flat-file
// approach taskboard and mergerequest already use.
type Store struct {
	rootDir string
}

// NewStore returns a Store rooted at rootDir. rootDir does not need to
// exist yet.
func NewStore(rootDir string) *Store {
	return &Store{rootDir: rootDir}
}

// Create persists a new, unread notification for recipient and returns it
// with its generated ID and CreatedAt populated.
func (s *Store) Create(recipient, kind, message, link string) (Notification, error) {
	if !validRecipient(recipient) {
		return Notification{}, ErrInvalidRecipient
	}

	dir := filepath.Join(s.rootDir, recipient)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Notification{}, err
	}

	n := Notification{
		Recipient: recipient,
		Kind:      kind,
		Message:   message,
		Link:      link,
		Read:      false,
		CreatedAt: time.Now().UTC(),
	}

	// Retry on the astronomically unlikely chance a random ID collides
	// with an existing file; O_EXCL makes the check-then-create atomic.
	for attempt := 0; attempt < 5; attempt++ {
		id, err := newID()
		if err != nil {
			return Notification{}, err
		}
		n.ID = id

		path := filepath.Join(dir, id+".json")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return Notification{}, err
		}
		err = json.NewEncoder(f).Encode(n)
		closeErr := f.Close()
		if err != nil {
			return Notification{}, err
		}
		if closeErr != nil {
			return Notification{}, closeErr
		}
		return n, nil
	}
	return Notification{}, fmt.Errorf("notify: failed to allocate a unique id after 5 attempts")
}

// ListForUser returns every notification for recipient, newest first.
func (s *Store) ListForUser(recipient string) ([]Notification, error) {
	if !validRecipient(recipient) {
		return nil, ErrInvalidRecipient
	}

	dir := filepath.Join(s.rootDir, recipient)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Notification{}, nil
		}
		return nil, err
	}

	notifications := []Notification{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var n Notification
		if err := json.Unmarshal(data, &n); err != nil {
			return nil, err
		}
		notifications = append(notifications, n)
	}

	sort.Slice(notifications, func(i, j int) bool {
		return notifications[i].CreatedAt.After(notifications[j].CreatedAt)
	})
	return notifications, nil
}

// MarkRead marks the notification identified by (recipient, id) as read.
// The file path is resolved from both recipient and id — never from id
// alone — which is what makes "can't mark another user's notification
// read" structurally true rather than an access-check that could be
// forgotten, the same defense-in-depth reasoning repostore's name
// validation used.
func (s *Store) MarkRead(recipient, id string) error {
	path, err := s.path(recipient, id)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}

	var n Notification
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	n.Read = true

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	err = json.NewEncoder(f).Encode(n)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func (s *Store) path(recipient, id string) (string, error) {
	if !validRecipient(recipient) {
		return "", ErrInvalidRecipient
	}
	if !idPattern.MatchString(id) {
		return "", ErrInvalidID
	}
	return filepath.Join(s.rootDir, recipient, id+".json"), nil
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// EmailSender abstracts sending an email so a real SMTP implementation can
// be swapped in later without touching call sites. NoopEmailSender is the
// default: it logs what it would have sent and does not actually send
// anything, matching this plan's explicit scope (see the plan's Global
// Constraints — no real SMTP send yet, config values have nowhere real to
// point until a future plan wires one).
type EmailSender interface {
	Send(to, subject, body string) error
}

type NoopEmailSender struct{}

func (NoopEmailSender) Send(to, subject, body string) error {
	log.Printf("notify: (no-op email sender) would send to=%q subject=%q", to, subject)
	return nil
}
