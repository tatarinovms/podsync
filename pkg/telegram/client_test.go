package telegram

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateTelegramProxy(t *testing.T) {
	for _, proxy := range []string{
		"",
		"http://127.0.0.1:8080",
		"https://user:pass@proxy.example.com:8443",
		"socks5://127.0.0.1:1080",
		"socks5h://user:pass@proxy.example.com:1080",
	} {
		require.NoError(t, ValidateProxy(proxy), proxy)
	}
	for _, proxy := range []string{
		"ftp://proxy.example.com:21",
		"http://",
		"://broken",
		"proxy.example.com:8080",
	} {
		require.Error(t, ValidateProxy(proxy), proxy)
	}
}

func TestNewTelegramHTTPClient(t *testing.T) {
	client, err := NewHTTPClient("http://user:pass@127.0.0.1:8080", 3*time.Second)
	require.NoError(t, err)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.Proxy)
	req, err := http.NewRequest(http.MethodGet, "https://api.telegram.org", nil)
	require.NoError(t, err)
	proxyURL, err := transport.Proxy(req)
	require.NoError(t, err)
	require.Equal(t, "http://user:pass@127.0.0.1:8080", proxyURL.String())

	socks, err := NewHTTPClient("socks5://127.0.0.1:1080", 3*time.Second)
	require.NoError(t, err)
	socksTransport := socks.Transport.(*http.Transport)
	require.Nil(t, socksTransport.Proxy)
	require.NotNil(t, socksTransport.DialContext)

	_, err = NewHTTPClient("ftp://127.0.0.1:21", 3*time.Second)
	require.ErrorContains(t, err, "http, https, socks5")
}

func TestBotSetProxyRejectsInvalidURL(t *testing.T) {
	bot, err := New(Config{Token: "token"}, t.TempDir()+"/state.json", nil, nil)
	require.NoError(t, err)
	before := bot.httpClient()
	require.Error(t, bot.SetProxy("ftp://127.0.0.1:21"))
	require.Same(t, before, bot.httpClient(), "invalid proxy must not replace the client")
	require.NoError(t, bot.SetProxy("socks5://127.0.0.1:1080"))
	require.NotSame(t, before, bot.httpClient())
}
