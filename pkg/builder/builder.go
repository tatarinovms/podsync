package builder

import (
	"context"
	"net/http"

	"github.com/pkg/errors"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

type Builder interface {
	Build(ctx context.Context, cfg *feed.Config) (*model.Feed, error)
}

func New(ctx context.Context, provider model.Provider, key string, downloader Downloader) (Builder, error) {
	return NewWithHTTPClient(ctx, provider, key, downloader, nil)
}

// NewWithHTTPClient creates a builder using client for provider API and RSS
// metadata requests. A nil client uses the system resolver.
func NewWithHTTPClient(ctx context.Context, provider model.Provider, key string, downloader Downloader, client *http.Client) (Builder, error) {
	switch provider {
	case model.ProviderYoutube:
		return NewYouTubeBuilderWithHTTPClient(key, downloader, client)
	case model.ProviderVimeo:
		return NewVimeoBuilderWithHTTPClient(ctx, key, client)
	case model.ProviderSoundcloud:
		return NewSoundcloudBuilder()
	case model.ProviderVkVideo:
		return NewVkVideoBuilderWithHTTPClient(key, downloader, client)
	case model.ProviderRutube:
		return NewRutubeBuilderWithHTTPClient(downloader, client)
	case model.ProviderAudiobook:
		return NewAudiobookBuilder("")
	case model.ProviderRSS:
		return NewRSSBuilderWithHTTPClient(client), nil
	default:
		return nil, errors.Errorf("unsupported provider %q", provider)
	}
}
