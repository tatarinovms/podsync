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

	// 4. Duplicate enqueue while running -> must deduplicate and record pending rerun!
	job3, created3, err := q.EnqueueManualUpdate(ctx, "feed1")
	require.NoError(t, err)
	assert.False(t, created3)
	assert.Equal(t, model.JobStatusRunning, job3.Status)
	assert.True(t, job3.PendingRerun)

	// 5. Complete current run -> re-queues for the rerun requested in step 4
	err = q.MarkDone(ctx, claimed.ID, claimed.ClaimToken)
	require.NoError(t, err)

	requeuedJob, err := q.GetJob(ctx, "update/feed1")
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusQueued, requeuedJob.Status)

	// 6. Complete the rerun -> status becomes done
	claimed2, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed2)
	err = q.MarkDone(ctx, claimed2.ID, claimed2.ClaimToken)
	require.NoError(t, err)

	doneJob, err := q.GetJob(ctx, "update/feed1")
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusDone, doneJob.Status)
	assert.NotNil(t, doneJob.FinishedAt)

	// 7. Enqueue after done -> new job is created!
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

	retried, err := q.MarkFailed(ctx, job.ID, tempErr, claimed.ClaimToken)
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
	claimedAuth, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)

	failedJob, err := q.MarkFailed(ctx, job.ID, errors.New("YouTube API token is missing or invalid"), claimedAuth.ClaimToken)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusFailed, failedJob.Status)
	assert.Contains(t, failedJob.LastError, "API token missing or invalid")
	assert.NotNil(t, failedJob.FinishedAt)

	// 2. 404 Channel not found
	job404, _, err := q.EnqueueUpdate(ctx, "feed_404")
	require.NoError(t, err)
	claimed404, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)

	failedJob404, err := q.MarkFailed(ctx, job404.ID, errors.New("ERROR: [youtube:tab] The channel does not exist (HTTP 404)"), claimed404.ClaimToken)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusFailed, failedJob404.Status)
	assert.Contains(t, failedJob404.LastError, "Resource not found (HTTP 404)")
}

func TestJobQueue_RateLimitBackoff(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	job, _, err := q.EnqueueUpdate(ctx, "feed_429")
	require.NoError(t, err)
	claimed429, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	retried, err := q.MarkFailed(ctx, job.ID, errors.New("HTTP Error 429: Too Many Requests"), claimed429.ClaimToken)
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

func TestJobQueue_PruneHistory(t *testing.T) {
	q, storage := setupTestQueue(t)
	ctx := context.Background()

	now := time.Now().UTC()
	oldFinished := now.Add(-40 * 24 * time.Hour)
	recentFinished := now.Add(-5 * 24 * time.Hour)

	oldDone := &model.UpdateJob{
		ID:         "update/old_done",
		FeedID:     "old_done",
		Kind:       model.JobKindUpdate,
		Status:     model.JobStatusDone,
		FinishedAt: &oldFinished,
		CreatedAt:  oldFinished.Add(-10 * time.Minute),
	}
	require.NoError(t, storage.SaveJob(ctx, oldDone))

	oldFailed := &model.UpdateJob{
		ID:         "update/old_failed",
		FeedID:     "old_failed",
		Kind:       model.JobKindUpdate,
		Status:     model.JobStatusFailed,
		FinishedAt: &oldFinished,
		CreatedAt:  oldFinished.Add(-10 * time.Minute),
	}
	require.NoError(t, storage.SaveJob(ctx, oldFailed))

	recentDone := &model.UpdateJob{
		ID:         "update/recent_done",
		FeedID:     "recent_done",
		Kind:       model.JobKindUpdate,
		Status:     model.JobStatusDone,
		FinishedAt: &recentFinished,
		CreatedAt:  recentFinished.Add(-10 * time.Minute),
	}
	require.NoError(t, storage.SaveJob(ctx, recentDone))

	runningJob := &model.UpdateJob{
		ID:        "update/running_job",
		FeedID:    "running_job",
		Kind:      model.JobKindUpdate,
		Status:    model.JobStatusRunning,
		CreatedAt: oldFinished,
	}
	require.NoError(t, storage.SaveJob(ctx, runningJob))

	// Prune history older than 30 days
	pruned, err := q.PruneHistory(ctx, 30*24*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 2, pruned)

	// Old done & failed must be deleted
	_, err = storage.GetJob(ctx, "update/old_done")
	assert.Equal(t, model.ErrNotFound, err)
	_, err = storage.GetJob(ctx, "update/old_failed")
	assert.Equal(t, model.ErrNotFound, err)

	// Recent done and running job must remain
	j, err := storage.GetJob(ctx, "update/recent_done")
	require.NoError(t, err)
	assert.NotNil(t, j)

	j2, err := storage.GetJob(ctx, "update/running_job")
	require.NoError(t, err)
	assert.NotNil(t, j2)
}

func TestJobQueue_RecoverStaleRunning(t *testing.T) {
	q, storage := setupTestQueue(t)
	ctx := context.Background()

	now := time.Now().UTC()
	threeHoursAgo := now.Add(-3 * time.Hour)

	stuckJob := &model.UpdateJob{
		ID:        "update/stuck_feed",
		FeedID:    "stuck_feed",
		Kind:      model.JobKindUpdate,
		Status:    model.JobStatusRunning,
		Attempts:  1,
		StartedAt: &threeHoursAgo,
		CreatedAt: threeHoursAgo.Add(-5 * time.Minute),
	}
	require.NoError(t, storage.SaveJob(ctx, stuckJob))

	recovered, err := q.RecoverStaleRunning(ctx, 2*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)

	updated, err := storage.GetJob(ctx, "update/stuck_feed")
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusRetrying, updated.Status)
	assert.Nil(t, updated.StartedAt)
	assert.Contains(t, updated.LastError, "timed out by watchdog")
}

