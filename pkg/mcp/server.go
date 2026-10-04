package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
)

// Server implements the Model Context Protocol (MCP) server
type Server struct {
	mgr web.AdminManager
}

// NewServer creates a new MCP server backed by web.AdminManager
func NewServer(mgr web.AdminManager) *Server {
	return &Server{
		mgr: mgr,
	}
}

// HandleMessage parses a JSON-RPC 2.0 message and returns the response bytes
func (s *Server) HandleMessage(ctx context.Context, data []byte) ([]byte, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(data, &req); err != nil {
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			Error: &JSONRPCError{
				Code:    CodeParseError,
				Message: fmt.Sprintf("Parse error: %v", err),
			},
		}
		return json.Marshal(resp)
	}

	// Notifications have no ID and do not return responses
	if req.ID == nil && req.Method == "notifications/initialized" {
		return nil, nil
	}

	resp := s.dispatch(ctx, &req)
	return json.Marshal(resp)
}

func (s *Server) dispatch(ctx context.Context, req *JSONRPCRequest) JSONRPCResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	switch req.Method {
	case "initialize":
		var params InitializeParams
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &params)
		}
		resp.Result = InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: ServerCapabilities{
				Tools:     &ToolsCapability{ListChanged: false},
				Resources: &ResourcesCapability{Subscribe: false, ListChanged: false},
				Prompts:   &PromptsCapability{ListChanged: false},
			},
			ServerInfo: Implementation{
				Name:    "podsync-mcp",
				Version: "v2.8.0",
			},
			Instructions: "Podsync MCP Server provides tools to manage podcast feeds, monitor downloads, retry episodes, inspect system status, and configure API tokens for YouTube, VK Video, Vimeo, SoundCloud, and Twitch.",
		}

	case "ping":
		resp.Result = map[string]interface{}{}

	case "tools/list":
		resp.Result = ListToolsResult{
			Tools: s.getToolDefinitions(),
		}

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Invalid params: %v", err),
			}
			return resp
		}
		result, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			resp.Result = CallToolResult{
				Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Error: %v", err)}},
				IsError: true,
			}
		} else {
			resp.Result = result
		}

	case "resources/list":
		resp.Result = ListResourcesResult{
			Resources: []Resource{
				{
					URI:         "podsync://feeds",
					Name:        "Podcast Feeds",
					Description: "List of all configured podcast feeds, episode counts, and disk usage",
					MimeType:    "application/json",
				},
				{
					URI:         "podsync://system",
					Name:        "System Diagnostics",
					Description: "Podsync runtime health, memory, disk capacity, and downloader tools",
					MimeType:    "application/json",
				},
				{
					URI:         "podsync://tokens",
					Name:        "API Tokens",
					Description: "Status and masked keys for YouTube, VK Video, Vimeo, SoundCloud, Twitch",
					MimeType:    "application/json",
				},
			},
		}

	case "resources/read":
		var params ReadResourceParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Invalid params: %v", err),
			}
			return resp
		}
		result, err := s.readResource(ctx, params.URI)
		if err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInternalError,
				Message: err.Error(),
			}
			return resp
		}
		resp.Result = result

	case "prompts/list":
		resp.Result = ListPromptsResult{
			Prompts: []Prompt{
				{
					Name:        "diagnose_podsync",
					Description: "Diagnose system health, failed episode downloads, and API token readiness",
				},
				{
					Name:        "summarize_library",
					Description: "Generate a detailed executive overview of all podcast channels and disk usage",
				},
			},
		}

	case "prompts/get":
		var params GetPromptParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInvalidParams,
				Message: fmt.Sprintf("Invalid params: %v", err),
			}
			return resp
		}
		result, err := s.getPrompt(ctx, params.Name, params.Arguments)
		if err != nil {
			resp.Error = &JSONRPCError{
				Code:    CodeInternalError,
				Message: err.Error(),
			}
			return resp
		}
		resp.Result = result

	default:
		resp.Error = &JSONRPCError{
			Code:    CodeMethodNotFound,
			Message: fmt.Sprintf("Method %q not supported", req.Method),
		}
	}

	return resp
}

