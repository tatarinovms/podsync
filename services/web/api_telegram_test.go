package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeTelegramControl struct {
	connected     bool
	token         string
	userID        int64
	allowedUserID int64
	proxy         string
	configureErr  error
	clearErr      error
	applied       []TelegramConfigUpdate
}

func (f *fakeTelegramControl) Status() TelegramStatus {
	return TelegramStatus{
		Enabled:         true,
		Connected:       f.connected,
		UserID:          f.userID,
		AllowedUserID:   f.allowedUserID,
		Proxy:           f.proxy,
		TokenConfigured: f.token != "",
		TokenSource:     "config",
		Running:         f.token != "",
	}
}
func (f *fakeTelegramControl) Configure(_ context.Context, update TelegramConfigUpdate) (TelegramStatus, error) {
	if f.configureErr != nil {
		return f.Status(), f.configureErr
	}
	f.applied = append(f.applied, update)
	if update.Token != nil {
		f.token = *update.Token
	}
	if update.AllowedUserID != nil {
		f.allowedUserID = *update.AllowedUserID
	}
	if update.Proxy != nil {
		f.proxy = *update.Proxy
	}
	return f.Status(), nil
}
func (f *fakeTelegramControl) ClearToken(context.Context) (TelegramStatus, error) {
	if f.clearErr != nil {
		return f.Status(), f.clearErr
	}
	f.token = ""
	return f.Status(), nil
}
func (f *fakeTelegramControl) PairCode() (string, error)  { return "code", nil }
func (f *fakeTelegramControl) Disconnect() error          { f.connected = false; return nil }
func (f *fakeTelegramControl) Test(context.Context) error { return nil }

func TestTelegramAdminActions(t *testing.T) {
	s := New(Config{Admin: AdminConfig{Enabled: true, Username: "admin", Password: "secret"}}, nil, nil)
	s.SetTelegram(&fakeTelegramControl{connected: true, token: "token"})
	unauth := httptest.NewRecorder()
	s.Handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/v1/telegram", nil))
	require.Equal(t, http.StatusUnauthorized, unauth.Code)

	get := httptest.NewRecorder()
	s.handleTelegram(get, httptest.NewRequest(http.MethodGet, "/api/v1/telegram", nil))
	require.Equal(t, http.StatusOK, get.Code)
	require.Contains(t, get.Body.String(), `"token_source":"config"`)
	require.Contains(t, get.Body.String(), `"running":true`)
	require.Contains(t, get.Body.String(), `"allowed_user_id":0`)

	for _, tc := range []struct {
		action string
		body   string
	}{
		{"pair", `"code":"code"`},
		{"test", `"Test message sent"`},
		{"disconnect", `"connected":false`},
		{"clear_token", `"token_configured":false`},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram", bytes.NewBufferString(`{"action":"`+tc.action+`"}`))
		w := httptest.NewRecorder()
		s.handleTelegram(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), tc.body)
	}
}

func TestTelegramConfigureAPI(t *testing.T) {
	control := &fakeTelegramControl{}
	s := New(Config{Admin: AdminConfig{Enabled: true, Username: "admin", Password: "secret"}}, nil, nil)
	s.SetTelegram(control)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/telegram", bytes.NewBufferString(`{"enabled":true,"token":"secret-token","allowed_user_id":123456789,"proxy":"socks5://127.0.0.1:1080"}`))
	w := httptest.NewRecorder()
	s.handleTelegram(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, control.applied, 1)
	require.NotNil(t, control.applied[0].Enabled)
	require.True(t, *control.applied[0].Enabled)
	require.NotNil(t, control.applied[0].AllowedUserID)
	require.Equal(t, int64(123456789), *control.applied[0].AllowedUserID)
	require.Contains(t, w.Body.String(), `"allowed_user_id":123456789`)
	require.NotNil(t, control.applied[0].Proxy)
	require.Equal(t, "socks5://127.0.0.1:1080", *control.applied[0].Proxy)
	require.Contains(t, w.Body.String(), `"proxy":"socks5://127.0.0.1:1080"`)
	require.NotContains(t, w.Body.String(), "secret-token")
	require.NotContains(t, w.Body.String(), `"token":`)

	unauth := httptest.NewRecorder()
	s.Handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodPut, "/api/v1/telegram", bytes.NewBufferString(`{"enabled":true}`)))
	require.Equal(t, http.StatusUnauthorized, unauth.Code)

	control.configureErr = errTestTelegram("telegram token check failed: Unauthorized")
	req = httptest.NewRequest(http.MethodPut, "/api/v1/telegram", bytes.NewBufferString(`{"enabled":true}`))
	w = httptest.NewRecorder()
	s.handleTelegram(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "Unauthorized")

	tooLong := `{"token":"` + strings.Repeat("x", 513) + `"}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/telegram", bytes.NewBufferString(tooLong))
	w = httptest.NewRecorder()
	s.handleTelegram(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "too long")

	req = httptest.NewRequest(http.MethodPut, "/api/v1/telegram", bytes.NewBufferString(`{"enabled":`))
	w = httptest.NewRecorder()
	s.handleTelegram(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.NotContains(t, w.Body.String(), "enabled")

	for _, body := range []string{`{"allowed_user_id":1.5}`, `{"allowed_user_id":"@user"}`, `{"allowed_user_id":999999999999999999999999}`} {
		req = httptest.NewRequest(http.MethodPut, "/api/v1/telegram", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		s.handleTelegram(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, body)
		require.NotContains(t, w.Body.String(), "allowed_user_id")
	}
}

type errTestTelegram string

func (e errTestTelegram) Error() string { return string(e) }
