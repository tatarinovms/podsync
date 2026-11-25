package main

// Runtime configuration for the optional Telegram bot. Configuration changes
// are validated, applied to the polling controller, and written atomically. If
// the config write fails, the controller and pairing state are rolled back so
// config.toml, the in-memory status, and the running bot never disagree.

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/pelletier/go-toml"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/telegram"
	"github.com/mxpv/podsync/services/web"
)

// maxTelegramUserID keeps the value within JavaScript's exact integer range.
const maxTelegramUserID int64 = 1<<53 - 1

type telegramRuntime interface {
	Status() web.TelegramStatus
	CheckToken(ctx context.Context, token, proxy string) error
	ApplyTransactional(cfg telegram.Config) (func() error, error)
	PairCode() (string, error)
	Disconnect() error
	Test(context.Context) error
}

type telegramManager struct {
	app     *AppFeedManager
	runtime telegramRuntime
	errMu   sync.RWMutex
	lastErr string
}

func NewTelegramManager(app *AppFeedManager, runtime telegramRuntime) *telegramManager {
	return &telegramManager{app: app, runtime: runtime}
}

func (t *telegramManager) current() telegram.Config {
	t.app.mu.RLock()
	defer t.app.mu.RUnlock()
	if t.app.cfg == nil {
		return telegram.Config{}
	}
	return t.app.cfg.Telegram
}

func (t *telegramManager) setConfig(cfg telegram.Config) {
	t.app.mu.Lock()
	if t.app.cfg != nil {
		t.app.cfg.Telegram = cfg
	}
	t.app.mu.Unlock()
}

func (t *telegramManager) setError(err error) {
	t.errMu.Lock()
	defer t.errMu.Unlock()
	if err == nil {
		t.lastErr = ""
		return
	}
	t.lastErr = err.Error()
}

func (t *telegramManager) lastError() string {
	t.errMu.RLock()
	defer t.errMu.RUnlock()
	return t.lastErr
}

func (t *telegramManager) Status() web.TelegramStatus {
	cfg := t.current()
	status := t.runtime.Status()
	status.Enabled = cfg.Enabled
	status.AllowedUserID = cfg.AllowedUserID
	status.Proxy = cfg.Proxy
	status.TokenConfigured = strings.TrimSpace(cfg.Token) != ""
	switch {
	case cfg.TokenFromEnv:
		status.TokenSource = "env"
	case status.TokenConfigured:
		status.TokenSource = "config"
	default:
		status.TokenSource = "none"
	}
	status.LastError = t.lastError()
	return status
}

func (t *telegramManager) Configure(ctx context.Context, update web.TelegramConfigUpdate) (web.TelegramStatus, error) {
	t.app.configWriteMu.Lock()
	defer t.app.configWriteMu.Unlock()

	current := t.current()
	next := current
	tokenProvided := update.Token != nil && strings.TrimSpace(*update.Token) != ""

	if current.TokenFromEnv && tokenProvided {
		return t.fail(errors.New("telegram token is supplied by PODSYNC_TELEGRAM_BOT_TOKEN; update the environment and restart Podsync"))
	}
	if update.Enabled != nil {
		next.Enabled = *update.Enabled
	}
	if tokenProvided {
		next.Token = strings.TrimSpace(*update.Token)
	}
	if update.AllowedUserID != nil {
		if *update.AllowedUserID < 0 || *update.AllowedUserID > maxTelegramUserID {
			return t.fail(errors.New("telegram user ID must be a non-negative integer"))
		}
		next.AllowedUserID = *update.AllowedUserID
	}
	if update.Proxy != nil {
		next.Proxy = strings.TrimSpace(*update.Proxy)
		if err := telegram.ValidateProxy(next.Proxy); err != nil {
			return t.fail(err)
		}
	}

	if next.Enabled && strings.TrimSpace(next.Token) == "" {
		return t.fail(errors.New("telegram token is required before the bot can be enabled"))
	}
	if next.Enabled || tokenProvided {
		if err := t.runtime.CheckToken(ctx, next.Token, next.Proxy); err != nil {
			return t.fail(err)
		}
	}

	tree, err := t.loadTree()
	if err != nil {
		return t.fail(err)
	}

	nextConfig := telegram.Config{Enabled: next.Enabled, Token: next.Token, AllowedUserID: next.AllowedUserID, Proxy: next.Proxy, TokenFromEnv: next.TokenFromEnv}
	rollback, err := t.runtime.ApplyTransactional(nextConfig)
	if err != nil {
		log.WithError(err).Warn("failed to apply Telegram configuration")
		return t.fail(errors.New("telegram configuration could not be applied; previous settings remain active"))
	}

	// Publish the new config before the file write so status and runtime agree.
	t.setConfig(next)
	if err := t.save(tree, next, tokenProvided); err != nil {
		t.setConfig(current)
		if rollbackErr := rollback(); rollbackErr != nil {
			log.WithError(rollbackErr).Error("failed to restore Telegram configuration")
			return t.fail(errors.New("failed to save Telegram configuration; previous bot state could not be restored"))
		}
		return t.fail(err)
	}
	t.setError(nil)
	return t.Status(), nil
}