func (s *Server) getToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "list_feeds",
			Description: "List all configured podcast channels/feeds with stats, URLs, and episode counts",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "get_feed",
			Description: "Get detailed information about a specific podcast feed and its episode list",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "Unique ID of the feed (e.g. LABELCOM, XYZ)",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "add_feed",
			Description: "Add a new podcast feed (e.g., YouTube channel/playlist, VK Video group, Vimeo)",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"url": {
						Type:        "string",
						Description: "URL of the channel, group, playlist, or user",
					},
					"format": {
						Type:        "string",
						Description: "Format to download: 'audio' (default) or 'video'",
						Enum:        []string{"audio", "video"},
					},
					"quality": {
						Type:        "string",
						Description: "Media quality: 'high' (default) or 'low'",
						Enum:        []string{"high", "low"},
					},
					"page_size": {
						Type:        "integer",
						Description: "Number of recent episodes to fetch/keep (default: 50)",
					},
					"update_period": {
						Type:        "string",
						Description: "Update interval (e.g., '1h', '6h', '12h', or cron expr)",
					},
				},
				Required: []string{"url"},
			},
		},
		{
			Name:        "delete_feed",
			Description: "Delete a podcast feed from configuration and optionally remove downloaded files from disk",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "ID of the feed to delete",
					},
					"delete_files": {
						Type:        "boolean",
						Description: "Whether to permanently delete downloaded media files from disk",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "update_feed",
			Description: "Trigger an immediate update check and download cycle for a specific feed",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "ID of the feed to update immediately",
					},
				},
				Required: []string{"feed_id"},
			},
		},
		{
			Name:        "retry_episode",
			Description: "Reset error status of a failed podcast episode to trigger a fresh download attempt",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"feed_id": {
						Type:        "string",
						Description: "ID of the feed",
					},
					"episode_id": {
						Type:        "string",
						Description: "ID of the episode to retry",
					},
				},
				Required: []string{"feed_id", "episode_id"},
			},
		},
		{
			Name:        "get_system_stats",
			Description: "Get Podsync instance health, memory usage, disk space (total/used/free), and yt-dlp/ffmpeg presence",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "get_tokens",
			Description: "Get configured API keys (masked) and origin (ENV vs config.toml) for all providers",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "get_downloader_config",
			Description: "Get yt-dlp downloader configuration, cookies status, and proxy settings",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
		{
			Name:        "update_downloader_config",
			Description: "Update yt-dlp downloader options such as timeout, proxy, or cookies_from_browser",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"timeout": {
						Type:        "integer",
						Description: "Download timeout in minutes",
					},
					"proxy": {
						Type:        "string",
						Description: "Proxy URL (e.g. http://proxy:8080 or socks5://127.0.0.1:1080)",
					},
					"cookies_from_browser": {
						Type:        "string",
						Description: "Browser to extract cookies from (e.g. chrome, firefox, safari, brave)",
					},
				},
			},
		},
		{
			Name:        "test_downloader",
			Description: "Test yt-dlp downloader against a video URL to verify cookies and connectivity",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"url": {
						Type:        "string",
						Description: "URL to test (optional, defaults to YouTube sample video)",
					},
				},
			},
		},
		{
			Name:        "update_tokens",
			Description: "Update API tokens for a provider. Supports multiple tokens for automatic round-robin rotation",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"provider": {
						Type:        "string",
						Description: "Provider identifier: 'youtube', 'vkvideo', 'vimeo', 'soundcloud', 'twitch'",
						Enum:        []string{"youtube", "vkvideo", "vimeo", "soundcloud", "twitch"},
					},
					"tokens": {
						Type:        "array",
						Description: "Array of API tokens/keys. For YouTube, multiple keys enable automatic quota rotation",
					},
				},
				Required: []string{"provider", "tokens"},
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]interface{}) (CallToolResult, error) {
	if s.mgr == nil {
		return CallToolResult{}, fmt.Errorf("feed manager not initialized")
	}

	switch name {
	case "list_feeds":
		feeds, err := s.mgr.ListFeeds(ctx)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(feeds, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_feed":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		summary, episodes, err := s.mgr.GetFeedDetail(ctx, feedID)
		if err != nil {
			return CallToolResult{}, err
		}
		res := map[string]interface{}{
			"feed":     summary,
			"episodes": episodes,
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "add_feed":
		urlStr, ok := args["url"].(string)
		if !ok || strings.TrimSpace(urlStr) == "" {
			return CallToolResult{}, fmt.Errorf("url parameter is required")
		}
		cfg := &feed.Config{
			URL:    strings.TrimSpace(urlStr),
			Format: model.FormatAudio,
		}
		if f, ok := args["format"].(string); ok && f != "" {
			cfg.Format = model.Format(f)
		}
		if q, ok := args["quality"].(string); ok && q != "" {
			cfg.Quality = model.Quality(q)
		}
		if ps, ok := args["page_size"].(float64); ok && ps > 0 {
			cfg.PageSize = int(ps)
		}
		if p, ok := args["update_period"].(string); ok && p != "" {
			if d, err := time.ParseDuration(p); err == nil {
				cfg.UpdatePeriod = d
			} else {
				cfg.CronSchedule = p
			}
		}

		if err := s.mgr.AddFeed(ctx, cfg); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully added feed %q (%s)", cfg.ID, cfg.URL)}},
		}, nil

	case "delete_feed":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		deleteFiles := false
		if df, ok := args["delete_files"].(bool); ok {
			deleteFiles = df
		}
		if err := s.mgr.DeleteFeed(ctx, feedID, deleteFiles); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully deleted feed %q (delete files: %v)", feedID, deleteFiles)}},
		}, nil

	case "update_feed":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		if err := s.mgr.TriggerUpdate(ctx, feedID); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Update triggered for feed %q", feedID)}},
		}, nil

	case "retry_episode":
		feedID, ok := args["feed_id"].(string)
		if !ok || strings.TrimSpace(feedID) == "" {
			return CallToolResult{}, fmt.Errorf("feed_id parameter is required")
		}
		epID, ok := args["episode_id"].(string)
		if !ok || strings.TrimSpace(epID) == "" {
			return CallToolResult{}, fmt.Errorf("episode_id parameter is required")
		}
		if err := s.mgr.RetryEpisode(ctx, feedID, epID); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Episode %q reset for retry in feed %q", epID, feedID)}},
		}, nil

	case "get_system_stats":
		stats, err := s.mgr.GetSystemStats(ctx)
		if err != nil {
			return CallToolResult{}, err
		}
		data, _ := json.MarshalIndent(stats, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "get_tokens":
		tokens := s.mgr.GetTokens(ctx)
		data, _ := json.MarshalIndent(tokens, "", "  ")
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: string(data)}},
		}, nil

	case "update_tokens":
		provider, ok := args["provider"].(string)
		if !ok || strings.TrimSpace(provider) == "" {
			return CallToolResult{}, fmt.Errorf("provider parameter is required")
		}
		var tokens []string
		if rawSlice, ok := args["tokens"].([]interface{}); ok {
			for _, item := range rawSlice {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					tokens = append(tokens, strings.TrimSpace(s))
				}
			}
		} else if rawStr, ok := args["tokens"].(string); ok {
			tokens = strings.Fields(rawStr)
		}
		if len(tokens) == 0 {
			return CallToolResult{}, fmt.Errorf("at least one token required")
		}
		if err := s.mgr.UpdateTokens(ctx, provider, tokens); err != nil {
			return CallToolResult{}, err
		}
		return CallToolResult{
			Content: []ContentItem{{Type: "text", Text: fmt.Sprintf("Successfully updated %d token(s) for provider %q", len(tokens), provider)}},
		}, nil

	default:
		return CallToolResult{}, fmt.Errorf("unknown tool %q", name)
	}
}

