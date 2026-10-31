package web

import (
	"encoding/json"
	"fmt"

	"github.com/mxpv/podsync/pkg/feed"
)

// Keep field presence through REST and the MCP HTTP proxy. An omitted field
// must not be confused with an explicit false, zero, or empty string.
func (u *FeedConfigUpdate) UnmarshalJSON(data []byte) error {
	type plain FeedConfigUpdate
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &value.fields); err != nil {
		return err
	}
	if value.fields == nil {
		return fmt.Errorf("settings must be a JSON object")
	}
	*u = FeedConfigUpdate(value)
	return nil
}

func (u FeedConfigUpdate) MarshalJSON() ([]byte, error) {
	if u.fields != nil {
		return json.Marshal(u.fields)
	}
	type plain FeedConfigUpdate
	return json.Marshal(plain(u))
}

// WithCurrent resolves a JSON patch into complete settings. Go callers can
// still supply complete settings directly, without JSON field tracking.
func (u *FeedConfigUpdate) WithCurrent(f *feed.Config) (*FeedConfigUpdate, error) {
	if u.fields == nil {
		copy := *u
		return &copy, nil
	}
	type plain FeedConfigUpdate
	ignoreLive := f.IgnoreLive
	normalizeAudio := f.NormalizeAudio
	merged := plain{
		PageSize: f.PageSize, CronSchedule: f.CronSchedule,
		Quality: string(f.Quality), Format: string(f.Format), MaxHeight: f.MaxHeight,
		PlaylistSort: string(f.PlaylistSort), OPML: f.OPML, PrivateFeed: f.PrivateFeed,
		IgnoreLive: &ignoreLive, NormalizeAudio: &normalizeAudio,
		Filters: f.Filters, Custom: f.Custom, YouTubeDLArgs: f.YouTubeDLArgs,
		FilenameTemplate: f.FilenameTemplate,
	}
	if f.Proxy != "" {
		proxyCopy := f.Proxy
		merged.Proxy = &proxyCopy
	}
	if f.Clean != nil {
		merged.KeepLast = f.Clean.KeepLast
	}
	// Filters are a complete group; {} explicitly removes all filters.
	if _, present := u.fields["filters"]; present {
		merged.Filters = feed.Filters{}
	}
	data, err := json.Marshal(u.fields)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &merged); err != nil {
		return nil, err
	}
	if language, present := u.fields["language"]; present {
		if err := json.Unmarshal(language, &merged.Custom.Language); err != nil {
			return nil, err
		}
	}
	result := FeedConfigUpdate(merged)
	return &result, nil
}