func TestJobQueue_GetActiveJobsMap(t *testing.T) {
	q, storage := setupTestQueue(t)
	ctx := context.Background()

	now := time.Now().UTC()

	queuedJob := &model.UpdateJob{
		ID:        "update/feed_q",
		FeedID:    "feed_q",
		Kind:      model.JobKindUpdate,
		Status:    model.JobStatusQueued,
		CreatedAt: now,
	}
	require.NoError(t, storage.SaveJob(ctx, queuedJob))

	runningJob := &model.UpdateJob{
		ID:        "update/feed_r",
		FeedID:    "feed_r",
		Kind:      model.JobKindUpdate,
		Status:    model.JobStatusRunning,
		CreatedAt: now,
	}
	require.NoError(t, storage.SaveJob(ctx, runningJob))

	doneJob := &model.UpdateJob{
		ID:         "update/feed_d",
		FeedID:     "feed_d",
		Kind:       model.JobKindUpdate,
		Status:     model.JobStatusDone,
		FinishedAt: &now,
		CreatedAt:  now,
	}
	require.NoError(t, storage.SaveJob(ctx, doneJob))

	activeMap, err := q.GetActiveJobsMap(ctx)
	require.NoError(t, err)
	assert.Len(t, activeMap, 2)
	assert.NotNil(t, activeMap["feed_q"])
	assert.NotNil(t, activeMap["feed_r"])
	assert.Nil(t, activeMap["feed_d"])
}

func TestJobQueue_SetPollPeriod(t *testing.T) {
	q, _ := setupTestQueue(t)
	assert.Equal(t, DefaultPollPeriod, q.pollPeriod)
	q.SetPollPeriod(10 * time.Second)
	assert.Equal(t, 10*time.Second, q.pollPeriod)
}

func TestJobQueue_PendingRerunWhileRunning(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	// 1. Initial enqueue & claim
	_, created, err := q.EnqueueUpdate(ctx, "feed_rerun")
	require.NoError(t, err)
	assert.True(t, created)

	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, model.JobStatusRunning, claimed.Status)
	assert.NotEmpty(t, claimed.ClaimToken)

	// 2. Retry or update triggered while running -> sets PendingRerun
	dupJob, created2, err := q.EnqueueManualUpdate(ctx, "feed_rerun")
	require.NoError(t, err)
	assert.False(t, created2)
	assert.True(t, dupJob.PendingRerun)

	// 3. Worker finishes and calls MarkDone -> must transition back to Queued!
	err = q.MarkDone(ctx, claimed.ID, claimed.ClaimToken)
	require.NoError(t, err)

	requeued, err := q.GetJob(ctx, claimed.ID)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusQueued, requeued.Status)
	assert.False(t, requeued.PendingRerun)
	assert.Nil(t, requeued.StartedAt)

	// 4. Next worker can now claim this rerun
	claimed2, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed2)
	assert.Equal(t, model.JobStatusRunning, claimed2.Status)

	// 5. Worker finishes this rerun with no further requests -> marks Done
	err = q.MarkDone(ctx, claimed2.ID, claimed2.ClaimToken)
	require.NoError(t, err)

	doneJob, err := q.GetJob(ctx, claimed2.ID)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusDone, doneJob.Status)
}

