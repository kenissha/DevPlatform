package notify

import "testing"

func TestCreate_PersistsAndReturnsNotification(t *testing.T) {
	store := NewStore(t.TempDir())

	n, err := store.Create("dev-1", "task_assigned", "You were assigned a task", "/tasks/abc")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if n.ID == "" {
		t.Fatal("expected a generated ID")
	}
	if n.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if n.Read {
		t.Error("expected Read to default to false")
	}
	if n.Recipient != "dev-1" {
		t.Errorf("Recipient = %q, want %q", n.Recipient, "dev-1")
	}
	if n.Kind != "task_assigned" {
		t.Errorf("Kind = %q, want %q", n.Kind, "task_assigned")
	}
	if n.Message != "You were assigned a task" {
		t.Errorf("Message = %q, want %q", n.Message, "You were assigned a task")
	}
	if n.Link != "/tasks/abc" {
		t.Errorf("Link = %q, want %q", n.Link, "/tasks/abc")
	}
}

func TestCreate_RejectsInvalidRecipient(t *testing.T) {
	store := NewStore(t.TempDir())

	_, err := store.Create("../escape", "task_assigned", "msg", "")
	if err != ErrInvalidRecipient {
		t.Fatalf("err = %v, want ErrInvalidRecipient", err)
	}
}

// TestCreate_RejectsPathTraversalRecipient covers recipients made entirely
// of characters the charset allows ("." and "@" are permitted so
// email-shaped subjects work) but that filepath.Join treats specially:
// "." and ".." are "this directory"/"parent directory", not literal
// filenames, so joining rootDir with recipient="." or ".." doesn't land
// inside a new per-recipient subdirectory the way every other recipient
// does. TestCreate_RejectsInvalidRecipient above only proves "/" is
// rejected, which is a different character than the one that actually
// matters here.
func TestCreate_RejectsPathTraversalRecipient(t *testing.T) {
	store := NewStore(t.TempDir())

	for _, recipient := range []string{".", ".."} {
		_, err := store.Create(recipient, "task_assigned", "msg", "")
		if err != ErrInvalidRecipient {
			t.Errorf("Create(%q): err = %v, want ErrInvalidRecipient", recipient, err)
		}
	}
}

func TestListForUser_ReturnsOnlyThatUsersNotifications(t *testing.T) {
	store := NewStore(t.TempDir())

	if _, err := store.Create("dev-1", "task_assigned", "for dev-1", ""); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if _, err := store.Create("dev-2", "task_assigned", "for dev-2", ""); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	notifications, err := store.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser returned error: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifications))
	}
	if notifications[0].Recipient != "dev-1" {
		t.Errorf("Recipient = %q, want %q", notifications[0].Recipient, "dev-1")
	}
}

func TestListForUser_NewestFirst(t *testing.T) {
	store := NewStore(t.TempDir())

	first, err := store.Create("dev-1", "task_assigned", "first", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	second, err := store.Create("dev-1", "task_assigned", "second", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	notifications, err := store.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser returned error: %v", err)
	}
	if len(notifications) != 2 {
		t.Fatalf("got %d notifications, want 2", len(notifications))
	}
	if notifications[0].ID != second.ID || notifications[1].ID != first.ID {
		t.Errorf("expected newest-first order [%s, %s], got [%s, %s]",
			second.ID, first.ID, notifications[0].ID, notifications[1].ID)
	}
}

func TestListForUser_ReturnsEmptySliceForUnknownUser(t *testing.T) {
	store := NewStore(t.TempDir())

	notifications, err := store.ListForUser("never-touched")
	if err != nil {
		t.Fatalf("ListForUser returned error: %v", err)
	}
	if len(notifications) != 0 {
		t.Errorf("got %d notifications, want 0", len(notifications))
	}
}

func TestMarkRead_SetsReadTrue(t *testing.T) {
	store := NewStore(t.TempDir())

	created, err := store.Create("dev-1", "task_assigned", "msg", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := store.MarkRead("dev-1", created.ID); err != nil {
		t.Fatalf("MarkRead returned error: %v", err)
	}

	notifications, err := store.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser returned error: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifications))
	}
	if !notifications[0].Read {
		t.Error("expected Read = true after MarkRead")
	}
}

func TestMarkRead_RejectsUnknownID(t *testing.T) {
	store := NewStore(t.TempDir())

	err := store.MarkRead("dev-1", "0123456789abcdef")
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMarkRead_CannotMarkAnotherUsersNotificationRead(t *testing.T) {
	store := NewStore(t.TempDir())

	created, err := store.Create("dev-1", "task_assigned", "msg", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = store.MarkRead("dev-2", created.ID)
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	// Confirm dev-1's notification is still unread.
	notifications, err := store.ListForUser("dev-1")
	if err != nil {
		t.Fatalf("ListForUser returned error: %v", err)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications, want 1", len(notifications))
	}
	if notifications[0].Read {
		t.Error("expected dev-1's notification to remain unread")
	}
}
