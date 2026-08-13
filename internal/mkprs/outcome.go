package mkprs

import "fmt"

// outcomeKind is the closed set of ways a repo can end up. A skip is a normal
// result (nothing to do here), not an error, hence three states rather than an
// error and its absence.
//
// failed is the zero value on purpose: an outcome nobody filled in counts as a
// repo to come back to, never as a silent success.
type outcomeKind int

const (
	failed outcomeKind = iota
	skipped
	succeeded
)

func (k outcomeKind) String() string {
	switch k {
	case failed:
		return "failed"
	case skipped:
		return "skipped"
	case succeeded:
		return "succeeded"
	}
	return fmt.Sprintf("outcomeKind(%d)", int(k))
}

// A *outcome is the same answer where it may not have arrived yet -- preflight
// and commitAndPush return nil to mean "carry on". process, work and openPR
// return the value, which is always a real result.
type outcome struct {
	kind   outcomeKind
	prURL  string // succeeded only
	reason string // skipped and failed only
}

func success(prURL string) *outcome  { return &outcome{kind: succeeded, prURL: prURL} }
func skip(reason string) *outcome    { return &outcome{kind: skipped, reason: reason} }
func failure(reason string) *outcome { return &outcome{kind: failed, reason: reason} }