func TestJobQueue_ClaimTokenAndGenerationRevocation(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	// 1. Worker 1 claims job
	_, _, err := q.EnqueueUpdate(ctx, "feed_watchdog")
	require.NoError(t, err)

	w1Job, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, w1Job)
	w1Token := w1Job.ClaimToken
	assert.NotEmpty(t, w1Token)
	assert.Equal(t, uint64(1), w1Job.Generation)

	// 2. Watchdog runs (simulate 3 hours running)
	threeHoursAgo := time.Now().UTC().Add(-3 * time.Hour)
	w1Job.StartedAt = &threeHoursAgo
	require.NoError(t, q.db.SaveJob(ctx, w1Job))

	recovered, err := q.RecoverStaleRunning(ctx, 2*time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)

	// Verify job was revoked
	revokedJob, err := q.GetJob(ctx, w1Job.ID)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), revokedJob.Generation)
	assert.Empty(t, revokedJob.ClaimToken)
	assert.Equal(t, model.JobStatusQueued, revokedJob.Status)

	// 3. Stale Worker 1 finishes and tries to mark done -> MUST FAIL with ErrClaimExpired!
	err = q.MarkDone(ctx, w1Job.ID, w1Token)
	assert.Equal(t, ErrClaimExpired, err)

	// 4. Stale Worker 1 tries to mark failed -> MUST ALSO FAIL with ErrClaimExpired!
	_, err = q.MarkFailed(ctx, w1Job.ID, errors.New("timeout"), w1Token)
	assert.Equal(t, ErrClaimExpired, err)

	// 5. Worker 2 claims the recovered job
	w2Job, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, w2Job)
	assert.Equal(t, uint64(3), w2Job.Generation)
	assert.NotEqual(t, w1Token, w2Job.ClaimToken)

	// 6. Worker 2 successfully completes
	err = q.MarkDone(ctx, w2Job.ID, w2Job.ClaimToken)
	require.NoError(t, err)

	finalJob, err := q.GetJob(ctx, w2Job.ID)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusDone, finalJob.Status)
}

func TestJobQueue_YieldJob(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	_, _, err := q.EnqueueUpdate(ctx, "feed_yield")
	require.NoError(t, err)

	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	// Yield job with 200ms delay
	err = q.YieldJob(ctx, claimed.ID, claimed.ClaimToken, 200*time.Millisecond)
	require.NoError(t, err)

	// Cannot be claimed immediately
	claimedAgain, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	assert.Nil(t, claimedAgain)

	// Wait 250ms -> can be claimed now
	time.Sleep(250 * time.Millisecond)
	claimedThird, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimedThird)
	assert.Equal(t, model.JobStatusRunning, claimedThird.Status)
}

func TestJobQueue_PendingRerunOnFailedRunDoesNotBackoff(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	// 1. Initial enqueue & claim
	_, created, err := q.EnqueueUpdate(ctx, "feed_fail_rerun")
	require.NoError(t, err)
	assert.True(t, created)

	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, model.JobStatusRunning, claimed.Status)

	// 2. User retries episode while feed is currently running -> sets PendingRerun
	_, created2, err := q.EnqueueManualUpdate(ctx, "feed_fail_rerun")
	require.NoError(t, err)
	assert.False(t, created2)

	// 3. Current run fails with HTTP 429 rate limit
	// Normally 429 schedules 30m delay, BUT because PendingRerun is set, it MUST requeue immediately!
	now := time.Now().UTC()
	retried, err := q.MarkFailed(ctx, claimed.ID, errors.New("HTTP Error 429: Too Many Requests"), claimed.ClaimToken)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusQueued, retried.Status)
	assert.Equal(t, 0, retried.Attempts)
	assert.False(t, retried.PendingRerun)
	assert.True(t, retried.AvailableAt.Before(now.Add(2*time.Second)))

	// 4. Job can be claimed immediately without 30m wait
	claimedAgain, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimedAgain)
	assert.Equal(t, model.JobStatusRunning, claimedAgain.Status)
}

