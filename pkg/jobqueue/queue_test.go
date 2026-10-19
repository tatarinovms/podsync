package jobqueue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/model"
)

func setupTestQueue(t *testing.T) (*JobQueue, db.Storage) {
	dir := t.TempDir()
	storage, err := db.NewBadger(&db.Config{Dir: dir})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = storage.Close()
	})

	queue := NewJobQueue(storage, 5)
	return queue, storage
}

func TestJobQueue_EnqueueAndDeduplication(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	// 1. Initial enqueue
	job1, created, err := q.EnqueueUpdate(ctx, "feed1")
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "update/feed1", job1.ID)
	assert.Equal(t, model.JobStatusQueued, job1.Status)
	assert.Equal(t, 0, job1.Attempts)

	// 2. Duplicate enqueue while queued -> must deduplicate!
	job2, created2, err := q.EnqueueUpdate(ctx, "feed1")
	require.NoError(t, err)
	assert.False(t, created2)
	assert.Equal(t, job1.ID, job2.ID)
	assert.Equal(t, model.JobStatusQueued, job2.Status)

	// 3. Claim job -> status becomes running
	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, "update/feed1", claimed.ID)
	assert.Equal(t, model.JobStatusRunning, claimed.Status)
	assert.NotNil(t, claimed.StartedAt)

	// 4. Duplicate enqueue while running -> must still deduplicate!
	job3, created3, err := q.EnqueueUpdate(ctx, "feed1")
	require.NoError(t, err)
	assert.False(t, created3)
	assert.Equal(t, model.JobStatusRunning, job3.Status)

	// 5. Complete job -> status becomes done
	err = q.MarkDone(ctx, claimed.ID)
	require.NoError(t, err)

	doneJob, err := q.GetJob(ctx, "update/feed1")
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusDone, doneJob.Status)
	assert.NotNil(t, doneJob.FinishedAt)

	// 6. Enqueue after done -> new job is created!
	job4, created4, err := q.EnqueueUpdate(ctx, "feed1")
	require.NoError(t, err)
	assert.True(t, created4)
	assert.Equal(t, model.JobStatusQueued, job4.Status)
}

func TestJobQueue_RetryBackoff(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	job, created, err := q.EnqueueUpdate(ctx, "feed_backoff")
	require.NoError(t, err)
	assert.True(t, created)

	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	// Simulate temporary network error
	tempErr := errors.New("temporary network timeout dial tcp 1.2.3.4:443")
	now := time.Now().UTC()

	retried, err := q.MarkFailed(ctx, job.ID, tempErr)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusRetrying, retried.Status)
	assert.Equal(t, 1, retried.Attempts)
	assert.True(t, retried.AvailableAt.After(now.Add(50*time.Second)))
	assert.True(t, retried.AvailableAt.Before(now.Add(70*time.Second)))

	// Should not be claimable immediately because AvailableAt is in the future
	claimed2, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	assert.Nil(t, claimed2)

	// Fast-forward AvailableAt to past
	retried.AvailableAt = time.Now().UTC().Add(-1 * time.Second)
	require.NoError(t, q.db.SaveJob(ctx, retried))

	// Now it should be claimable
	claimed3, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed3)
	assert.Equal(t, model.JobStatusRunning, claimed3.Status)
}

func TestJobQueue_FatalErrorsImmediatelyFail(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	// 1. Missing token error
	job, _, err := q.EnqueueUpdate(ctx, "feed_auth_err")
	require.NoError(t, err)
	_, err = q.ClaimNextJob(ctx)
	require.NoError(t, err)

	failedJob, err := q.MarkFailed(ctx, job.ID, errors.New("YouTube API token is missing or invalid"))
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusFailed, failedJob.Status)
	assert.Contains(t, failedJob.LastError, "API token missing or invalid")
	assert.NotNil(t, failedJob.FinishedAt)

	// 2. 404 Channel not found
	job404, _, err := q.EnqueueUpdate(ctx, "feed_404")
	require.NoError(t, err)
	_, err = q.ClaimNextJob(ctx)
	require.NoError(t, err)

	failedJob404, err := q.MarkFailed(ctx, job404.ID, errors.New("ERROR: [youtube:tab] The channel does not exist (HTTP 404)"))
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusFailed, failedJob404.Status)
	assert.Contains(t, failedJob404.LastError, "Resource not found (HTTP 404)")
}

func TestJobQueue_RateLimitBackoff(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	job, _, err := q.EnqueueUpdate(ctx, "feed_429")
	require.NoError(t, err)
	_, err = q.ClaimNextJob(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	retried, err := q.MarkFailed(ctx, job.ID, errors.New("HTTP Error 429: Too Many Requests"))
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusRetrying, retried.Status)
	// Rate limit attempt 1 uses 30 minute delay
	assert.True(t, retried.AvailableAt.After(now.Add(25*time.Minute)))
}

func TestJobQueue_CrashRecovery(t *testing.T) {
	q, storage := setupTestQueue(t)
	ctx := context.Background()

	// Simulate a job left in running state when process died
	crashedJob := &model.UpdateJob{
		ID:          "update/crashed_feed",
		FeedID:      "crashed_feed",
		Kind:        model.JobKindUpdate,
		Status:      model.JobStatusRunning,
		Attempts:    0,
		MaxAttempts: 5,
		AvailableAt: time.Now().UTC().Add(-10 * time.Minute),
		CreatedAt:   time.Now().UTC().Add(-10 * time.Minute),
	}
	require.NoError(t, storage.SaveJob(ctx, crashedJob))

	recovered, err := q.RecoverStaleJobs(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)

	// Status should have been reset to Queued
	job, err := q.GetJob(ctx, crashedJob.ID)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusQueued, job.Status)
	assert.Nil(t, job.StartedAt)

	// And it must be claimable immediately
	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, "update/crashed_feed", claimed.ID)
}

func TestCalculateBackoff(t *testing.T) {
	assert.Equal(t, 1*time.Minute, CalculateBackoff(1, false))
	assert.Equal(t, 5*time.Minute, CalculateBackoff(2, false))
	assert.Equal(t, 15*time.Minute, CalculateBackoff(3, false))
	assert.Equal(t, 1*time.Hour, CalculateBackoff(4, false))
	assert.Equal(t, 6*time.Hour, CalculateBackoff(5, false))

	// 429 rate limit
	assert.Equal(t, 30*time.Minute, CalculateBackoff(1, true))
	assert.Equal(t, 1*time.Hour, CalculateBackoff(2, true))
	assert.Equal(t, 2*time.Hour, CalculateBackoff(3, true))
	assert.Equal(t, 6*time.Hour, CalculateBackoff(4, true))
}
