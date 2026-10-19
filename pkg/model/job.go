package model

import "time"

type JobKind string

const (
	JobKindUpdate  = JobKind("update")
	JobKindRetry   = JobKind("retry")
	JobKindRebuild = JobKind("rebuild")
)

type JobStatus string

const (
	JobStatusQueued   = JobStatus("queued")
	JobStatusRunning  = JobStatus("running")
	JobStatusRetrying = JobStatus("retrying")
	JobStatusDone     = JobStatus("done")
	JobStatusFailed   = JobStatus("failed")
)

type UpdateJob struct {
	ID          string     `json:"id"` // e.g. "update/feed_id" or "retry/feed_id/episode_id"
	FeedID      string     `json:"feed_id"`
	EpisodeID   string     `json:"episode_id,omitempty"`
	Kind        JobKind    `json:"kind"`
	Status      JobStatus  `json:"status"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"max_attempts"`
	AvailableAt time.Time  `json:"available_at"`
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// IsActive returns true if the job is in a state that should prevent duplicate queueing.
func (j *UpdateJob) IsActive() bool {
	return j.Status == JobStatusQueued || j.Status == JobStatusRunning || j.Status == JobStatusRetrying
}
