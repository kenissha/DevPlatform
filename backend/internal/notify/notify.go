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
	"strings"
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

// EmailLookup resolves a notification recipient (a bare JWT subject, e.g.
// "dev-1") to the email address to send real mail to. Typically
// users.Store.Get adapted to this shape — kept as a plain function type
// rather than an interface importing internal/users, so this package
// doesn't need to depend on that one just to describe the one method it
// calls.
type EmailLookup func(recipient string) (email string, ok bool)

// Store persists notifications as one JSON file per notification under
// rootDir, grouped in a per-recipient subdirectory — the same flat-file
// approach taskboard and mergerequest already use.
type Store struct {
	rootDir string

	// Sender and LookupEmail are both optional and both public fields
	// (matching this codebase's Handlers structs, which configure their
	// own optional collaborators — audit.Logger, notify.Store itself — the
	// same way rather than via constructor parameters or setters). Real
	// mail is only ever attempted when both are set: LookupEmail is what
	// turns a bare recipient subject into somewhere to send it, so one
	// without the other can never do anything useful. Set from main.go
	// once a real SMTPEmailSender exists; nil in every test and in any
	// deployment that hasn't configured DEVPLATFORM_SMTP_HOST.
	Sender      EmailSender
	LookupEmail EmailLookup
	// BaseURL, if set, is prefixed onto a notification's Link when it's
	// included in an outgoing email, turning a relative frontend path like
	// "/repos/x/tasks" into a clickable absolute URL. The in-app
	// notification itself always stores the relative Link unchanged — this
	// only affects the copy of it that goes out as mail.
	BaseURL string
}

// NewStore returns a Store rooted at rootDir, with no email sending
// configured (Sender/LookupEmail are left nil — set them directly on the
// returned Store to enable it). rootDir does not need to exist yet.
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
		s.sendEmail(n)
		return n, nil
	}
	return Notification{}, fmt.Errorf("notify: failed to allocate a unique id after 5 attempts")
}

// sendEmail best-effort mirrors n out as real mail, if both Sender and
// LookupEmail are configured. Errors are logged, not returned: the in-app
// notification n already persisted successfully by the time this runs, and
// a colleague's SMTP server being briefly unreachable must not be reported
// back to the caller as "creating the notification failed" — the two are
// different operations that happen to share one call for convenience.
func (s *Store) sendEmail(n Notification) {
	if s.Sender == nil || s.LookupEmail == nil {
		return
	}
	email, ok := s.LookupEmail(n.Recipient)
	if !ok || email == "" {
		return
	}
	// The subject line is a fixed string, never built from n.Message/Link —
	// both can contain arbitrary text (a task title, a branch name) that
	// this package has no reason to trust as header-safe. Keeping every
	// dynamic value in the body instead of a header sidesteps header
	// injection entirely rather than needing to sanitize it.
	body := n.Message
	if n.Link != "" {
		link := n.Link
		if s.BaseURL != "" && strings.HasPrefix(link, "/") {
			link = strings.TrimRight(s.BaseURL, "/") + link
		}
		body += "\n\n" + link
	}
	if err := s.Sender.Send(email, "DevPlatform bildirimi", body); err != nil {
		log.Printf("notify: failed to send email to %q: %v", email, err)
	}
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
