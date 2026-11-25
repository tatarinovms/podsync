package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pelletier/go-toml"
	"github.com/stretchr/testify/require"

	"github.com/mxpv/podsync/pkg/telegram"
	"github.com/mxpv/podsync/services/web"
)

type stubTelegramRuntime struct {
	status         web.TelegramStatus
	checkErr       error
	applyErr       error
	rollbackErr    error
	checked        []string
	checkedProxies []string
	applied        []telegram.Config
	rollbacks      int
	pairCode       string
	pairErr        error
	disconnectErr  error
	testErr        error
}

func (s *stubTelegramRuntime) Status() web.TelegramStatus { return s.status }
func (s *stubTelegramRuntime) CheckToken(_ context.Context, token, proxy string) error {
	s.checked = append(s.checked, token)
	s.checkedProxies = append(s.checkedProxies, proxy)
	return s.checkErr
}
func (s *stubTelegramRuntime) ApplyTransactional(cfg telegram.Config) (func() error, error) {
	if s.applyErr != nil {
		return nil, s.applyErr
	}
	s.applied = append(s.applied, cfg)
	return func() error {
		s.rollbacks++
		return s.rollbackErr
	}, nil
}
func (s *stubTelegramRuntime) PairCode() (string, error)  { return s.pairCode, s.pairErr }
func (s *stubTelegramRuntime) Disconnect() error          { return s.disconnectErr }
func (s *stubTelegramRuntime) Test(context.Context) error { return s.testErr }

type telegramFixture struct {
	manager *telegramManager
	runtime *stubTelegramRuntime
	app     *AppFeedManager
	path    string
}

func newTelegramFixture(t *testing.T, content string, cfg telegram.Config) *telegramFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	require.NoError(t, os.Chmod(path, 0644))
	app := &AppFeedManager{configPath: path, cfg: &Config{Telegram: cfg}}
	runtime := &stubTelegramRuntime{}
	return &telegramFixture{manager: NewTelegramManager(app, runtime), runtime: runtime, app: app, path: path}
}

func loadTelegramTree(t *testing.T, path string) *toml.Tree {
	t.Helper()
	tree, err := toml.LoadFile(path)
	require.NoError(t, err)
	return tree
}

func TestTelegramManagerConfigureValidatesAndPersists(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\ntoken = \"old-token\"\n", telegram.Config{Token: "old-token"})
	enabled := true
	token := "new-token"
	status, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &enabled, Token: &token})
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.True(t, status.TokenConfigured)
	require.Equal(t, "config", status.TokenSource)
	require.Equal(t, []string{"new-token"}, fixture.runtime.checked)
	require.Equal(t, []telegram.Config{{Enabled: true, Token: "new-token"}}, fixture.runtime.applied)
	require.Equal(t, "new-token", fixture.app.cfg.Telegram.Token)
	require.True(t, fixture.app.cfg.Telegram.Enabled)

	tree := loadTelegramTree(t, fixture.path)
	require.Equal(t, true, tree.Get("telegram.enabled"))
	require.Equal(t, "new-token", tree.Get("telegram.token"))
	info, err := os.Stat(fixture.path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm(), "config with a token must be private")

	body, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(body), "new-token")
}

func TestTelegramManagerKeepsConfigWhenTokenCheckFails(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\ntoken = \"old-token\"\n", telegram.Config{Token: "old-token"})
	fixture.runtime.checkErr = errors.New("telegram token check failed: Unauthorized")
	enabled := true
	bad := "bad-token"
	_, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &enabled, Token: &bad})
	require.ErrorContains(t, err, "Unauthorized")
	require.NotContains(t, err.Error(), bad)
	require.False(t, fixture.app.cfg.Telegram.Enabled)
	require.Equal(t, "old-token", fixture.app.cfg.Telegram.Token)
	require.Empty(t, fixture.runtime.applied)
	tree := loadTelegramTree(t, fixture.path)
	require.Equal(t, false, tree.Get("telegram.enabled"))
	require.Equal(t, "old-token", tree.Get("telegram.token"))
	require.Contains(t, fixture.manager.Status().LastError, "Unauthorized")
}

func TestTelegramManagerRejectsEnabledWithoutToken(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\n", telegram.Config{})
	enabled := true
	_, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &enabled})
	require.ErrorContains(t, err, "token is required")
	require.Empty(t, fixture.runtime.checked)
	require.Empty(t, fixture.runtime.applied)
}

