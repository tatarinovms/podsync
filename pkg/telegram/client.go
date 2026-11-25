package telegram

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// ValidateProxy checks an optional Telegram Bot API proxy URL. Supported
// schemes are http, https, socks5, and socks5h; an empty value means a direct
// connection.
func ValidateProxy(raw string) error {
	_, err := parseProxy(raw)
	return err
}

func parseProxy(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid Telegram proxy URL")
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("Telegram proxy must use http, https, socks5, or socks5h")
	}
	if parsed.Hostname() == "" {
		return nil, errors.New("Telegram proxy host is required")
	}
	return parsed, nil
}

// NewHTTPClient builds a client for the Telegram Bot API with an optional
// proxy. Errors intentionally omit the proxy URL so credentials cannot leak
// into API responses or logs.
func NewHTTPClient(rawProxy string, timeout time.Duration) (*http.Client, error) {
	parsed, err := parseProxy(rawProxy)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if parsed != nil {
		switch parsed.Scheme {
		case "http", "https":
			transport.Proxy = http.ProxyURL(parsed)
		case "socks5", "socks5h":
			dialer, err := xproxy.FromURL(parsed, xproxy.Direct)
			if err != nil {
				return nil, errors.New("invalid Telegram SOCKS5 proxy")
			}
			contextDialer, ok := dialer.(xproxy.ContextDialer)
			if !ok {
				return nil, errors.New("invalid Telegram SOCKS5 proxy")
			}
			transport.Proxy = nil
			transport.DialContext = contextDialer.DialContext
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}
