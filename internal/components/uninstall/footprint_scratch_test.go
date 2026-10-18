package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// isolateTempDir points os.TempDir at a fresh directory, where scratch homes
// land, and returns it.
func isolateTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	if os.TempDir() != dir {
		t.Fatalf("os.TempDir() = %q, want %q", os.TempDir(), dir)
	}
	return dir
}

func footprintScratchHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	files := map[string]string{
		"CLAUDE.md":                       "My own rules.\n\n<!-- gentle-ai:orchestrator -->\nODD orchestrator\n<!-- /gentle-ai:orchestrator -->\n",
		"skills/judgment-day/SKILL.md":    "gentle skill",
		"projects/session/transcript.txt": "history",
	}
	for rel, content := range files {
		path := filepath.Join(home, ".claude", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func scratchHomesIn(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, scratchHomePattern))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestAgentFootprintRemovesItsScratchHomeAfterSuccess(t *testing.T) {
	tmp := isolateTempDir(t)
	home := footprintScratchHome(t)

	footprint, err := AgentFootprint(home, []model.AgentID{model.AgentClaudeCode})
	if err != nil {
		t.Fatalf("AgentFootprint() error = %v", err)
	}
	if !slices.Contains(footprint.Removed, filepath.Join(home, ".claude", "skills", "judgment-day")) {
		t.Fatalf("the run should still report gentle-ai's skill: %v", footprint.Removed)
	}
	if left := scratchHomesIn(t, tmp); len(left) > 0 {
		t.Fatalf("scratch homes left after success: %v", left)
	}
}

func TestAgentFootprintRemovesItsScratchHomeAfterFailure(t *testing.T) {
	tmp := isolateTempDir(t)
	home := footprintScratchHome(t)
	failure := errors.New("disk full")
	original := copyIntoScratch
	t.Cleanup(func() { copyIntoScratch = original })
	copyIntoScratch = func(source, target string) error {
		if err := original(source, target); err != nil {
			return err
		}
		return failure
	}

	if _, err := AgentFootprint(home, []model.AgentID{model.AgentClaudeCode}); !errors.Is(err, failure) {
		t.Fatalf("AgentFootprint() error = %v, want %v", err, failure)
	}
	if left := scratchHomesIn(t, tmp); len(left) > 0 {
		t.Fatalf("scratch homes left after failure: %v", left)
	}
}

// A killed run cannot remove its scratch home, so the next run sweeps it.
func TestAgentFootprintSweepsStaleScratchHomes(t *testing.T) {
	tmp := isolateTempDir(t)
	home := footprintScratchHome(t)
	stale := filepath.Join(tmp, "gentle-ai-footprint-killed")
	fresh := filepath.Join(tmp, "gentle-ai-footprint-running")
	unrelated := filepath.Join(tmp, "someone-else")
	for _, dir := range []string{stale, fresh, unrelated} {
		if err := os.MkdirAll(filepath.Join(dir, "AppData"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, dir := range []string{stale, unrelated} {
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := AgentFootprint(home, []model.AgentID{model.AgentClaudeCode}); err != nil {
		t.Fatalf("AgentFootprint() error = %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a scratch home older than an hour should be swept: %v", err)
	}
	for _, dir := range []string{fresh, unrelated} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s must stay: %v", dir, err)
		}
	}
}

// The agent's config directory holds its transcripts, gigabytes on a real
// machine; a footprint copies only the files the uninstall touches.
func TestAgentFootprintDoesNotCopyTheWholeConfigDirectory(t *testing.T) {
	isolateTempDir(t)
	home := footprintScratchHome(t)
	claude := filepath.Join(home, ".claude")
	var copied []string
	original := copyIntoScratch
	t.Cleanup(func() { copyIntoScratch = original })
	copyIntoScratch = func(source, target string) error {
		copied = append(copied, source)
		return original(source, target)
	}

	if _, err := AgentFootprint(home, []model.AgentID{model.AgentClaudeCode}); err != nil {
		t.Fatalf("AgentFootprint() error = %v", err)
	}
	for _, source := range copied {
		if source == claude || source == home {
			t.Fatalf("copied %s whole; copied: %v", source, copied)
		}
		if rel, ok := relativeTo(claude, source); ok && filepath.ToSlash(rel) == "projects" {
			t.Fatalf("copied the transcripts; copied: %v", copied)
		}
	}
}
