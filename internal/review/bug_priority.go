package review

import "slices"

// BugPriority records the user impact of a confirmed hypothesis.
type BugPriority string

// Bug priorities from lowest to highest impact.
const (
	BugPriorityLow      BugPriority = "low"
	BugPriorityHigh     BugPriority = "high"
	BugPriorityCritical BugPriority = "critical"
)

func (p BugPriority) rank() int {
	return slices.Index([]BugPriority{BugPriorityLow, BugPriorityHigh, BugPriorityCritical}, p)
}

// IsValid rejects an empty priority in repo settings.
func (p BugPriority) IsValid() bool {
	return p.rank() >= 0
}

func (p BugPriority) orHigh() BugPriority {
	if p.IsValid() {
		return p
	}
	return BugPriorityHigh
}