func TestTelegramManagerEnvironmentTokenIsReadOnly(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\n", telegram.Config{Token: "env-token", TokenFromEnv: true})
	bad := "replacement"
	_, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Token: &bad})
	require.ErrorContains(t, err, "PODSYNC_TELEGRAM_BOT_TOKEN")
	require.Empty(t, fixture.runtime.checked)

	enabled := true
	status, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &enabled})
	require.NoError(t, err)
	require.Equal(t, []string{"env-token"}, fixture.runtime.checked)
	require.Equal(t, "env", status.TokenSource)
	require.True(t, status.TokenConfigured)
	tree := loadTelegramTree(t, fixture.path)
	require.Equal(t, true, tree.Get("telegram.enabled"))
	require.Nil(t, tree.Get("telegram.token"), "environment token must not be written to config.toml")

	_, err = fixture.manager.ClearToken(context.Background())
	require.ErrorContains(t, err, "cannot be removed")
}

func TestTelegramManagerClearToken(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\ntoken = \"stored-token\"\n", telegram.Config{Token: "stored-token"})
	status, err := fixture.manager.ClearToken(context.Background())
	require.NoError(t, err)
	require.False(t, status.TokenConfigured)
	require.Equal(t, "none", status.TokenSource)
	require.Empty(t, fixture.app.cfg.Telegram.Token)
	require.Equal(t, []telegram.Config{{Enabled: false, Token: ""}}, fixture.runtime.applied)
	tree := loadTelegramTree(t, fixture.path)
	require.Nil(t, tree.Get("telegram.token"))
	info, err := os.Stat(fixture.path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	fixture.runtime.applied = nil
	fixture.app.cfg.Telegram = telegram.Config{Enabled: true, Token: "stored-token"}
	_, err = fixture.manager.ClearToken(context.Background())
	require.ErrorContains(t, err, "disable the Telegram bot")
	require.Empty(t, fixture.runtime.applied)
}

func TestTelegramManagerKeepsConfigWhenWriteFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only directories")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[telegram]\nenabled = false\ntoken = \"old-token\"\n"), 0600))
	app := &AppFeedManager{configPath: path, cfg: &Config{Telegram: telegram.Config{Token: "old-token"}}}
	runtimeStub := &stubTelegramRuntime{}
	manager := NewTelegramManager(app, runtimeStub)
	require.NoError(t, os.Chmod(dir, 0500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	enabled := true
	token := "new-token"
	_, err := manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &enabled, Token: &token})
	require.ErrorContains(t, err, "failed to save config file")
	require.False(t, app.cfg.Telegram.Enabled)
	require.Equal(t, "old-token", app.cfg.Telegram.Token)
	require.Len(t, runtimeStub.applied, 1, "the bot is applied before the config write")
	require.Equal(t, 1, runtimeStub.rollbacks, "a failed config write must roll back the bot")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(content), "old-token")
	require.NotContains(t, string(content), "new-token")
}

func TestTelegramManagerReportsApplyFailureWithoutToken(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\ntoken = \"old-token\"\n", telegram.Config{Token: "old-token"})
	fixture.runtime.applyErr = errors.New("reset telegram pairing: state directory is read-only")
	enabled := true
	token := "new-token"
	_, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &enabled, Token: &token})
	require.ErrorContains(t, err, "could not be applied")
	require.NotContains(t, err.Error(), "new-token")
	require.NotContains(t, strings.ToLower(fixture.manager.Status().LastError), "new-token")
	require.False(t, fixture.app.cfg.Telegram.Enabled)
	require.Equal(t, "old-token", fixture.app.cfg.Telegram.Token)
	tree := loadTelegramTree(t, fixture.path)
	require.Equal(t, false, tree.Get("telegram.enabled"))
	require.Equal(t, "old-token", tree.Get("telegram.token"))
	require.Empty(t, fixture.runtime.rollbacks)
}

func TestTelegramManagerAllowedUserIDPersistsAndReports(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\n", telegram.Config{})
	allowed := int64(123456789)
	status, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{AllowedUserID: &allowed})
	require.NoError(t, err)
	require.Equal(t, int64(123456789), status.AllowedUserID)
	require.Len(t, fixture.runtime.applied, 1)
	require.Equal(t, int64(123456789), fixture.runtime.applied[0].AllowedUserID)
	tree := loadTelegramTree(t, fixture.path)
	require.Equal(t, int64(123456789), tree.Get("telegram.allowed_user_id"))

	// Omitting the field keeps the value; explicit zero removes the restriction.
	status, err = fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{})
	require.NoError(t, err)
	require.Equal(t, int64(123456789), status.AllowedUserID)
	zero := int64(0)
	status, err = fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{AllowedUserID: &zero})
	require.NoError(t, err)
	require.Zero(t, status.AllowedUserID)
	tree = loadTelegramTree(t, fixture.path)
	require.Equal(t, int64(0), tree.Get("telegram.allowed_user_id"))
}

