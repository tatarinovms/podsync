package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/personal"
	"github.com/mxpv/podsync/services/web"
)

// Controller owns the long-lived bot and its polling lifecycle. Web Admin can
// disable, enable, or replace the token at runtime; polling is always stopped
// and awaited before a new configuration is applied, so a token never has two
// concurrent getUpdates loops.
type Controller struct {
	opMu sync.Mutex

	mu      sync.Mutex
	bot     *Bot
	parent  context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	running bool

	baseURL string
}

func NewController(cfg Config, path string, mgr web.AdminManager, personalFeeds *personal.Service) (*Controller, error) {
	bot, err := New(cfg, path, mgr, personalFeeds)
	if err != nil {
		return nil, err
	}
	return &Controller{
		bot:     bot,
		baseURL: "https://api.telegram.org",
	}, nil
}

// Run blocks until ctx is cancelled and keeps the poller state in sync with
// runtime configuration changes made through Apply.
func (c *Controller) Run(ctx context.Context) error {
	c.mu.Lock()
	c.parent = ctx
	c.mu.Unlock()

	c.opMu.Lock()
	c.startLocked()
	c.opMu.Unlock()

	<-ctx.Done()
	c.Stop()
	return nil
}

func (c *Controller) Stop() {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	c.stopLocked()
}

// Apply stops the current poller, applies cfg, and restarts polling when the
// bot is enabled. A token change clears the previous owner before starting.
func (c *Controller) Apply(cfg Config) error {
	c.opMu.Lock()
	defer c.opMu.Unlock()

	c.stopLocked()
	previousCfg := c.bot.Config()
	if err := c.applyLocked(cfg); err != nil {
		_ = c.bot.SetProxy(previousCfg.Proxy)
		c.bot.SetConfig(previousCfg)
		c.startLocked()
		return err
	}
	return nil
}

// ApplyTransactional applies cfg and returns a rollback that restores the
// previous pairing state and poller. The caller uses it when persisting the
// configuration can still fail, so config.toml and the running bot never
// disagree after an error.
func (c *Controller) ApplyTransactional(cfg Config) (func() error, error) {
	c.opMu.Lock()
	c.stopLocked()
	previousCfg := c.bot.Config()
	snapshot := c.bot.Snapshot()
	if err := c.applyLocked(cfg); err != nil {
		_ = c.bot.SetProxy(previousCfg.Proxy)
		c.bot.SetConfig(previousCfg)
		c.startLocked()
		c.opMu.Unlock()
		return nil, err
	}
	c.opMu.Unlock()

	var once sync.Once
	var rollbackErr error
	rollback := func() error {
		once.Do(func() {
			c.opMu.Lock()
			defer c.opMu.Unlock()
			c.stopLocked()
			if err := c.bot.Restore(snapshot); err != nil {
				rollbackErr = fmt.Errorf("restore telegram state: %w", err)
			}
			_ = c.bot.SetProxy(previousCfg.Proxy)
			c.bot.SetConfig(previousCfg)
			c.startLocked()
		})
		return rollbackErr
	}
	return rollback, nil
}

// applyLocked assumes the poller is stopped and opMu is held.
func (c *Controller) applyLocked(cfg Config) error {
	oldToken := c.bot.Token()
	oldAllowedUserID := c.bot.AllowedUserID()
	oldProxy := c.bot.Config().Proxy
	if cfg.Proxy != oldProxy {
		if err := c.bot.SetProxy(cfg.Proxy); err != nil {
			return fmt.Errorf("configure telegram proxy: %w", err)
		}
	}
	c.bot.SetConfig(cfg)
	tokenChanged := strings.TrimSpace(cfg.Token) != strings.TrimSpace(oldToken)
	allowedChanged := cfg.AllowedUserID != oldAllowedUserID
	reset := tokenChanged
	if allowedChanged && cfg.AllowedUserID != 0 && c.bot.OwnerUserID() != cfg.AllowedUserID {
		reset = true
	}
	if reset {
		var err error
		if tokenChanged {
			err = c.bot.ResetForTokenChange()
		} else {
			err = c.bot.ResetOwner()
		}
		if err != nil {
			return fmt.Errorf("reset telegram pairing: %w", err)
		}
	}
	c.startLocked()
	return nil
}

func (c *Controller) startLocked() {
	c.mu.Lock()
	if c.running || !c.bot.Enabled() || strings.TrimSpace(c.bot.Token()) == "" {
		c.mu.Unlock()
		return
	}
	parent := c.parent
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	c.cancel, c.done, c.running = cancel, done, true
	c.mu.Unlock()

	go func() {
		defer close(done)
		if err := c.bot.Run(ctx); err != nil && ctx.Err() == nil {
			log.WithError(err).Warn("telegram polling stopped")
		}
		c.mu.Lock()
		if c.done == done {
			c.running = false
		}
		c.mu.Unlock()
	}()
}

func (c *Controller) stopLocked() {
	c.mu.Lock()
	cancel, done := c.cancel, c.done
	c.cancel, c.done, c.running = nil, nil, false
	c.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
}

func (c *Controller) Status() web.TelegramStatus {
	status := c.bot.Status()
	c.mu.Lock()
	status.Running = c.running
	c.mu.Unlock()
	return status
}

func (c *Controller) Token() string {
	return c.bot.Token()
}

func (c *Controller) PairCode() (string, error) {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.bot.PairCode()
}

func (c *Controller) Disconnect() error {
	c.opMu.Lock()
	defer c.opMu.Unlock()
	return c.bot.Disconnect()
}

func (c *Controller) Test(ctx context.Context) error {
	return c.bot.Test(ctx)
}

func (c *Controller) NotifyJobFailure(feedID string, jobErr error) {
	c.bot.NotifyJobFailure(feedID, jobErr)
}

func (c *Controller) ReconcileFeed(ctx context.Context, feedID string, updateFailed bool) {
	c.bot.ReconcileFeed(ctx, feedID, updateFailed)
}

// CheckToken validates a bot token with getMe. Errors never include the token
// or the request URL because Telegram embeds the token in its API path.
func (c *Controller) CheckToken(ctx context.Context, token, proxy string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("telegram token is required")
	}
	client, err := NewHTTPClient(proxy, 10*time.Second)
	if err != nil {
		return errors.New("telegram proxy configuration is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/bot"+token+"/getMe", nil)
	if err != nil {
		return errors.New("telegram token check failed: invalid token")
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("telegram token check failed: request timed out")
		}
		return errors.New("telegram token check failed: Telegram API is unreachable")
	}
	defer res.Body.Close()
	limited, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return errors.New("telegram token check failed: could not read the response")
	}
	var body response
	if err := json.Unmarshal(limited, &body); err != nil {
		return errors.New("telegram token check failed: invalid response from Telegram")
	}
	if !body.OK {
		description := strings.TrimSpace(body.Description)
		if description == "" {
			description = "token rejected"
		}
		description = strings.ReplaceAll(description, token, "token")
		return fmt.Errorf("telegram token check failed: %s", description)
	}
	var info struct {
		ID    int64 `json:"id"`
		IsBot bool  `json:"is_bot"`
	}
	if len(body.Result) > 0 {
		if err := json.Unmarshal(body.Result, &info); err == nil && (!info.IsBot || info.ID == 0) {
			return errors.New("telegram token check failed: response is not a bot account")
		}
	}
	return nil
}