func (s *Server) readResource(ctx context.Context, uri string) (ReadResourceResult, error) {
	if s.mgr == nil {
		return ReadResourceResult{}, fmt.Errorf("feed manager not initialized")
	}

	switch uri {
	case "podsync://feeds":
		feeds, err := s.mgr.ListFeeds(ctx)
		if err != nil {
			return ReadResourceResult{}, err
		}
		data, _ := json.MarshalIndent(feeds, "", "  ")
		return ReadResourceResult{
			Contents: []ResourceContent{
				{
					URI:      uri,
					MimeType: "application/json",
					Text:     string(data),
				},
			},
		}, nil

	case "podsync://system":
		stats, err := s.mgr.GetSystemStats(ctx)
		if err != nil {
			return ReadResourceResult{}, err
		}
		data, _ := json.MarshalIndent(stats, "", "  ")
		return ReadResourceResult{
			Contents: []ResourceContent{
				{
					URI:      uri,
					MimeType: "application/json",
					Text:     string(data),
				},
			},
		}, nil

	case "podsync://tokens":
		tokens := s.mgr.GetTokens(ctx)
		data, _ := json.MarshalIndent(tokens, "", "  ")
		return ReadResourceResult{
			Contents: []ResourceContent{
				{
					URI:      uri,
					MimeType: "application/json",
					Text:     string(data),
				},
			},
		}, nil

	default:
		return ReadResourceResult{}, fmt.Errorf("unknown resource URI: %s", uri)
	}
}

