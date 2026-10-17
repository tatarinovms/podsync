package logbuffer

import (
	"errors"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestBufferBasicAndEviction(t *testing.T) {
	buf := New(3)

	logger := logrus.New()
	logger.SetOutput(&testNullWriter{})
	logger.AddHook(buf)

	logger.Info("message 1")
	logger.Warn("message 2")
	logger.Error("message 3")

	res := buf.Query(Filter{Level: "all"})
	assert.Equal(t, 3, res.Total)
	assert.Equal(t, 3, len(res.Entries))
	assert.Equal(t, "message 1", res.Entries[0].Message)
	assert.Equal(t, "message 3", res.Entries[2].Message)

	// Add 4th message - should evict message 1
	logger.Info("message 4")
	res = buf.Query(Filter{Level: "all"})
	assert.Equal(t, 3, res.Total)
	assert.Equal(t, 3, len(res.Entries))
	assert.Equal(t, "message 2", res.Entries[0].Message)
	assert.Equal(t, "message 4", res.Entries[2].Message)
}

func TestBufferFiltering(t *testing.T) {
	buf := New(10)
	logger := logrus.New()
	logger.SetOutput(&testNullWriter{})
	logger.AddHook(buf)

	logger.WithField("feed_id", "vk_show").Info("started update")
	logger.WithField("feed_id", "vk_show").WithError(errors.New("VK API token is required")).Error("failed vk")
	logger.WithField("feed_id", "yt_pod").Warn("yt-dlp rate limit warning")
	logger.WithField("other", "val").Info("regular log")

	// Filter by level error
	errRes := buf.Query(Filter{Level: "error"})
	assert.Equal(t, 1, len(errRes.Entries))
	assert.Equal(t, "failed vk", errRes.Entries[0].Message)

	// Filter by search
	searchRes := buf.Query(Filter{Search: "vk_show"})
	assert.Equal(t, 2, len(searchRes.Entries))

	// Filter by feed_id
	feedRes := buf.Query(Filter{FeedID: "yt_pod"})
	assert.Equal(t, 1, len(feedRes.Entries))
	assert.Equal(t, "yt-dlp rate limit warning", feedRes.Entries[0].Message)

	// Plain text format
	txt := buf.FormatPlainText(Filter{Level: "error"})
	assert.Contains(t, txt, "[ERROR]")
	assert.Contains(t, txt, "failed vk")

	// Clear
	buf.Clear()
	emptyRes := buf.Query(Filter{})
	assert.Equal(t, 0, emptyRes.Total)
}

func TestBufferSinceID(t *testing.T) {
	buf := New(10)
	logger := logrus.New()
	logger.SetOutput(&testNullWriter{})
	logger.AddHook(buf)

	logger.Info("entry 1")
	logger.Info("entry 2")
	logger.Info("entry 3")

	res := buf.Query(Filter{})
	assert.Equal(t, 3, len(res.Entries))
	id2 := res.Entries[1].ID

	sinceRes := buf.Query(Filter{SinceID: id2})
	assert.Equal(t, 1, len(sinceRes.Entries))
	assert.Equal(t, "entry 3", sinceRes.Entries[0].Message)
}

type testNullWriter struct{}

func (w *testNullWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}