func (t *telegramManager) fail(err error) (web.TelegramStatus, error) {
	t.setError(err)
	return t.Status(), err
}

func (t *telegramManager) loadTree() (*toml.Tree, error) {
	if t.app.configPath == "" {
		return nil, nil
	}
	tree, err := toml.LoadFile(t.app.configPath)
	if err != nil {
		return nil, errors.New("failed to load config file")
	}
	return tree, nil
}

// save writes only the fields controlled by the UI. An empty token input keeps
// the stored token; removing it requires ClearToken.
func (t *telegramManager) save(tree *toml.Tree, cfg telegram.Config, tokenProvided bool) error {
	if tree == nil {
		return nil
	}
	tree.Set("telegram.enabled", cfg.Enabled)
	tree.Set("telegram.allowed_user_id", cfg.AllowedUserID)
	if strings.TrimSpace(cfg.Proxy) != "" {
		tree.Set("telegram.proxy", cfg.Proxy)
	} else {
		tree.Delete("telegram.proxy")
	}
	if tokenProvided && !cfg.TokenFromEnv {
		tree.Set("telegram.token", cfg.Token)
	}
	if err := writeConfigTreeMode(t.app.configPath, tree, 0600); err != nil {
		return errors.New("failed to save config file")
	}
	return nil
}

func (t *telegramManager) ClearToken(ctx context.Context) (web.TelegramStatus, error) {
	t.app.configWriteMu.Lock()
	defer t.app.configWriteMu.Unlock()

	current := t.current()
	if current.TokenFromEnv {
		return t.fail(errors.New("telegram token is supplied by PODSYNC_TELEGRAM_BOT_TOKEN and cannot be removed here"))
	}
	if current.Enabled {
		return t.fail(errors.New("disable the Telegram bot before removing its token"))
	}
	tree, err := t.loadTree()
	if err != nil {
		return t.fail(err)
	}
	if tree != nil {
		tree.Delete("telegram.token")
	}

	next := current
	next.Token = ""
	next.Enabled = false
	nextConfig := telegram.Config{Enabled: false, Token: "", AllowedUserID: next.AllowedUserID, Proxy: next.Proxy}
	rollback, err := t.runtime.ApplyTransactional(nextConfig)
	if err != nil {
		log.WithError(err).Warn("failed to reset Telegram after token removal")
		return t.fail(errors.New("telegram token could not be removed; previous settings remain active"))
	}

	t.setConfig(next)
	if tree != nil {
		if err := writeConfigTreeMode(t.app.configPath, tree, 0600); err != nil {
			t.setConfig(current)
			if rollbackErr := rollback(); rollbackErr != nil {
				log.WithError(rollbackErr).Error("failed to restore Telegram configuration")
				return t.fail(errors.New("failed to remove Telegram token; previous bot state could not be restored"))
			}
			return t.fail(errors.New("failed to save config file"))
		}
	}
	t.setError(nil)
	return t.Status(), nil
}

func (t *telegramManager) PairCode() (string, error) {
	return t.runtime.PairCode()
}

func (t *telegramManager) Disconnect() error {
	return t.runtime.Disconnect()
}

func (t *telegramManager) Test(ctx context.Context) error {
	return t.runtime.Test(ctx)
}

var _ web.TelegramControl = (*telegramManager)(nil)

// A process crash between applying the controller state and writing config.toml
// leaves the new pairing state with the old config. That is the safe direction:
// the previous owner is disconnected and must pair again after restart.
