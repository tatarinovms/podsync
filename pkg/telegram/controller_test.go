package telegram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestControllerCheckToken(t *testing.T) {
	controller := &Controller{baseURL: "https://api.telegram.org"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botvalid/getMe" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"result":{"id":42,"is_bot":true,"username":"podsync_bot"}}`)
	}))
	defer server.Close()
	controller.baseURL = server.URL
	require.NoError(t, controller.CheckToken(context.Background(), "valid", ""))

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":false,"description":"Unauthorized"}`)
	}))
	defer bad.Close()
	controller.baseURL = bad.URL
	err := controller.CheckToken(context.Background(), "secret-token", "")
	require.ErrorContains(t, err, "Unauthorized")
	require.NotContains(t, err.Error(), "secret-token")

	notBot := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"result":{"id":0,"is_bot":false}}`)
	}))
	defer notBot.Close()
	controller.baseURL = notBot.URL
	err = controller.CheckToken(context.Background(), "secret-token", "")
	require.ErrorContains(t, err, "not a bot")

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer slow.Close()
	controller.baseURL = slow.URL
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = controller.CheckToken(ctx, "secret-token", "")
	require.ErrorContains(t, err, "timed out")
	require.NotContains(t, err.Error(), "secret-token")
}

func TestControllerPollingLifecycle(t *testing.T) {
	var active, maxActive, updates int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/deleteWebhook"):
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Header: make(http.Header)}, nil
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			current := atomic.AddInt32(&active, 1)
			defer atomic.AddInt32(&active, -1)
			for {
				previous := atomic.LoadInt32(&maxActive)
				if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
					break
				}
			}
			atomic.AddInt32(&updates, 1)
			select {
			case <-r.Context().Done():
				return nil, r.Context().Err()
			case <-time.After(10 * time.Millisecond):
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":[]}`)), Header: make(http.Header)}, nil
		default:
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Header: make(http.Header)}, nil
		}
	})

	statePath := filepath.Join(t.TempDir(), "telegram-state.json")
	controller, err := NewController(Config{}, statePath, nil, nil)
	require.NoError(t, err)
	controller.bot.SetHTTPClient(&http.Client{Transport: transport})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		require.NoError(t, controller.Run(ctx))
	}()

	require.NoError(t, controller.Apply(Config{Enabled: true, Token: "token"}))
	require.Eventually(t, func() bool { return atomic.LoadInt32(&updates) > 0 }, 2*time.Second, 5*time.Millisecond)
	require.True(t, controller.Status().Running)

	// Re-applying the same configuration must not create a second poller.
	require.NoError(t, controller.Apply(Config{Enabled: true, Token: "token"}))
	require.Eventually(t, func() bool { return atomic.LoadInt32(&updates) > 1 }, 2*time.Second, 5*time.Millisecond)
	require.Equal(t, int32(1), atomic.LoadInt32(&maxActive))

	// A token change stops the old poller, clears the owner, and starts one new poller.
	controller.bot.mu.Lock()
	controller.bot.state.ChatID, controller.bot.state.UserID, controller.bot.state.Offset = 10, 10, 99
	controller.bot.mu.Unlock()
	require.NoError(t, controller.Apply(Config{Enabled: true, Token: "new-token"}))
	controller.bot.mu.Lock()
	require.Zero(t, controller.bot.state.ChatID)
	require.Zero(t, controller.bot.state.UserID)
	require.Zero(t, controller.bot.state.Offset)
	controller.bot.mu.Unlock()
	require.True(t, controller.Status().Running)
	require.Equal(t, int32(1), atomic.LoadInt32(&maxActive))

	// Disabling keeps the paired owner and queued state for the next enable.
	controller.bot.mu.Lock()
	controller.bot.state.ChatID, controller.bot.state.UserID = 55, 55
	controller.bot.state.Offset = 123
	controller.bot.mu.Unlock()
	require.NoError(t, controller.Apply(Config{Enabled: false, Token: "new-token"}))
	require.False(t, controller.Status().Running)
	controller.bot.mu.Lock()
	require.Equal(t, int64(55), controller.bot.state.ChatID)
	require.Equal(t, int64(123), controller.bot.state.Offset)
	controller.bot.mu.Unlock()
	stoppedAt := atomic.LoadInt32(&updates)
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, stoppedAt, atomic.LoadInt32(&updates))

	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("controller Run did not stop")
	}
}

func TestControllerAllowedUserIDChangesOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	controller, err := NewController(Config{Token: "token"}, path, nil, nil)
	require.NoError(t, err)
	controller.bot.mu.Lock()
	controller.bot.state.ChatID, controller.bot.state.UserID, controller.bot.state.Offset = 10, 10, 77
	controller.bot.code = "active-code"
	require.NoError(t, controller.bot.saveLocked())
	controller.bot.mu.Unlock()

	_, err = controller.ApplyTransactional(Config{Token: "token", AllowedUserID: 10})
	require.NoError(t, err)
	controller.bot.mu.Lock()
	require.Equal(t, int64(10), controller.bot.state.UserID)
	require.Equal(t, int64(77), controller.bot.state.Offset)
	require.Equal(t, "active-code", controller.bot.code, "a matching restriction preserves the owner")
	controller.bot.mu.Unlock()

	_, err = controller.ApplyTransactional(Config{Token: "token", AllowedUserID: 20})
	require.NoError(t, err)
	controller.bot.mu.Lock()
	require.Zero(t, controller.bot.state.ChatID)
	require.Zero(t, controller.bot.state.UserID)
	require.Equal(t, int64(77), controller.bot.state.Offset, "the same token keeps its update offset")
	require.Empty(t, controller.bot.code)
	require.Empty(t, controller.bot.draft)
	controller.bot.mu.Unlock()

	controller.bot.mu.Lock()
	controller.bot.state.ChatID, controller.bot.state.UserID = 20, 20
	require.NoError(t, controller.bot.saveLocked())
	controller.bot.mu.Unlock()
	_, err = controller.ApplyTransactional(Config{Token: "token", AllowedUserID: 0})
	require.NoError(t, err)
	controller.bot.mu.Lock()
	require.Equal(t, int64(20), controller.bot.state.UserID, "removing the restriction must not reset the owner")
	controller.bot.mu.Unlock()
}

func TestControllerApplyTransactionalRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	controller, err := NewController(Config{Token: "old-token"}, path, nil, nil)
	require.NoError(t, err)
	controller.bot.mu.Lock()
	controller.bot.state.ChatID, controller.bot.state.UserID, controller.bot.state.Offset = 10, 10, 42
	controller.bot.state.Outbox = []notice{{Text: "keep me"}}
	require.NoError(t, controller.bot.saveLocked())
	controller.bot.mu.Unlock()

	rollback, err := controller.ApplyTransactional(Config{Token: "new-token", AllowedUserID: 99})
	require.NoError(t, err)
	controller.bot.mu.Lock()
	require.Zero(t, controller.bot.state.ChatID)
	require.Equal(t, int64(99), controller.bot.Config().AllowedUserID)
	controller.bot.mu.Unlock()

	require.NoError(t, rollback())
	config := controller.bot.Config()
	require.Equal(t, "old-token", config.Token)
	require.Zero(t, config.AllowedUserID)
	controller.bot.mu.Lock()
	require.Equal(t, int64(10), controller.bot.state.ChatID)
	require.Equal(t, int64(10), controller.bot.state.UserID)
	require.Equal(t, int64(42), controller.bot.state.Offset)
	require.Equal(t, []notice{{Text: "keep me"}}, controller.bot.state.Outbox)
	controller.bot.mu.Unlock()
}

func TestControllerApplyFailsBeforeChangingConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only directories")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	controller, err := NewController(Config{Token: "old-token"}, path, nil, nil)
	require.NoError(t, err)
	controller.bot.mu.Lock()
	controller.bot.state.ChatID, controller.bot.state.UserID, controller.bot.state.Offset = 10, 10, 42
	require.NoError(t, controller.bot.saveLocked())
	controller.bot.mu.Unlock()
	require.NoError(t, os.Chmod(dir, 0500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	_, err = controller.ApplyTransactional(Config{Token: "new-token"})
	require.ErrorContains(t, err, "reset telegram pairing")
	require.Equal(t, "old-token", controller.bot.Config().Token)
	controller.bot.mu.Lock()
	require.Equal(t, int64(10), controller.bot.state.UserID)
	require.Equal(t, int64(42), controller.bot.state.Offset)
	controller.bot.mu.Unlock()
}

func TestAllowedUserIDConcurrentUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	controller, err := NewController(Config{Token: "token", AllowedUserID: 10}, path, nil, nil)
	require.NoError(t, err)
	controller.bot.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Header: make(http.Header)}, nil
	})})
	controller.bot.mu.Lock()
	controller.bot.state.ChatID, controller.bot.state.UserID = 10, 10
	controller.bot.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			controller.bot.handle(context.Background(), incoming(10, 10, "/cancel"))
		}()
		go func(i int) {
			defer wg.Done()
			allowed := []int64{0, 10, 20}[i%3]
			if err := controller.Apply(Config{Token: "token", AllowedUserID: allowed}); err != nil {
				t.Errorf("apply allowed user ID: %v", err)
			}
		}(i)
	}
	wg.Wait()
	controller.Stop()
}

func TestControllerProxyChangeKeepsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	controller, err := NewController(Config{Token: "token"}, path, nil, nil)
	require.NoError(t, err)
	controller.bot.mu.Lock()
	controller.bot.state.ChatID, controller.bot.state.UserID, controller.bot.state.Offset = 10, 10, 42
	require.NoError(t, controller.bot.saveLocked())
	controller.bot.mu.Unlock()
	before := controller.bot.httpClient()

	rollback, err := controller.ApplyTransactional(Config{Token: "token", Proxy: "socks5://127.0.0.1:1080"})
	require.NoError(t, err)
	require.Equal(t, "socks5://127.0.0.1:1080", controller.bot.Config().Proxy)
	require.NotSame(t, before, controller.bot.httpClient())
	controller.bot.mu.Lock()
	require.Equal(t, int64(10), controller.bot.state.UserID, "proxy changes must not disconnect the owner")
	require.Equal(t, int64(42), controller.bot.state.Offset)
	controller.bot.mu.Unlock()

	require.NoError(t, rollback())
	require.Empty(t, controller.bot.Config().Proxy)
	controller.bot.mu.Lock()
	require.Equal(t, int64(10), controller.bot.state.UserID)
	controller.bot.mu.Unlock()

	_, err = controller.ApplyTransactional(Config{Token: "token", Proxy: "ftp://127.0.0.1:21"})
	require.ErrorContains(t, err, "configure telegram proxy")
	require.Empty(t, controller.bot.Config().Proxy)
	controller.bot.mu.Lock()
	require.Equal(t, int64(10), controller.bot.state.UserID)
	controller.bot.mu.Unlock()
}
