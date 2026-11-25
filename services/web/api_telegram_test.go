package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeTelegramControl struct{ connected bool }

func (f *fakeTelegramControl) Status() TelegramStatus {
	return TelegramStatus{Enabled: true, Connected: f.connected}
}
func (f *fakeTelegramControl) PairCode() (string, error)  { return "code", nil }
func (f *fakeTelegramControl) Disconnect() error          { f.connected = false; return nil }
func (f *fakeTelegramControl) Test(context.Context) error { return nil }

func TestTelegramAdminActions(t *testing.T) {
	s := New(Config{Admin: AdminConfig{Enabled: true, Username: "admin", Password: "secret"}}, nil, nil)
	s.SetTelegram(&fakeTelegramControl{connected: true})
	unauth := httptest.NewRecorder()
	s.Handler.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/api/v1/telegram", nil))
	require.Equal(t, http.StatusUnauthorized, unauth.Code)
	for _, tc := range []struct {
		action string
		body   string
	}{
		{"pair", `"code":"code"`},
		{"test", `"Test message sent"`},
		{"disconnect", `"connected":false`},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram", bytes.NewBufferString(`{"action":"`+tc.action+`"}`))
		w := httptest.NewRecorder()
		s.handleTelegram(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		require.Contains(t, w.Body.String(), tc.body)
	}
}
