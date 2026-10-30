// Package network provides outbound HTTP clients with an optional DNS resolver.
package network

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"time"
)

// ValidateDNSServers verifies that every configured resolver is an IP address.
func ValidateDNSServers(servers []string) error {
	for _, server := range servers {
		if _, err := netip.ParseAddr(server); err != nil {
			return fmt.Errorf("invalid DNS server %q: use an IPv4 or IPv6 address", server)
		}
	}
	return nil
}

// NewHTTPClient returns an HTTP client that uses servers for hostname lookups.
// An empty list preserves the operating system's resolver configuration.
func NewHTTPClient(servers []string) (*http.Client, error) {
	if err := ValidateDNSServers(servers); err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if len(servers) == 0 {
		return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
	}

	servers = append([]string(nil), servers...)
	transport.DialContext = (&net.Dialer{Resolver: newResolver(servers, (&net.Dialer{}).DialContext)}).DialContext
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

func newResolver(servers []string, dialContext func(context.Context, string, string) (net.Conn, error)) *net.Resolver {
	var next uint64
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			server := servers[(atomic.AddUint64(&next, 1)-1)%uint64(len(servers))]
			return dialContext(ctx, "udp", net.JoinHostPort(server, "53"))
		},
	}
}
