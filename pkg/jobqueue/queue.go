package jobqueue

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/model"
)

const (
	DefaultMaxAttempts = 5
	DefaultPollPeriod  = 5 * time.Second
)

type JobQueue struct {
	db          db.Storage
	maxAttempts int
	pollPeriod  time.Duration
	notifyCh    chan struct{}
	mu          sync.Mutex
}

func NewJobQueue(storage db.Storage, maxAttempts int) *JobQueue {
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	return &JobQueue{
		db:          storage,
		maxAttempts: maxAttempts,
		pollPeriod:  DefaultPollPeriod,
		notifyCh:    make(chan struct{}, 1),
	}
}

func (q *JobQueue) notify() {
	select {
	case q.notifyCh <- struct{}{}:
	default:
	}
}

func (q *JobQueue) SetPollPeriod(d time.Duration) {
	if d > 0 {
		q.pollPeriod = d
	}
}

// EnqueueUpdate creates or returns an active update job for the given feed ID.
// Returns (job, created, err). If a job is already queued, running or retrying, created=false.
func (q *JobQueue) EnqueueUpdate(ctx context.Context, feedID string) (*model.UpdateJob, bool, error) {
	if strings.TrimSpace(feedID) == "" {
		return nil, false, errors.New("empty feed_id")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	jobID := "update/" + feedID
	existing, err := q.db.GetJob(ctx, jobID)
	if err == nil {
		if existing != nil && existing.IsActive() {
			return existing, false, nil
		}
	} else if err != model.ErrNotFound {
		return nil, false, errors.Wrapf(err, "failed to check existing job %q", jobID)
	}

	now := time.Now().UTC()
	job := &model.UpdateJob{
		ID:          jobID,
		FeedID:      feedID,
		Kind:        model.JobKindUpdate,
		Status:      model.JobStatusQueued,
		Attempts:    0,
		MaxAttempts: q.maxAttempts,
		AvailableAt: now,
		CreatedAt:   now,
	}

	if err := q.db.SaveJob(ctx, job); err != nil {
		return nil, false, errors.Wrapf(err, "failed to persist update job %q", jobID)
	}

	q.notify()
	return job, true, nil
}

// EnqueueRetry creates or returns an active retry job for the given feed and episode ID.
func (q *JobQueue) EnqueueRetry(ctx context.Context, feedID, episodeID string) (*model.UpdateJob, bool, error) {
	if strings.TrimSpace(feedID) == "" || strings.TrimSpace(episodeID) == "" {
		return nil, false, errors.New("empty feed_id or episode_id")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	jobID := "retry/" + feedID + "/" + episodeID
	existing, err := q.db.GetJob(ctx, jobID)
	if err == nil {
		if existing != nil && existing.IsActive() {
			return existing, false, nil
		}
	} else if err != model.ErrNotFound {
		return nil, false, errors.Wrapf(err, "failed to check existing job %q", jobID)
	}

	now := time.Now().UTC()
	job := &model.UpdateJob{
		ID:          jobID,
		FeedID:      feedID,
		EpisodeID:   episodeID,
		Kind:        model.JobKindRetry,
		Status:      model.JobStatusQueued,
		Attempts:    0,
		MaxAttempts: q.maxAttempts,
		AvailableAt: now,
		CreatedAt:   now,
	}

	if err := q.db.SaveJob(ctx, job); err != nil {
		return nil, false, errors.Wrapf(err, "failed to persist retry job %q", jobID)
	}

	q.notify()
	return job, true, nil
}

// ClaimNextJob finds the next available job whose AvailableAt is in the past,
// marks it as Running, and persists the update atomically under mutex.
func (q *JobQueue) ClaimNextJob(ctx context.Context) (*model.UpdateJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now().UTC()
	var candidate *model.UpdateJob

	err := q.db.WalkJobs(ctx, func(j *model.UpdateJob) error {
		if (j.Status == model.JobStatusQueued || j.Status == model.JobStatusRetrying) && !j.AvailableAt.After(now) {
			if candidate == nil || j.AvailableAt.Before(candidate.AvailableAt) {
				candidate = j
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to query available jobs")
	}

	if candidate == nil {
		return nil, nil
	}

	candidate.Status = model.JobStatusRunning
	candidate.StartedAt = &now
	if err := q.db.SaveJob(ctx, candidate); err != nil {
		return nil, errors.Wrapf(err, "failed to claim job %q", candidate.ID)
	}

	return candidate, nil
}

// WaitForJob blocks until an executable job becomes available or the context is cancelled.
func (q *JobQueue) WaitForJob(ctx context.Context) (*model.UpdateJob, error) {
	for {
		job, err := q.ClaimNextJob(ctx)
		if err != nil {
			return nil, err
		}
		if job != nil {
			return job, nil
		}

		nextWakeup := q.calculateNextWakeup(ctx)
		timer := time.NewTimer(nextWakeup)

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-q.notifyCh:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (q *JobQueue) calculateNextWakeup(ctx context.Context) time.Duration {
	now := time.Now().UTC()
	shortest := q.pollPeriod

	_ = q.db.WalkJobs(ctx, func(j *model.UpdateJob) error {
		if j.Status == model.JobStatusQueued || j.Status == model.JobStatusRetrying {
			if j.AvailableAt.After(now) {
				wait := j.AvailableAt.Sub(now)
				if wait < shortest {
					shortest = wait
				}
			} else {
				shortest = 50 * time.Millisecond
			}
		}
		return nil
	})

	if shortest < 50*time.Millisecond {
		shortest = 50 * time.Millisecond
	}
	return shortest
}

// MarkDone marks a job as successfully completed.
func (q *JobQueue) MarkDone(ctx context.Context, jobID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, err := q.db.GetJob(ctx, jobID)
	if err != nil {
		return errors.Wrapf(err, "failed to get job %q to mark done", jobID)
	}

	now := time.Now().UTC()
	job.Status = model.JobStatusDone
	job.FinishedAt = &now
	job.LastError = ""

	return q.db.SaveJob(ctx, job)
}

// MarkFailed transitions a job either to retrying with exponential backoff or to failed.
func (q *JobQueue) MarkFailed(ctx context.Context, jobID string, jobErr error) (*model.UpdateJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, err := q.db.GetJob(ctx, jobID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get job %q to mark failed", jobID)
	}

	now := time.Now().UTC()
	isFatal, fatalMsg := IsFatalError(jobErr)
	isRateLimit := IsRateLimitError(jobErr)

	errMsg := ""
	if jobErr != nil {
		errMsg = jobErr.Error()
	}
	if fatalMsg != "" {
		errMsg = fatalMsg
	}

	job.Attempts++
	job.LastError = errMsg

	if isFatal || job.Attempts >= job.MaxAttempts {
		job.Status = model.JobStatusFailed
		job.FinishedAt = &now
		log.WithFields(log.Fields{
			"job_id":   jobID,
			"attempts": job.Attempts,
			"fatal":    isFatal,
		}).Warnf("job failed permanently: %s", errMsg)
	} else {
		delay := CalculateBackoff(job.Attempts, isRateLimit)
		job.Status = model.JobStatusRetrying
		job.AvailableAt = now.Add(delay)
		job.StartedAt = nil
		log.WithFields(log.Fields{
			"job_id":       jobID,
			"attempts":     job.Attempts,
			"next_retry":   job.AvailableAt.Format(time.RFC3339),
			"delay":        delay.String(),
			"rate_limited": isRateLimit,
		}).Infof("job scheduled for retry: %s", errMsg)
	}

	if err := q.db.SaveJob(ctx, job); err != nil {
		return nil, errors.Wrapf(err, "failed to save failed/retrying job %q", jobID)
	}

	return job, nil
}

// RecoverStaleJobs recovers jobs left in Running state from a previous server crash or restart.
// Running jobs are moved back to Queued or Retrying with their StartedAt reset.
func (q *JobQueue) RecoverStaleJobs(ctx context.Context) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now().UTC()
	recovered := 0

	err := q.db.WalkJobs(ctx, func(j *model.UpdateJob) error {
		if j.Status == model.JobStatusRunning {
			if j.Attempts > 0 {
				j.Status = model.JobStatusRetrying
				j.AvailableAt = now.Add(15 * time.Second)
			} else {
				j.Status = model.JobStatusQueued
				j.AvailableAt = now
			}
			j.StartedAt = nil
			if err := q.db.SaveJob(ctx, j); err != nil {
				return err
			}
			recovered++
			log.WithField("job_id", j.ID).Warn("recovered stale running job from previous crash/shutdown")
		}
		return nil
	})
	if err != nil {
		return 0, errors.Wrap(err, "failed to recover stale jobs")
	}

	if recovered > 0 {
		q.notify()
	}
	return recovered, nil
}

// GetJob returns a job by ID.
func (q *JobQueue) GetJob(ctx context.Context, jobID string) (*model.UpdateJob, error) {
	return q.db.GetJob(ctx, jobID)
}

// GetActiveJobForFeed returns any active (queued, running, retrying) job for the specified feed.
func (q *JobQueue) GetActiveJobForFeed(ctx context.Context, feedID string) (*model.UpdateJob, error) {
	jobID := "update/" + feedID
	job, err := q.db.GetJob(ctx, jobID)
	if err != nil {
		if err == model.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	if job.IsActive() {
		return job, nil
	}
	return nil, nil
}

// ListJobs returns all jobs stored in the database.
func (q *JobQueue) ListJobs(ctx context.Context) ([]*model.UpdateJob, error) {
	var list []*model.UpdateJob
	err := q.db.WalkJobs(ctx, func(j *model.UpdateJob) error {
		list = append(list, j)
		return nil
	})
	return list, err
}

// GetActiveJobsMap returns a map of feedID -> active job in a single pass.
func (q *JobQueue) GetActiveJobsMap(ctx context.Context) (map[string]*model.UpdateJob, error) {
	result := make(map[string]*model.UpdateJob)
	err := q.db.WalkJobs(ctx, func(j *model.UpdateJob) error {
		if j.IsActive() && j.FeedID != "" {
			result[j.FeedID] = j
		}
		return nil
	})
	return result, err
}

// PruneHistory deletes finished (done or failed) jobs older than maxAge.
func (q *JobQueue) PruneHistory(ctx context.Context, maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		return 0, nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now().UTC()
	var toDelete []string

	err := q.db.WalkJobs(ctx, func(j *model.UpdateJob) error {
		if j.Status == model.JobStatusDone || j.Status == model.JobStatusFailed {
			finished := j.CreatedAt
			if j.FinishedAt != nil {
				finished = *j.FinishedAt
			}
			if now.Sub(finished) > maxAge {
				toDelete = append(toDelete, j.ID)
			}
		}
		return nil
	})
	if err != nil {
		return 0, errors.Wrap(err, "failed to query finished jobs for pruning")
	}

	for _, id := range toDelete {
		if err := q.db.DeleteJob(ctx, id); err != nil {
			log.WithField("job_id", id).WithError(err).Warn("failed to prune old job")
		}
	}

	if len(toDelete) > 0 {
		log.Infof("pruned %d old completed job(s) from history", len(toDelete))
	}
	return len(toDelete), nil
}

// RecoverStaleRunning checks for jobs that have been in Running state longer than maxRunningDuration
// (e.g. worker hung or crashed silently) and resets them to Retrying or Queued.
func (q *JobQueue) RecoverStaleRunning(ctx context.Context, maxRunningDuration time.Duration) (int, error) {
	if maxRunningDuration <= 0 {
		maxRunningDuration = 2 * time.Hour
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now().UTC()
	recovered := 0

	err := q.db.WalkJobs(ctx, func(j *model.UpdateJob) error {
		if j.Status == model.JobStatusRunning {
			startTime := j.CreatedAt
			if j.StartedAt != nil {
				startTime = *j.StartedAt
			}
			if now.Sub(startTime) > maxRunningDuration {
				if j.Attempts > 0 {
					j.Status = model.JobStatusRetrying
					j.AvailableAt = now.Add(15 * time.Second)
				} else {
					j.Status = model.JobStatusQueued
					j.AvailableAt = now
				}
				j.StartedAt = nil
				j.LastError = "job execution timed out by watchdog"
				if err := q.db.SaveJob(ctx, j); err != nil {
					return err
				}
				recovered++
				log.WithFields(log.Fields{
					"job_id":   j.ID,
					"duration": now.Sub(startTime).String(),
				}).Warn("watchdog recovered timed out running job")
			}
		}
		return nil
	})
	if err != nil {
		return 0, errors.Wrap(err, "failed to check stale running jobs")
	}

	if recovered > 0 {
		q.notify()
	}
	return recovered, nil
}

// CalculateBackoff determines retry delay according to attempts:
// 1m -> 5m -> 15m -> 1h -> 6h (standard)
// 30m -> 1h -> 2h -> 6h (for HTTP 429 rate limits)
func CalculateBackoff(attempts int, isRateLimit bool) time.Duration {
	if isRateLimit {
		switch attempts {
		case 1:
			return 30 * time.Minute
		case 2:
			return 1 * time.Hour
		case 3:
			return 2 * time.Hour
		default:
			return 6 * time.Hour
		}
	}

	switch attempts {
	case 1:
		return 1 * time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 15 * time.Minute
	case 4:
		return 1 * time.Hour
	default:
		return 6 * time.Hour
	}
}

// IsFatalError checks whether an error is non-retryable (missing token, invalid auth, broken URL).
func IsFatalError(err error) (bool, string) {
	if err == nil {
		return false, ""
	}
	msg := strings.ToLower(err.Error())

	// Missing or invalid tokens
	if strings.Contains(msg, "token") && (strings.Contains(msg, "missing") || strings.Contains(msg, "invalid") || strings.Contains(msg, "unauthorized")) {
		return true, "API token missing or invalid. Check token configuration in Web Admin."
	}
	if strings.Contains(msg, "api key") && (strings.Contains(msg, "missing") || strings.Contains(msg, "invalid")) {
		return true, "API key missing or invalid. Check YouTube/Vimeo credentials."
	}

	// 401 Unauthorized
	if strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized") {
		return true, "Unauthorized access (HTTP 401). Verify credentials."
	}

	// 404 Not Found / Deleted resource
	if strings.Contains(msg, "404") || strings.Contains(msg, "not found") ||
		strings.Contains(msg, "does not exist") || strings.Contains(msg, "video unavailable") ||
		strings.Contains(msg, "channel unavailable") || strings.Contains(msg, "playlist does not exist") {
		return true, "Resource not found (HTTP 404). Channel, playlist or URL does not exist."
	}

	// 403 Forbidden (excluding bot challenges that might resolve with cookies)
	if (strings.Contains(msg, "403") || strings.Contains(msg, "forbidden")) &&
		!strings.Contains(msg, "bot") && !strings.Contains(msg, "sign in to confirm") {
		return true, "Access forbidden (HTTP 403). Content may be private, blocked or georestricted."
	}

	return false, ""
}

// IsRateLimitError detects HTTP 429 or rate-limiting responses.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") || strings.Contains(msg, "too many requests") || strings.Contains(msg, "rate limit")
}
