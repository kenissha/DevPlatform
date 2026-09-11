package taskboard

import (
	"errors"
	"reflect"
	"testing"
)

func TestCreateAcceptsLabels(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1", LabelBug, LabelTechDebt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if want := []Label{LabelBug, LabelTechDebt}; !reflect.DeepEqual(task.Labels, want) {
		t.Fatalf("labels = %v, want %v", task.Labels, want)
	}

	reloaded, err := store.Get("deneme", task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(reloaded.Labels, task.Labels) {
		t.Fatalf("reloaded labels = %v, want %v", reloaded.Labels, task.Labels)
	}
}

func TestCreateRejectsUnknownLabel(t *testing.T) {
	store := NewStore(t.TempDir())

	if _, err := store.Create("deneme", "Görev", "", "", "dev-1", Label("acil")); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("err = %v, want ErrInvalidLabel", err)
	}
}

// Order comes from KnownLabels rather than the caller, so the same set
// always renders the same way round on the card.
func TestLabelsAreNormalisedNotStoredAsGiven(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1",
		LabelResearch, LabelBug, LabelBug, LabelFeature)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := []Label{LabelBug, LabelFeature, LabelResearch}
	if !reflect.DeepEqual(task.Labels, want) {
		t.Fatalf("labels = %v, want %v", task.Labels, want)
	}
}

func TestUpdateReplacesLabelSet(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1", LabelBug)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	replaced := []Label{LabelFeature}
	updated, err := store.Update("deneme", task.ID, Changes{Labels: &replaced})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !reflect.DeepEqual(updated.Labels, []Label{LabelFeature}) {
		t.Fatalf("labels = %v, want [ozellik]", updated.Labels)
	}
}

// An empty (non-nil) slice clears the set; a nil pointer leaves it alone.
// The two are easy to conflate and mean opposite things.
func TestUpdateEmptyLabelsClearsAndNilLeavesAlone(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1", LabelBug)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	title := "Yeni başlık"
	untouched, err := store.Update("deneme", task.ID, Changes{Title: &title})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !reflect.DeepEqual(untouched.Labels, []Label{LabelBug}) {
		t.Fatalf("nil Labels changed the set: %v", untouched.Labels)
	}

	none := []Label{}
	cleared, err := store.Update("deneme", task.ID, Changes{Labels: &none})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(cleared.Labels) != 0 {
		t.Fatalf("labels = %v, want empty", cleared.Labels)
	}
}

func TestUpdateRejectsUnknownLabel(t *testing.T) {
	store := NewStore(t.TempDir())

	task, err := store.Create("deneme", "Görev", "", "", "dev-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	bad := []Label{Label("kritik")}
	if _, err := store.Update("deneme", task.ID, Changes{Labels: &bad}); !errors.Is(err, ErrInvalidLabel) {
		t.Fatalf("err = %v, want ErrInvalidLabel", err)
	}
}
