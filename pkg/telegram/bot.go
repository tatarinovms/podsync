// Package telegram implements the optional single-owner Podsync bot.
package telegram

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/personal"
	"github.com/mxpv/podsync/services/web"
	log "github.com/sirupsen/logrus"
)

type Config struct {
	Enabled bool   `toml:"enabled"`
	Token   string `toml:"token"`
}

type notice struct {
	Text string `json:"text"`
}

type state struct {
	ChatID   int64             `json:"chat_id"`
	UserID   int64             `json:"user_id"`
	Offset   int64             `json:"offset"`
	Errors   map[string]string `json:"errors"`
	Awaiting map[string]bool   `json:"awaiting"`
	Outbox   []notice          `json:"outbox"`
}

type draft struct {
	URL, FeedID, Format, Nonce string
	Height                     int
}

type Bot struct {
	mu       sync.Mutex
	cfg      Config
	path     string
	client   *http.Client
	baseURL  string
	mgr      web.AdminManager
	personal *personal.Service
	state    state
	code     string
	codeTill time.Time
	draft    draft
}

func New(cfg Config, path string, mgr web.AdminManager, personalFeeds *personal.Service) (*Bot, error) {
	b := &Bot{cfg: cfg, path: path, client: &http.Client{Timeout: 40 * time.Second}, baseURL: "https://api.telegram.org", mgr: mgr, personal: personalFeeds}
	if cfg.Enabled && strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("telegram token is required when enabled")
	}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &b.state); err != nil {
			return nil, fmt.Errorf("read telegram state: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if b.state.Errors == nil {
		b.state.Errors = map[string]string{}
	}
	if b.state.Awaiting == nil {
		b.state.Awaiting = map[string]bool{}
	}
	return b, nil
}

func (b *Bot) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(&b.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(b.path), ".telegram-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), b.path)
}

func (b *Bot) queueLocked(text string) {
	b.state.Outbox = append(b.state.Outbox, notice{Text: text})
	if len(b.state.Outbox) > 200 {
		b.state.Outbox = b.state.Outbox[len(b.state.Outbox)-200:]
	}
}

func (b *Bot) Status() web.TelegramStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return web.TelegramStatus{Enabled: b.cfg.Enabled, Connected: b.state.ChatID != 0, ChatID: b.state.ChatID}
}

func (b *Bot) PairCode() (string, error) {
	if !b.cfg.Enabled {
		return "", errors.New("telegram bot is disabled")
	}
	var random [5]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	code := fmt.Sprintf("%x", random)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.ChatID != 0 {
		return "", errors.New("disconnect the current owner before pairing another")
	}
	b.code, b.codeTill = code, time.Now().Add(10*time.Minute)
	return b.code, nil
}

func (b *Bot) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	previous, previousCode, previousTill, previousDraft := b.state, b.code, b.codeTill, b.draft
	b.state.ChatID, b.state.UserID = 0, 0
	b.state.Outbox = nil
	b.state.Errors = map[string]string{}
	b.state.Awaiting = map[string]bool{}
	b.code = ""
	b.draft = draft{}
	if err := b.saveLocked(); err != nil {
		b.state, b.code, b.codeTill, b.draft = previous, previousCode, previousTill, previousDraft
		return err
	}
	return nil
}

func (b *Bot) Test(ctx context.Context) error {
	b.mu.Lock()
	chat := b.state.ChatID
	b.mu.Unlock()
	if chat == 0 {
		return errors.New("telegram owner is not connected")
	}
	return b.send(ctx, chat, "Podsync Telegram connection is working.", nil)
}

