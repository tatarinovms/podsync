package feed

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanToken(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		token    string
		want     string
	}{
		{name: "plain YouTube key", provider: "youtube", token: " AIzaSyTest ", want: "AIzaSyTest"},
		{name: "quoted token", provider: "vimeo", token: `"abc123"`, want: "abc123"},
		{name: "VK oauth URL", provider: "vkvideo", token: "https://oauth.vk.com/blank.html#access_token=vk1.a123&expires_in=0", want: "vk1.a123"},
		{name: "VK token with trailing query", provider: "vk", token: "access_token=secret&user_id=1", want: "secret"},
		{name: "VK URL without token", provider: "vkvideo", token: "https://vk.com/example", want: "https://vk.com/example"},
		{name: "other provider URL is untouched", provider: "youtube", token: "access_token=keep", want: "keep"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, CleanToken(tc.provider, tc.token))
		})
	}
}
