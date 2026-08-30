package domain

// JobStatus represents the state of a processing job.
type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

// IsValid checks if the status is one of the supported states.
func (s JobStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusProcessing, StatusCompleted, StatusFailed:
		return true
	default:
		return false
	}
}

// IsTerminal returns true if the status cannot transition to any other state.
func (s JobStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed
}

// CanTransitionTo returns true if transitioning from s to next is permitted by domain rules.
// Rules:
// - pending -> processing, failed
// - processing -> completed, failed
// - completed, failed are terminal
func (s JobStatus) CanTransitionTo(next JobStatus) bool {
	if s.IsTerminal() {
		return false
	}
	switch s {
	case StatusPending:
		return next == StatusProcessing || next == StatusFailed
	case StatusProcessing:
		return next == StatusCompleted || next == StatusFailed
	default:
		return false
	}
}