type update struct {
	ID       int64     `json:"update_id"`
	Message  *message  `json:"message"`
	Callback *callback `json:"callback_query"`
}
type message struct {
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Text string `json:"text"`
}
type callback struct {
	ID   string `json:"id"`
	From struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Message *message `json:"message"`
	Data    string   `json:"data"`
}
type response struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (b *Bot) api(ctx context.Context, method string, payload any, result any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/bot"+b.cfg.Token+"/"+method, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s request failed", method)
	}
	defer res.Body.Close()
	limited, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	var body response
	if err := json.Unmarshal(limited, &body); err != nil {
		return err
	}
	if !body.OK {
		return fmt.Errorf("telegram %s: %s", method, body.Description)
	}
	if result != nil {
		return json.Unmarshal(body.Result, result)
	}
	return nil
}

func (b *Bot) send(ctx context.Context, chat int64, text string, buttons [][]map[string]string) error {
	if len(text) > 4000 {
		text = text[:4000]
	}
	payload := map[string]any{"chat_id": chat, "text": text}
	if len(buttons) > 0 {
		payload["reply_markup"] = map[string]any{"inline_keyboard": buttons}
	}
	return b.api(ctx, "sendMessage", payload, nil)
}
func button(label, data string) map[string]string {
	return map[string]string{"text": label, "callback_data": data}
}

func (b *Bot) Run(ctx context.Context) error {
	if !b.cfg.Enabled {
		return nil
	}
	if err := b.api(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil); err != nil {
		log.WithError(err).Warn("telegram webhook cleanup failed")
	}
	for ctx.Err() == nil {
		b.flush(ctx)
		b.mu.Lock()
		offset := b.state.Offset
		b.mu.Unlock()
		var updates []update
		err := b.api(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message", "callback_query"}}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.WithError(err).Warn("telegram polling failed")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			b.handle(ctx, u)
			b.mu.Lock()
			if u.ID >= b.state.Offset {
				b.state.Offset = u.ID + 1
				if err := b.saveLocked(); err != nil {
					log.WithError(err).Error("save telegram offset")
				}
			}
			b.mu.Unlock()
		}
	}
	return nil
}