func TestTelegramManagerRejectsInvalidAllowedUserID(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\n", telegram.Config{})
	for _, value := range []int64{-1, maxTelegramUserID + 1} {
		_, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{AllowedUserID: &value})
		require.ErrorContains(t, err, "non-negative integer")
	}
	require.Empty(t, fixture.runtime.applied)
	require.Empty(t, fixture.runtime.checked)
}

func TestTelegramManagerRollsBackWhenApplyFails(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\ntoken = \"old-token\"\n", telegram.Config{Token: "old-token"})
	fixture.runtime.applyErr = errors.New("reset telegram pairing: state directory is read-only")
	enabled := true
	token := "new-token"
	_, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &enabled, Token: &token})
	require.ErrorContains(t, err, "previous settings remain active")
	require.False(t, fixture.app.cfg.Telegram.Enabled)
	require.Equal(t, "old-token", fixture.app.cfg.Telegram.Token)
	tree := loadTelegramTree(t, fixture.path)
	require.Equal(t, false, tree.Get("telegram.enabled"))
	require.Equal(t, "old-token", tree.Get("telegram.token"))
	require.Zero(t, fixture.runtime.rollbacks)
}

func TestTelegramManagerRollsBackWhenConfigWriteFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only directories")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[telegram]\nenabled = false\ntoken = \"old-token\"\n"), 0600))
	app := &AppFeedManager{configPath: path, cfg: &Config{Telegram: telegram.Config{Token: "old-token"}}}
	runtimeStub := &stubTelegramRuntime{}
	manager := NewTelegramManager(app, runtimeStub)
	require.NoError(t, os.Chmod(dir, 0500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	disabled := false
	allowed := int64(42)
	_, err := manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &disabled, AllowedUserID: &allowed})
	require.ErrorContains(t, err, "failed to save config file")
	require.Equal(t, int64(0), app.cfg.Telegram.AllowedUserID)
	require.Equal(t, 1, runtimeStub.rollbacks)
}

func TestTelegramManagerProxyValidationAndPersistence(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\n", telegram.Config{})
	proxy := "socks5://user:pass@127.0.0.1:1080"
	status, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Proxy: &proxy})
	require.NoError(t, err)
	require.Equal(t, proxy, status.Proxy)
	require.Len(t, fixture.runtime.applied, 1)
	require.Equal(t, proxy, fixture.runtime.applied[0].Proxy)
	tree := loadTelegramTree(t, fixture.path)
	require.Equal(t, proxy, tree.Get("telegram.proxy"))

	bad := "ftp://127.0.0.1:21"
	_, err = fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Proxy: &bad})
	require.ErrorContains(t, err, "http, https, socks5")
	require.Len(t, fixture.runtime.applied, 1, "invalid proxy must not be applied")
	require.Equal(t, proxy, fixture.app.cfg.Telegram.Proxy)
	require.Equal(t, proxy, loadTelegramTree(t, fixture.path).Get("telegram.proxy"))

	empty := ""
	status, err = fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Proxy: &empty})
	require.NoError(t, err)
	require.Empty(t, status.Proxy)
	require.Len(t, fixture.runtime.applied, 2)
	require.Nil(t, loadTelegramTree(t, fixture.path).Get("telegram.proxy"))
}

func TestTelegramManagerValidatesTokenThroughProxy(t *testing.T) {
	fixture := newTelegramFixture(t, "[telegram]\nenabled = false\n", telegram.Config{})
	proxy := "http://127.0.0.1:8080"
	enabled := true
	token := "token"
	_, err := fixture.manager.Configure(context.Background(), web.TelegramConfigUpdate{Enabled: &enabled, Token: &token, Proxy: &proxy})
	require.NoError(t, err)
	require.Equal(t, []string{proxy}, fixture.runtime.checkedProxies)
	require.Equal(t, []telegram.Config{{Enabled: true, Token: "token", Proxy: proxy}}, fixture.runtime.applied)
}
