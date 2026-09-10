package taskboard

import (
	"errors"
	"strings"
)

// Subtasks are a checklist on a task, not tasks of their own.
//
// The alternative — real parent/child tasks — was rejected deliberately:
// each child would need its own key, appear on the board, carry its own
// assignee and due date, and then the board shows twenty rows where there
// is one piece of work. What people actually want when they break
// something down is "these five things have to be true before this is
// done", which is a list of ticks.
//
// They live on the Task itself rather than in their own file: they are
// always read and written with the task, and there are never many.

var (
	ErrEmptySubtask    = errors.New("taskboard: subtask title must not be empty")
	ErrTooManySubtasks = errors.New("taskboard: too many subtasks")
)

// MaxSubtasks caps a checklist. Past this many, the thing being described
// is a project, not a task, and it should be split into real tasks that
// the board can show and people can be assigned to.
const MaxSubtasks = 50

type Subtask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// SubtaskProgress reports how many of a task's subtasks are ticked.
func (t Task) SubtaskProgress() (done, total int) {
	for _, st := range t.Subtasks {
		if st.Done {
			done++
		}
	}
	return done, len(t.Subtasks)
}

// AddSubtask appends one item to a task's checklist.
func (s *Store) AddSubtask(repo, id, title string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, ErrEmptySubtask
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.Get(repo, id)
	if err != nil {
		return Task{}, err
	}
	if len(task.Subtasks) >= MaxSubtasks {
		return Task{}, ErrTooManySubtasks
	}

	subtaskID, err := newID()
	if err != nil {
		return Task{}, err
	}
	task.Subtasks = append(task.Subtasks, Subtask{ID: subtaskID, Title: title})

	if err := s.write(repo, task); err != nil {
		return Task{}, err
	}
	return task, nil
}

// SetSubtaskDone ticks or unticks one item.
func (s *Store) SetSubtaskDone(repo, id, subtaskID string, done bool) (Task, error) {
	return s.mutateSubtask(repo, id, subtaskID, func(st *Subtask) error {
		st.Done = done
		return nil
	})
}

// RenameSubtask changes one item's text.
func (s *Store) RenameSubtask(repo, id, subtaskID, title string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, ErrEmptySubtask
	}
	return s.mutateSubtask(repo, id, subtaskID, func(st *Subtask) error {
		st.Title = title
		return nil
	})
}

// RemoveSubtask drops one item from the checklist.
func (s *Store) RemoveSubtask(repo, id, subtaskID string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.Get(repo, id)
	if err != nil {
		return Task{}, err
	}
	for i, st := range task.Subtasks {
		if st.ID != subtaskID {
			continue
		}
		task.Subtasks = append(task.Subtasks[:i:i], task.Subtasks[i+1:]...)
		if err := s.write(repo, task); err != nil {
			return Task{}, err
		}
		return task, nil
	}
	return Task{}, ErrNotFound
}

// mutateSubtask finds one item, applies change, and saves the task.
func (s *Store) mutateSubtask(repo, id, subtaskID string, change func(*Subtask) error) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.Get(repo, id)
	if err != nil {
		return Task{}, err
	}
	for i := range task.Subtasks {
		if task.Subtasks[i].ID != subtaskID {
			continue
		}
		if err := change(&task.Subtasks[i]); err != nil {
			return Task{}, err
		}
		if err := s.write(repo, task); err != nil {
			return Task{}, err
		}
		return task, nil
	}
	return Task{}, ErrNotFound
}