func (b *Bot) handle(ctx context.Context, u update) {
	var chat, user int64
	var text, action string
	private := false
	if u.Message != nil {
		chat, user, text, private = u.Message.Chat.ID, u.Message.From.ID, strings.TrimSpace(u.Message.Text), u.Message.Chat.Type == "private"
	}
	if u.Callback != nil && u.Callback.Message != nil {
		chat, user, action, private = u.Callback.Message.Chat.ID, u.Callback.From.ID, u.Callback.Data, u.Callback.Message.Chat.Type == "private"
		_ = b.api(ctx, "answerCallbackQuery", map[string]string{"callback_query_id": u.Callback.ID}, nil)
	}
	if !private || chat == 0 || user == 0 {
		return
	}
	b.mu.Lock()
	owner := b.state.ChatID == chat && b.state.UserID == user
	newPair := false
	if !owner && b.state.ChatID == 0 && strings.HasPrefix(text, "/start ") && time.Now().Before(b.codeTill) && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(text, "/start ")), []byte(b.code)) == 1 {
		b.state.ChatID, b.state.UserID = chat, user
		if err := b.saveLocked(); err == nil {
			b.code = ""
			owner = true
		} else {
			b.state.ChatID, b.state.UserID = 0, 0
			log.WithError(err).Error("save telegram owner")
		}
		newPair = owner
	}
	b.mu.Unlock()
	if !owner {
		return
	}
	if newPair {
		b.seedErrors(ctx, chat)
	}
	if action != "" {
		b.handleAction(ctx, chat, action)
		return
	}
	switch {
	case text == "/start" || strings.HasPrefix(text, "/start ") || text == "/help":
		_ = b.send(ctx, chat, "Send a media URL to add it to a Personal Feed. Commands: /status, /errors, /retry <feed_id> <episode_id>, /cancel.", nil)
	case text == "/status":
		status, err := b.mgr.GetSyncStatus(ctx)
		if err != nil {
			_ = b.send(ctx, chat, "Status unavailable: "+err.Error(), nil)
			return
		}
		_ = b.send(ctx, chat, fmt.Sprintf("Podsync: %d feeds, %d downloaded, %d failed episodes. Downloaded in 24h: %d.", status.ActiveFeeds, status.DownloadedEpisodes, status.FailedEpisodes, status.DownloadedPast24h), nil)
	case text == "/errors":
		items, err := b.mgr.GetDownloadErrors(ctx, "", 5)
		if err != nil {
			_ = b.send(ctx, chat, "Errors unavailable: "+err.Error(), nil)
			return
		}
		var lines []string
		for _, item := range items {
			lines = append(lines, fmt.Sprintf("%s / %s (%s %s): %s", item.FeedTitle, item.EpisodeTitle, item.FeedID, item.EpisodeID, item.Error))
		}
		if feeds, err := b.personal.List(); err == nil {
			for _, f := range feeds {
				if len(lines) >= 5 {
					break
				}
				if detail, err := b.personal.Get(f.ID); err == nil {
					for _, ep := range detail.Episodes {
						if ep.Status == model.EpisodeError {
							lines = append(lines, fmt.Sprintf("%s / %s (%s %s): %s", f.Title, ep.Title, f.ID, ep.ID, ep.Error))
						}
						if len(lines) >= 5 {
							break
						}
					}
				}
			}
		}
		if len(lines) == 0 {
			_ = b.send(ctx, chat, "No current download errors.", nil)
			return
		}
		_ = b.send(ctx, chat, strings.Join(lines, "\n"), nil)
	case strings.HasPrefix(text, "/retry "):
		parts := strings.Fields(text)
		if len(parts) != 3 {
			_ = b.send(ctx, chat, "Usage: /retry <feed_id> <episode_id>", nil)
			return
		}
		var err error
		if _, getErr := b.personal.Get(parts[1]); getErr == nil {
			err = b.personal.Retry(ctx, parts[1], parts[2])
		} else {
			err = b.mgr.RetryEpisode(ctx, parts[1], parts[2])
		}
		if err != nil {
			_ = b.send(ctx, chat, "Retry failed: "+err.Error(), nil)
		} else {
			_ = b.send(ctx, chat, "Retry queued.", nil)
		}
	case text == "/cancel":
		b.mu.Lock()
		b.draft = draft{}
		b.mu.Unlock()
		_ = b.send(ctx, chat, "Cancelled.", nil)
	default:
		u, err := url.Parse(text)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			_ = b.send(ctx, chat, "Send an HTTP or HTTPS media URL, or /help.", nil)
			return
		}
		var random [4]byte
		if _, err := rand.Read(random[:]); err != nil {
			return
		}
		nonce := fmt.Sprintf("%x", random)
		b.mu.Lock()
		b.draft = draft{URL: text, Nonce: nonce}
		b.mu.Unlock()
		b.sendFeeds(ctx, chat, 0, nonce)
	}
}

func (b *Bot) sendFeeds(ctx context.Context, chat int64, page int, nonce string) {
	feeds, err := b.personal.List()
	if err != nil || len(feeds) == 0 {
		_ = b.send(ctx, chat, "Personal Feeds are unavailable.", nil)
		return
	}
	const pageSize = 10
	if page < 0 || page*pageSize >= len(feeds) {
		return
	}
	var rows [][]map[string]string
	for i := page * pageSize; i < len(feeds) && i < (page+1)*pageSize; i++ {
		rows = append(rows, []map[string]string{button(feeds[i].Title, "feed:"+feeds[i].ID+":"+nonce)})
	}
	var navigation []map[string]string
	if page > 0 {
		navigation = append(navigation, button("Previous", fmt.Sprintf("page:%d:%s", page-1, nonce)))
	}
	if (page+1)*pageSize < len(feeds) {
		navigation = append(navigation, button("Next", fmt.Sprintf("page:%d:%s", page+1, nonce)))
	}
	if len(navigation) > 0 {
		rows = append(rows, navigation)
	}
	_ = b.send(ctx, chat, "Choose a Personal Feed:", rows)
}

