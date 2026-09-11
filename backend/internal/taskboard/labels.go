package taskboard

import "errors"

// Labels say what kind of work a task is, which is the axis priority and
// status do not cover: "kritik" tells you when to look, "hata" tells you
// what you are looking at.
//
// The set is fixed in code rather than managed per repository. A
// label-management screen is real machinery — who may create one, what
// colour it gets, what happens to tasks when one is deleted — and for a
// team this size it would be a screen visited once and then never again.
// Growing the set means adding a line here; that is a smaller cost than
// the alternative, and it keeps every board using the same vocabulary.
type Label string

const (
	LabelBug           Label = "hata"
	LabelFeature       Label = "ozellik"
	LabelImprovement   Label = "iyilestirme"
	LabelTechDebt      Label = "teknik-borc"
	LabelDocumentation Label = "dokuman"
	LabelResearch      Label = "arastirma"
)

var ErrInvalidLabel = errors.New("taskboard: invalid label")

// KnownLabels is the whole vocabulary, in the order a picker shows it —
// most-used first rather than alphabetical.
var KnownLabels = []Label{
	LabelBug,
	LabelFeature,
	LabelImprovement,
	LabelTechDebt,
	LabelDocumentation,
	LabelResearch,
}

// Values are ASCII on purpose: a label ends up in URLs (the board's
// filter is a query parameter) and in filenames nowhere, but the URL
// alone is reason enough not to put "teknik borç" through percent
// encoding on every filter click. The Turkish spelling lives in the
// frontend's label map, which is where every other display string lives.
func (l Label) valid() bool {
	for _, known := range KnownLabels {
		if l == known {
			return true
		}
	}
	return false
}

// normaliseLabels validates a set and removes duplicates, preserving
// KnownLabels' order so two tasks with the same labels always render them
// the same way round.
func normaliseLabels(labels []Label) ([]Label, error) {
	seen := map[Label]bool{}
	for _, l := range labels {
		if !l.valid() {
			return nil, ErrInvalidLabel
		}
		seen[l] = true
	}

	// nil, not an empty slice, when nothing is set. Labels is written with
	// omitempty, so an empty slice would be dropped on save and read back
	// as nil — a task in memory would then not equal the same task
	// reloaded, which is exactly the kind of difference that only shows up
	// much later and somewhere else.
	var out []Label
	for _, known := range KnownLabels {
		if seen[known] {
			out = append(out, known)
		}
	}
	return out, nil
}

// HasLabel reports whether the task carries one.
func (t Task) HasLabel(l Label) bool {
	for _, own := range t.Labels {
		if own == l {
			return true
		}
	}
	return false
}
