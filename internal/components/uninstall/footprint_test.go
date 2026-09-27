package uninstall

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v3/internal/components/reviewassets"
	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
)

func TestAgentFootprintReportsGentleAiPartsWithoutTouchingTheHome(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	settings := `{
  "outputStyle": "Gentleman",
  "hooks": {
    "UserPromptSubmit": [
      {
        "matcher": "",
        "hooks": [
          {"type": "command", "command": "gentle-ai skill-registry refresh --quiet --no-gitignore --cwd \"${CLAUDE_PROJECT_DIR:-$PWD}\" || true"},
          {"type": "command", "command": "echo mine"}
        ]
      }
    ]
  }
}`
	files := map[string]string{
		"settings.json":                   settings,
		"CLAUDE.md":                       "My own rules.\n\n<!-- gentle-ai:orchestrator -->\nODD orchestrator\n<!-- /gentle-ai:orchestrator -->\n",
		"skills/judgment-day/SKILL.md":    "gentle skill",
		"skills/my-skill/SKILL.md":        "my skill",
		"agents/review-risk.md":           "---\ndescription: gentle reviewer\n---\n",
		"agents/review-readability.md":    "---\ndescription: gentle reviewer the user changed\n---\n",
		"agents/mine.md":                  "---\ndescription: mine\n---\n",
		"projects/session/transcript.txt": "history",
	}
	for rel, content := range files {
		path := filepath.Join(claude, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The installer recorded review-risk as it wrote it; review-readability no
	// longer matches what it recorded, so it counts as the user's.
	hash := func(content string) string {
		sum := sha256.Sum256([]byte(content))
		return hex.EncodeToString(sum[:])
	}
	ledger := `{"version":1,"files":{"review-risk.md":"` + hash(files["agents/review-risk.md"]) +
		`","review-readability.md":"` + hash("as installed") + `"}}`
	if err := os.WriteFile(reviewassets.OwnershipLedgerPath(filepath.Join(claude, "agents")), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}

	footprint, err := AgentFootprint(home, model.AgentClaudeCode)
	if err != nil {
		t.Fatalf("AgentFootprint() error = %v", err)
	}

	removed := func(rel string) bool {
		return slices.Contains(footprint.Removed, filepath.Join(claude, filepath.FromSlash(rel)))
	}
	if !removed("skills/judgment-day") || !removed("agents/review-risk.md") {
		t.Fatalf("gentle-ai's skill and agent should be removed: %v", footprint.Removed)
	}
	for _, rel := range []string{"skills/my-skill", "agents/mine.md", "agents/review-readability.md", "projects/session/transcript.txt"} {
		if removed(rel) {
			t.Fatalf("%s is the user's and must stay: %v", rel, footprint.Removed)
		}
	}
	rewritten := func(rel string) *RewrittenFile {
		for i := range footprint.Rewritten {
			if footprint.Rewritten[i].Path == filepath.Join(claude, rel) {
				return &footprint.Rewritten[i]
			}
		}
		return nil
	}
	if after := rewritten("settings.json"); after == nil || strings.Contains(after.Content, "skill-registry refresh") || !strings.Contains(after.Content, "echo mine") {
		t.Fatalf("settings.json should lose gentle-ai's hook and keep the user's: %+v", footprint.Rewritten)
	}
	if after := rewritten("CLAUDE.md"); after == nil || strings.Contains(after.Content, "ODD orchestrator") || !strings.Contains(after.Content, "My own rules.") {
		t.Fatalf("CLAUDE.md should lose the orchestrator and keep the user's rules: %+v", footprint.Rewritten)
	}

	// Nothing in the real home changed.
	for rel, content := range files {
		got, err := os.ReadFile(filepath.Join(claude, filepath.FromSlash(rel)))
		if err != nil || string(got) != content {
			t.Fatalf("%s changed on disk: %q, %v", rel, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".gentle-ai", "backups")); !os.IsNotExist(err) {
		t.Fatalf("a footprint must not create backups: %v", err)
	}
}
