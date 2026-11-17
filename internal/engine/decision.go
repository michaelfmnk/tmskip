package engine

// Reason is why a path is (or isn't) to be excluded.
type Reason int

const (
	ReasonMatched Reason = iota + 1
	ReasonFixed
	ReasonNotMatched
	ReasonSkipped
	ReasonOutsideRoots
	ReasonBlocked
	ReasonUnreadable
	ReasonMissing
	ReasonUnmounted
	ReasonCovered
)

func (r Reason) String() string {
	switch r {
	case ReasonMatched:
		return "matched"
	case ReasonFixed:
		return "fixed"
	case ReasonNotMatched:
		return "not_matched"
	case ReasonSkipped:
		return "skipped"
	case ReasonOutsideRoots:
		return "outside_roots"
	case ReasonBlocked:
		return "blocked"
	case ReasonUnreadable:
		return "unreadable"
	case ReasonMissing:
		return "missing"
	case ReasonUnmounted:
		return "unmounted"
	case ReasonCovered:
		return "covered"
	}
	return "unknown"
}

// Decision is what tmskip decides for one path.
type Decision struct {
	Path      string
	Exclude   bool
	Reason    Reason
	Match     Match  // when matched/fixed, or blocked after a match
	Guard     string // guardrail reason when blocked
	CoveredBy string // nearest ancestor (below the root) that is itself excluded; only for why
	Err       error  // when unreadable
}

// Problem is a folder that couldn't be read. It is reported, never skipped
// silently, and leaves existing exclusions untouched.
type Problem struct {
	Path   string
	Err    error
	Denied bool // EPERM: a Full Disk Access (TCC) denial
}
