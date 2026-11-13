package feed

import "strings"

// CleanToken normalizes a provider credential pasted into the Web UI, MCP or
// config file. VK tokens are frequently copied as a full oauth URL; extract
// just the access_token value so the same input works everywhere.
func CleanToken(provider, token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "\"'")
	if !strings.EqualFold(provider, "vkvideo") && !strings.EqualFold(provider, "vk") && !strings.Contains(token, "access_token=") {
		return token
	}
	idx := strings.Index(token, "access_token=")
	if idx == -1 {
		return token
	}
	value := token[idx+len("access_token="):]
	if end := strings.IndexAny(value, " &;\n\r#"); end != -1 {
		return value[:end]
	}
	return value
}