func (s *Server) getPrompt(ctx context.Context, name string, args map[string]string) (GetPromptResult, error) {
	switch name {
	case "diagnose_podsync":
		stats, _ := s.mgr.GetSystemStats(ctx)
		tokens := s.mgr.GetTokens(ctx)
		feeds, _ := s.mgr.ListFeeds(ctx)

		systemInfo, _ := json.MarshalIndent(stats, "", "  ")
		tokensInfo, _ := json.MarshalIndent(tokens, "", "  ")
		feedsInfo, _ := json.MarshalIndent(feeds, "", "  ")

		text := fmt.Sprintf(`Please analyze the health of this Podsync instance:

### System Metrics:
%s

### API Tokens:
%s

### Configured Feeds:
%s

Please report:
1. Are required dependencies (yt-dlp, ffmpeg) operational?
2. Are any API tokens missing for configured feeds?
3. Are there any feeds with episode download errors?
4. Are disk usage and memory within healthy limits?`, systemInfo, tokensInfo, feedsInfo)

		return GetPromptResult{
			Description: "Diagnose system health and feeds",
			Messages: []PromptMessage{
				{
					Role:    "user",
					Content: ContentItem{Type: "text", Text: text},
				},
			},
		}, nil

	case "summarize_library":
		feeds, _ := s.mgr.ListFeeds(ctx)
		feedsInfo, _ := json.MarshalIndent(feeds, "", "  ")

		text := fmt.Sprintf(`Here is the current list of podcast feeds hosted by Podsync:
%s

Please summarize:
1. Total channels and distribution by provider (YouTube, VK Video, Vimeo, etc.).
2. Total episodes downloaded and total storage used.
3. Recommendations for update frequencies or cleanup policies if applicable.`, feedsInfo)

		return GetPromptResult{
			Description: "Summarize podcast library",
			Messages: []PromptMessage{
				{
					Role:    "user",
					Content: ContentItem{Type: "text", Text: text},
				},
			},
		}, nil

	default:
		return GetPromptResult{}, fmt.Errorf("unknown prompt: %s", name)
	}
}
