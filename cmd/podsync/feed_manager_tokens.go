package main

// Provider API tokens and administrator credential management.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
)

func (m *AppFeedManager) GetTokens(ctx context.Context) []web.TokenInfo {
	m.mu.RLock()
	tokensCopy := make(map[model.Provider]StringSlice, len(m.tokens))
	for k, v := range m.tokens {
		tokensCopy[k] = append([]string(nil), v...)
	}
	m.mu.RUnlock()

	providers := []struct {
		provider model.Provider
		envVars  []string
	}{
		{model.ProviderVkVideo, []string{"PODSYNC_VKVIDEO_API_KEY", "PODSYNC_VK_API_KEY"}},
		{model.ProviderYoutube, []string{"PODSYNC_YOUTUBE_API_KEY"}},
		{model.ProviderVimeo, []string{"PODSYNC_VIMEO_API_KEY"}},
		{model.ProviderSoundcloud, []string{"PODSYNC_SOUNDCLOUD_API_KEY"}},
		{model.ProviderRutube, []string{"PODSYNC_RUTUBE_API_KEY"}},
	}

	result := make([]web.TokenInfo, 0, len(providers))
	for _, p := range providers {
		fromEnv := false
		for _, v := range p.envVars {
			if os.Getenv(v) != "" {
				fromEnv = true
				break
			}
		}

		var rawTokens []string
		if list, ok := tokensCopy[p.provider]; ok {
			rawTokens = list
		}

		maskedTokens := make([]string, 0, len(rawTokens))
		for _, tok := range rawTokens {
			maskedTokens = append(maskedTokens, maskToken(tok))
		}

		result = append(result, web.TokenInfo{
			Provider: string(p.provider),
			Tokens:   maskedTokens,
			FromEnv:  fromEnv,
		})
	}

	return result
}

func maskToken(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return "••••••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// Caller holds configWriteMu across reading, persisting and publishing tokens.

// Caller holds configWriteMu across reading, persisting and publishing tokens.
func (m *AppFeedManager) persistTokens(providerStr string, newTokens []string) error {
	provider := model.Provider(providerStr)
	// Save to config.toml
	if m.configPath != "" {
		tree, err := toml.LoadFile(m.configPath)
		if err == nil {
			key := fmt.Sprintf("tokens.%s", providerStr)
			if len(newTokens) == 0 {
				tree.Delete(key)
			} else if len(newTokens) == 1 {
				tree.Set(key, newTokens[0])
			} else {
				tree.Set(key, newTokens)
			}
			if err := writeConfigTree(m.configPath, tree); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	m.mu.Lock()
	if m.tokens == nil {
		m.tokens = make(map[model.Provider]StringSlice)
	}
	if len(newTokens) > 0 {
		m.tokens[provider] = newTokens
	} else {
		delete(m.tokens, provider)
	}
	m.mu.Unlock()

	return nil
}

func cleanToken(providerStr, tok string) string {
	tok = strings.TrimSpace(tok)
	tok = strings.Trim(tok, "\"'")
	if strings.EqualFold(providerStr, "vkvideo") || strings.EqualFold(providerStr, "vk") || strings.Contains(tok, "access_token=") {
		if idx := strings.Index(tok, "access_token="); idx != -1 {
			sub := tok[idx+len("access_token="):]
			if end := strings.IndexAny(sub, " &;\n\r#"); end != -1 {
				return sub[:end]
			}
			return sub
		}
	}
	return tok
}

func (m *AppFeedManager) UpdateTokens(ctx context.Context, providerStr string, tokens []string) error {
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	var newTokens []string
	for _, tok := range tokens {
		tok = cleanToken(providerStr, tok)
		if tok == "" || strings.Contains(tok, "•") {
			continue
		}
		newTokens = append(newTokens, tok)
	}

	return m.persistTokens(providerStr, newTokens)
}

func (m *AppFeedManager) AddTokens(ctx context.Context, providerStr string, tokens []string) error {
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	var validTokens []string
	for _, tok := range tokens {
		tok = cleanToken(providerStr, tok)
		if tok == "" || strings.Contains(tok, "•") {
			continue
		}
		validTokens = append(validTokens, tok)
	}
	if len(validTokens) == 0 {
		return nil
	}

	provider := model.Provider(providerStr)
	m.mu.RLock()
	current := m.tokens[provider]
	combined := append(append([]string(nil), current...), validTokens...)
	m.mu.RUnlock()

	return m.persistTokens(providerStr, combined)
}

func (m *AppFeedManager) DeleteToken(ctx context.Context, providerStr string, index int) error {
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	if index < 0 {
		return m.persistTokens(providerStr, nil)
	}

	provider := model.Provider(providerStr)
	m.mu.RLock()
	current := m.tokens[provider]
	if index >= len(current) {
		m.mu.RUnlock()
		return fmt.Errorf("token index %d out of bounds (total tokens: %d)", index, len(current))
	}

	var remaining []string
	for i, tok := range current {
		if i != index {
			remaining = append(remaining, tok)
		}
	}
	m.mu.RUnlock()

	return m.persistTokens(providerStr, remaining)
}

func (m *AppFeedManager) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	m.mu.RLock()
	currentPassword := ""
	if m.cfg != nil && m.cfg.Server.Admin.Password != "" {
		currentPassword = m.cfg.Server.Admin.Password
	}
	m.mu.RUnlock()

	if currentPassword != "" && currentPassword != oldPassword {
		return errors.New("current password does not match")
	}

	if strings.TrimSpace(newPassword) == "" {
		return errors.New("new password cannot be empty")
	}
	if len(newPassword) < 4 {
		return errors.New("new password must be at least 4 characters long")
	}

	if m.configPath != "" {
		tree, err := toml.LoadFile(m.configPath)
		if err != nil {
			return errors.Wrap(err, "failed to load config file")
		}
		tree.Set("server.admin.password", newPassword)

		if err := writeConfigTree(m.configPath, tree); err != nil {
			return errors.Wrap(err, "failed to save config file")
		}
	}

	m.mu.Lock()
	if m.cfg != nil {
		m.cfg.Server.Admin.Password = newPassword
	}
	m.mu.Unlock()

	return nil
}

func (m *AppFeedManager) UpdateAdminSecurity(ctx context.Context, maxAttempts int, window, lockout string) error {
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	if m.configPath != "" {
		tree, err := toml.LoadFile(m.configPath)
		if err != nil {
			return errors.Wrap(err, "failed to load config file")
		}
		tree.Set("server.admin.max_attempts", int64(maxAttempts))
		tree.Set("server.admin.window", window)
		tree.Set("server.admin.lockout", lockout)

		if err := writeConfigTree(m.configPath, tree); err != nil {
			return errors.Wrap(err, "failed to save config file")
		}
	}

	m.mu.Lock()
	if m.cfg != nil {
		m.cfg.Server.Admin.MaxAttempts = maxAttempts
		m.cfg.Server.Admin.WindowPeriod = window
		m.cfg.Server.Admin.LockoutPeriod = lockout
	}
	m.mu.Unlock()

	return nil
}
