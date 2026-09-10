package taskboard

// Priority replaces the original boolean "urgent" flag.
//
// A yes/no flag forces everything into two buckets, and in practice that
// makes the flag meaningless: once more than a couple of things are
// marked urgent, "urgent" stops distinguishing anything. Four levels give
// a board that can actually be ordered.
//
// PriorityNormal is the zero-value-equivalent default, deliberately not
// the empty string: a task's priority is always an explicit, displayable
// value, so no caller has to decide what "" means.
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityNormal   Priority = "normal"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

func (p Priority) valid() bool {
	return p == PriorityLow || p == PriorityNormal || p == PriorityHigh || p == PriorityCritical
}

// upgradePriority fills in the priority of a task written before the
// field existed, so nothing on disk needs rewriting.
//
// Tasks saved with the old boolean carry `"urgent": true`, which was the
// only way to say "this one matters" — those become PriorityHigh rather
// than PriorityCritical: the old flag was routinely used for "look at
// this soon", not for "the site is down", and promoting all of them to
// the top level would leave the new scale just as flat as the flag was.
func upgradePriority(t Task) Task {
	if t.Priority != "" {
		return t
	}
	if t.Urgent {
		t.Priority = PriorityHigh
	} else {
		t.Priority = PriorityNormal
	}
	return t
}
