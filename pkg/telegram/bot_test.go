package telegram

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/personal"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type processFunc func(context.Context, *personal.Media) (io.ReadCloser, error)

func (f processFunc) Process(ctx context.Context, m *personal.Media) (io.ReadCloser, error) {
	return f(ctx, m)
}

func incoming(chat, user int64, text string) update {
	u := update{Message: &message{Text: text}}
	u.Message.Chat.ID, u.Message.Chat.Type, u.Message.From.ID = chat, "private", user
	return u
}
func pressed(chat, user int64, data string) update {
	u := update{Callback: &callback{ID: "callback", Message: &message{}, Data: data}}
	u.Callback.Message.Chat.ID, u.Callback.Message.Chat.Type, u.Callback.From.ID = chat, "private", user
	return u
}

func TestPairingAndPersonalURLFlow(t *testing.T) {
	ctx := context.Background()
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	files, err := fs.NewLocal(t.TempDir(), false, true)
	require.NoError(t, err)
	feeds, err := personal.New(ctx, database, files, processFunc(func(context.Context, *personal.Media) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("audio")), nil
	}), "https://example.org", nil, nil)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "telegram-state.json")
	bot, err := New(Config{Enabled: true, Token: "test"}, path, nil, feeds)
	require.NoError(t, err)
	var sent []string
	bot.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			data, _ := io.ReadAll(r.Body)
			sent = append(sent, string(data))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Header: make(http.Header)}, nil
	})}
	code, err := bot.PairCode()
	require.NoError(t, err)
	bot.handle(ctx, incoming(10, 10, "/start wrong"))
	require.False(t, bot.Status().Connected)
	expired := bot.codeTill
	bot.codeTill = expired.Add(-11 * time.Minute)
	bot.handle(ctx, incoming(10, 10, "/start "+code))
	require.False(t, bot.Status().Connected)
	bot.codeTill = expired
	bot.handle(ctx, incoming(10, 10, "/start "+code))
	require.True(t, bot.Status().Connected)
	bot.handle(ctx, incoming(20, 20, "https://example.org/forbidden.mp3"))
	require.Empty(t, bot.draft.URL)
	bot.handle(ctx, incoming(10, 10, "https://example.org/audio.mp3"))
	nonce := bot.draft.Nonce
	bot.handle(ctx, pressed(10, 20, "feed:personal:"+nonce))
	require.Empty(t, bot.draft.FeedID)
	bot.handle(ctx, pressed(10, 10, "feed:personal:stale"))
	require.Empty(t, bot.draft.FeedID)
	bot.handle(ctx, pressed(10, 10, "feed:personal:"+nonce))
	bot.handle(ctx, pressed(10, 10, "format:audio:"+nonce))
	bot.handle(ctx, pressed(10, 10, "quality:high:"+nonce))
	require.Len(t, bot.state.Awaiting, 1)
	require.NoError(t, feeds.Update(ctx, "personal"))
	bot.ReconcileFeed(ctx, "personal", false)
	bot.flush(ctx)
	require.Empty(t, bot.state.Awaiting)
	require.Contains(t, strings.Join(sent, " "), "Download completed")

	reloaded, err := New(Config{Enabled: true, Token: "test"}, path, nil, feeds)
	require.NoError(t, err)
	require.True(t, reloaded.Status().Connected)
	require.Empty(t, reloaded.state.Outbox)
	require.NoError(t, reloaded.Disconnect())
	require.False(t, reloaded.Status().Connected)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestTelegramDeliveryFailureKeepsOutbox(t *testing.T) {
	bot, err := New(Config{Enabled: true, Token: "test"}, filepath.Join(t.TempDir(), "state.json"), nil, nil)
	require.NoError(t, err)
	bot.state.ChatID = 10
	bot.state.Outbox = []notice{{Text: "failure"}}
	bot.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	bot.flush(context.Background())
	require.Len(t, bot.state.Outbox, 1)
}

func TestJobFailureIsDeduplicatedAndRecovered(t *testing.T) {
	ctx := context.Background()
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	files, err := fs.NewLocal(t.TempDir(), false, true)
	require.NoError(t, err)
	feeds, err := personal.New(ctx, database, files, nil, "https://example.org", nil, nil)
	require.NoError(t, err)
	statePath := filepath.Join(t.TempDir(), "state.json")
	bot, err := New(Config{Enabled: true, Token: "test"}, statePath, nil, feeds)
	require.NoError(t, err)
	bot.state.ChatID = 10
	bot.NotifyJobFailure("personal", errors.New("network failed"))
	bot.ReconcileFeed(ctx, "personal", true)
	bot.NotifyJobFailure("personal", errors.New("network failed"))
	require.Len(t, bot.state.Outbox, 1)
	reloaded, err := New(Config{Enabled: true, Token: "test"}, statePath, nil, feeds)
	require.NoError(t, err)
	reloaded.NotifyJobFailure("personal", errors.New("network failed"))
	require.Len(t, reloaded.state.Outbox, 1)
	bot.ReconcileFeed(ctx, "personal", false)
	require.Len(t, bot.state.Outbox, 2)
	require.Contains(t, bot.state.Outbox[1].Text, "recovered")
	bot.ReconcileFeed(ctx, "personal", false)
	require.Len(t, bot.state.Outbox, 2)
}