// NotifyJobFailure records a Personal Feed update failure, which is not part
// of the regular feed error list. ReconcileFeed clears it after a good update.
func (b *Bot) NotifyJobFailure(feedID string, jobErr error) {
	if !b.cfg.Enabled || jobErr == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.ChatID == 0 {
		return
	}
	key := feedID + "/feed_update"
	message := jobErr.Error()
	if b.state.Errors[key] == message {
		return
	}
	b.state.Errors[key] = message
	b.queueLocked("Podsync update failed in " + feedID + ": " + message)
	if err := b.saveLocked(); err != nil {
		log.WithError(err).Error("save telegram update failure")
	}
}

func (b *Bot) seedErrors(ctx context.Context, chat int64) {
	baseline := map[string]string{}
	if b.mgr != nil {
		if items, err := b.mgr.GetDownloadErrors(ctx, "", 1000); err == nil {
			for _, item := range items {
				baseline[item.FeedID+"/"+item.EpisodeID] = item.Error
			}
		}
	}
	if b.personal != nil {
		if feeds, err := b.personal.List(); err == nil {
			for _, f := range feeds {
				if detail, err := b.personal.Get(f.ID); err == nil {
					for _, ep := range detail.Episodes {
						if ep.Status == model.EpisodeError {
							baseline[f.ID+"/"+ep.ID] = ep.Error
						}
					}
				}
			}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.ChatID != chat {
		return
	}
	b.state.Errors = baseline
	if err := b.saveLocked(); err != nil {
		log.WithError(err).Error("save telegram error baseline")
	}
}

func (b *Bot) handleAction(ctx context.Context, chat int64, action string) {
	b.mu.Lock()
	d := b.draft
	b.mu.Unlock()
	if d.URL == "" {
		_ = b.send(ctx, chat, "Send the URL again to start.", nil)
		return
	}
	separator := strings.LastIndexByte(action, ':')
	if separator < 0 || action[separator+1:] != d.Nonce {
		return
	}
	action = action[:separator]
	switch {
	case strings.HasPrefix(action, "page:"):
		page, err := strconv.Atoi(strings.TrimPrefix(action, "page:"))
		if err == nil {
			b.sendFeeds(ctx, chat, page, d.Nonce)
		}
	case strings.HasPrefix(action, "feed:"):
		id := strings.TrimPrefix(action, "feed:")
		if _, err := b.personal.Get(id); err != nil {
			return
		}
		b.mu.Lock()
		b.draft.FeedID = id
		b.mu.Unlock()
		_ = b.send(ctx, chat, "Choose a format:", [][]map[string]string{{button("Audio", "format:audio:"+d.Nonce), button("Video", "format:video:"+d.Nonce)}})
	case action == "format:audio" && d.FeedID != "":
		b.mu.Lock()
		b.draft.Format = "audio"
		b.mu.Unlock()
		_ = b.send(ctx, chat, "Choose audio quality:", [][]map[string]string{{button("High", "quality:high:"+d.Nonce), button("Low", "quality:low:"+d.Nonce)}})
	case action == "format:video" && d.FeedID != "":
		b.mu.Lock()
		b.draft.Format = "video"
		b.mu.Unlock()
		_ = b.send(ctx, chat, "Choose video resolution:", [][]map[string]string{{button("Original", "height:0:"+d.Nonce), button("720p", "height:720:"+d.Nonce), button("1080p", "height:1080:"+d.Nonce)}, {button("480p", "height:480:"+d.Nonce), button("1440p", "height:1440:"+d.Nonce), button("2160p", "height:2160:"+d.Nonce)}})
	case strings.HasPrefix(action, "quality:") && d.Format == "audio":
		quality := strings.TrimPrefix(action, "quality:")
		if quality != "high" && quality != "low" {
			return
		}
		b.add(ctx, chat, d, quality, 0)
	case strings.HasPrefix(action, "height:") && d.Format == "video":
		height, err := strconv.Atoi(strings.TrimPrefix(action, "height:"))
		if err != nil {
			return
		}
		switch height {
		case 0, 480, 720, 1080, 1440, 2160:
			b.add(ctx, chat, d, "high", height)
		}
	}
}

func (b *Bot) add(ctx context.Context, chat int64, d draft, quality string, height int) {
	if d.FeedID == "" {
		return
	}
	ep, err := b.personal.AddURL(ctx, d.FeedID, d.URL, "", d.Format, quality, height)
	if err != nil {
		_ = b.send(ctx, chat, "Could not add URL: "+err.Error(), nil)
		return
	}
	b.mu.Lock()
	b.draft = draft{}
	b.state.Awaiting[d.FeedID+"/"+ep.ID] = true
	if saveErr := b.saveLocked(); saveErr != nil {
		log.WithError(saveErr).Error("save telegram pending episode")
	}
	b.mu.Unlock()
	_ = b.send(ctx, chat, fmt.Sprintf("Queued in %s. Episode ID: %s", d.FeedID, ep.ID), nil)
}

// ReconcileFeed is called after an update job reaches a final result. It queues
// only state changes and never blocks a download on Telegram's availability.
func (b *Bot) ReconcileFeed(ctx context.Context, feedID string, updateFailed bool) {
	if !b.cfg.Enabled {
		return
	}
	current := map[string]string{}
	if detail, err := b.personal.Get(feedID); err == nil {
		for _, ep := range detail.Episodes {
			if ep.Status == model.EpisodeError {
				current[feedID+"/"+ep.ID] = ep.Error
			}
			if ep.Status == model.EpisodeDownloaded {
				b.mu.Lock()
				if b.state.Awaiting[feedID+"/"+ep.ID] {
					b.queueLocked("Download completed: " + ep.Title)
					delete(b.state.Awaiting, feedID+"/"+ep.ID)
					_ = b.saveLocked()
				}
				b.mu.Unlock()
			}
		}
	} else {
		items, err := b.mgr.GetDownloadErrors(ctx, feedID, 1000)
		if err != nil {
			return
		}
		for _, item := range items {
			current[feedID+"/"+item.EpisodeID] = item.Error
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.ChatID == 0 {
		return
	}
	prefix := feedID + "/"
	for key, value := range current {
		if b.state.Errors[key] != value {
			b.queueLocked(fmt.Sprintf("Podsync error in %s: %s (%s)", feedID, value, key))
		}
		b.state.Errors[key] = value
	}
	for key := range b.state.Errors {
		if strings.HasPrefix(key, prefix) {
			if key == feedID+"/feed_update" && updateFailed {
				continue
			}
			if _, ok := current[key]; !ok {
				b.queueLocked("Podsync recovered: " + key)
				delete(b.state.Errors, key)
			}
		}
	}
	if err := b.saveLocked(); err != nil {
		log.WithError(err).Error("save telegram notifications")
	}
}

func (b *Bot) flush(ctx context.Context) {
	for {
		b.mu.Lock()
		if b.state.ChatID == 0 || len(b.state.Outbox) == 0 {
			b.mu.Unlock()
			return
		}
		chat, item := b.state.ChatID, b.state.Outbox[0]
		b.mu.Unlock()
		if err := b.send(ctx, chat, item.Text, nil); err != nil {
			log.WithError(err).Warn("send telegram notification")
			return
		}
		b.mu.Lock()
		if b.state.ChatID == chat && len(b.state.Outbox) > 0 && b.state.Outbox[0].Text == item.Text {
			b.state.Outbox = b.state.Outbox[1:]
			if err := b.saveLocked(); err != nil {
				log.WithError(err).Error("save telegram outbox")
			}
		}
		b.mu.Unlock()
	}
}
