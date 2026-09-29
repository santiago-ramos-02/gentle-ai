package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
)

// Claude Code pointed at a proxy offers the proxy's models to every phase, and a
// phase can be set to one of them.
func TestClaudeModelsIncludeTheProxysModels(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-fable-5-dd-los-6.5-tpg","display_name":"GPT 5.6 Sol"},{"id":"claude-opus-5-5"},{"id":"claude-fable-5-dd-los-6.5-tpg"}]}`))
	}))
	defer server.Close()

	deps := testDeps(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	deps.ClaudeModels = discoverClaudeProxyModels

	plain := result[modelsGetResult](t, deps, "models.get", `{"agent":"claude-code","discover":true}`)
	if len(plain.Options.Claude.Models) != 4 {
		t.Fatalf("without a proxy, Claude offers only its tiers: %+v", plain.Options.Claude.Models)
	}

	settings := `{"env":{"ANTHROPIC_BASE_URL":"` + server.URL + `","ANTHROPIC_AUTH_TOKEN":"proxy-key"}}`
	if err := writeFile(filepath.Join(deps.HomeDir, ".claude", "settings.json"), settings); err != nil {
		t.Fatal(err)
	}
	proxied := result[modelsGetResult](t, deps, "models.get", `{"agent":"claude-code","discover":true}`)
	ids := []string{}
	for _, option := range proxied.Options.Claude.Models {
		ids = append(ids, option.ID)
	}
	if !slices.Equal(ids, []string{"fable", "opus", "sonnet", "haiku", "custom:claude-opus-5-5", "custom:claude-fable-5-dd-los-6.5-tpg"}) {
		t.Fatalf("proxied Claude models = %v", ids)
	}
	// The proxy's own name labels a model it gives a Claude-shaped ID.
	if last := proxied.Options.Claude.Models[5]; last.Label != "GPT 5.6 Sol" || len(last.Efforts) != 6 {
		t.Fatalf("custom option = %+v", last)
	}
	if authorization != "Bearer proxy-key" {
		t.Fatalf("the proxy was asked with %q, want Claude Code's token", authorization)
	}

	params := modelsParams{ClaudePhaseAssignments: map[string]claudeAssignment{
		"odd-worker": {Model: "custom:gpt-5.6-sol", Effort: "high"},
	}}
	overrides, err := params.toOverrides()
	if err != nil || overrides.ClaudePhaseAssignments["odd-worker"].Model.ModelID() != "gpt-5.6-sol" {
		t.Fatalf("custom assignment = %+v, %v", overrides, err)
	}
}
