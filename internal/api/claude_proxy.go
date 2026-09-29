package api

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// claudeProxyModel is one model a proxy offers Claude Code: the ID Claude Code sends, and
// the proxy's name for it. CLIProxyAPI, for one, gives other providers' models
// Claude-shaped IDs for Claude Code and names them in display_name.
type claudeProxyModel struct {
	ID    string
	Label string
}

// discoverClaudeProxyModels lists the models behind the endpoint Claude Code is pointed
// at, when it is pointed at one: ANTHROPIC_BASE_URL in Claude Code's settings.json env
// block, else in the environment, the way a proxy such as CLIProxyAPI is connected.
// Claude Code talking to Anthropic directly has nothing beyond the tiers to discover, and
// an endpoint that cannot be listed adds nothing either.
func discoverClaudeProxyModels(ctx context.Context, homeDir string) []claudeProxyModel {
	baseURL, token := claudeEndpoint(homeDir)
	if baseURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/v1/models", nil)
	if err != nil {
		return nil
	}
	request.Header.Set("anthropic-version", "2023-06-01")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("x-api-key", token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil
	}
	var listing struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&listing); err != nil {
		return nil
	}
	models := make([]claudeProxyModel, 0, len(listing.Data))
	for _, entry := range listing.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" || slices.ContainsFunc(models, func(m claudeProxyModel) bool { return m.ID == id }) {
			continue
		}
		models = append(models, claudeProxyModel{ID: id, Label: cmp.Or(strings.TrimSpace(entry.DisplayName), id)})
	}
	slices.SortFunc(models, func(a, b claudeProxyModel) int {
		return cmp.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
	return models
}

// claudeEndpoint is the base URL and token Claude Code sends requests with. Its own
// settings.json env block wins over the process environment, as in Claude Code.
func claudeEndpoint(homeDir string) (baseURL, token string) {
	configDir := os.Getenv("CLAUDE_CONFIG_DIR")
	if configDir == "" {
		configDir = filepath.Join(homeDir, ".claude")
	}
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if raw, err := os.ReadFile(filepath.Join(configDir, "settings.json")); err == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	lookup := func(name string) string {
		if value := strings.TrimSpace(settings.Env[name]); value != "" {
			return value
		}
		return strings.TrimSpace(os.Getenv(name))
	}
	token = lookup("ANTHROPIC_AUTH_TOKEN")
	if token == "" {
		token = lookup("ANTHROPIC_API_KEY")
	}
	return lookup("ANTHROPIC_BASE_URL"), token
}
