package builder

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseURL_TwitchUnsupported(t *testing.T) {
	_, err := ParseURL("https://www.twitch.tv/example")
	require.EqualError(t, err, "Twitch is no longer supported")
}