func TestJobQueue_MissingOrInvalidTokenRejected(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	_, _, err := q.EnqueueUpdate(ctx, "feed_token_enforce")
	require.NoError(t, err)

	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	// Empty token -> rejected
	err = q.MarkDone(ctx, claimed.ID, "")
	assert.Equal(t, ErrClaimExpired, err)

	_, err = q.MarkFailed(ctx, claimed.ID, errors.New("err"), "")
	assert.Equal(t, ErrClaimExpired, err)

	err = q.YieldJob(ctx, claimed.ID, "", time.Second)
	assert.Equal(t, ErrClaimExpired, err)

	// Wrong token -> rejected
	err = q.MarkDone(ctx, claimed.ID, "wrong-token")
	assert.Equal(t, ErrClaimExpired, err)

	// Valid token -> succeeds
	err = q.MarkDone(ctx, claimed.ID, claimed.ClaimToken)
	require.NoError(t, err)
}

func TestJobQueue_ScheduledUpdateRespectsBackoff(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	// 1. Initial enqueue & claim
	_, created, err := q.EnqueueScheduledUpdate(ctx, "feed_cron_backoff")
	require.NoError(t, err)
	assert.True(t, created)

	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	// 2. Job fails with HTTP 429 rate limit -> 30 minute backoff
	now := time.Now().UTC()
	retried, err := q.MarkFailed(ctx, claimed.ID, errors.New("HTTP Error 429: Too Many Requests"), claimed.ClaimToken)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusRetrying, retried.Status)
	assert.Equal(t, 1, retried.Attempts)
	originalAvailableAt := retried.AvailableAt
	assert.True(t, originalAvailableAt.After(now.Add(25*time.Minute)))

	// 3. Cron ticks and calls EnqueueScheduledUpdate -> MUST NOT reset backoff!
	schedJob, schedCreated, err := q.EnqueueScheduledUpdate(ctx, "feed_cron_backoff")
	require.NoError(t, err)
	assert.False(t, schedCreated)
	assert.Equal(t, originalAvailableAt, schedJob.AvailableAt)
	assert.Equal(t, model.JobStatusRetrying, schedJob.Status)
	assert.Equal(t, 1, schedJob.Attempts)

	// Verify worker cannot claim it (backoff respected)
	claimedPremature, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	assert.Nil(t, claimedPremature)

	// 4. User explicitly clicks "Update" -> EnqueueManualUpdate MUST reset backoff to now!
	manualJob, manualCreated, err := q.EnqueueManualUpdate(ctx, "feed_cron_backoff")
	require.NoError(t, err)
	assert.False(t, manualCreated)
	assert.True(t, manualJob.AvailableAt.Before(time.Now().UTC().Add(2*time.Second)))

	// Now worker can claim it immediately!
	claimedManual, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimedManual)
	assert.Equal(t, model.JobStatusRunning, claimedManual.Status)
}

func TestJobQueue_ScheduledUpdateDuringRunningDoesNotSetPendingRerun(t *testing.T) {
	q, _ := setupTestQueue(t)
	ctx := context.Background()

	// 1. Initial enqueue & claim
	_, created, err := q.EnqueueScheduledUpdate(ctx, "feed_cron_running")
	require.NoError(t, err)
	assert.True(t, created)

	claimed, err := q.ClaimNextJob(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, model.JobStatusRunning, claimed.Status)

	// 2. Cron ticks while update is running -> EnqueueScheduledUpdate MUST NOT set PendingRerun!
	schedJob, schedCreated, err := q.EnqueueScheduledUpdate(ctx, "feed_cron_running")
	require.NoError(t, err)
	assert.False(t, schedCreated)
	assert.False(t, schedJob.PendingRerun)

	// 3. Current run fails with HTTP 429
	// Since PendingRerun is false, MarkFailed MUST enforce full 30m backoff!
	now := time.Now().UTC()
	failedJob, err := q.MarkFailed(ctx, claimed.ID, errors.New("HTTP Error 429: Too Many Requests"), claimed.ClaimToken)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusRetrying, failedJob.Status)
	assert.Equal(t, 1, failedJob.Attempts)
	assert.True(t, failedJob.AvailableAt.After(now.Add(25*time.Minute)))
}
