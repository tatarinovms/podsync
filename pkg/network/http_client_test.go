package network

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDNSServers(t *testing.T) {
	require.NoError(t, ValidateDNSServers([]string{"1.1.1.1", "2606:4700:4700::1111"}))
	require.Error(t, ValidateDNSServers([]string{"resolver.example"}))
}

func TestResolverUsesConfiguredServersInOrder(t *testing.T) {
	var addresses []string
	resolver := newResolver([]string{"1.1.1.1", "9.9.9.9"}, func(_ context.Context, _, address string) (net.Conn, error) {
		addresses = append(addresses, address)
		return nil, errors.New("dial stopped by test")
	})

	_, _ = resolver.Dial(context.Background(), "udp", "ignored:53")
	_, _ = resolver.Dial(context.Background(), "udp", "ignored:53")
	require.Equal(t, []string{"1.1.1.1:53", "9.9.9.9:53"}, addresses)
}

func TestNewHTTPClient(t *testing.T) {
	client, err := NewHTTPClient([]string{"1.1.1.1"})
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, client.Transport)

	_, err = NewHTTPClient([]string{"not-an-address"})
	require.Error(t, err)
}
