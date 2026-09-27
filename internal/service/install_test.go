package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
	"github.com/gentleman-programming/gentle-ai/v3/internal/pipeline"
	"github.com/gentleman-programming/gentle-ai/v3/internal/state"
)

func TestInstallPreservesConcurrentCLIStateMutation(t *testing.T) {
	home := t.TempDir()
	candidate := buildCandidateBinary(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	initialRecordedAt := time.Now().UTC().Add(-time.Hour)
	if err := state.Write(home, state.InstallState{
		RDDMode:           "off",
		RDDModeRecordedAt: &initialRecordedAt,
		BackgroundIntent:  model.OpenCodeBackgroundOff,
	}); err != nil {
		t.Fatal(err)
	}

	publicationReached := make(chan struct{})
	releasePublication := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePublication) }) }
	t.Cleanup(release)
	previousWriteReconciled := writeInstallState
	writeInstallState = func(homeDir string, installState state.InstallState) error {
		close(publicationReached)
		<-releasePublication
		return state.WriteReconciled(homeDir, installState)
	}
	t.Cleanup(func() { writeInstallState = previousWriteReconciled })

	selection := model.Selection{
		Agents:     []model.AgentID{model.AgentOpenCode},
		Components: []model.ComponentID{},
		Preset:     model.PresetCustom,
	}
	resultCh := make(chan pipeline.ExecutionResult, 1)
	go func() {
		resultCh <- Install(home, InstallRequest{Selection: selection, OpenCodeBackground: model.OpenCodeBackgroundOn, OpenCodeBackgroundPersist: model.OpenCodeBackgroundOn}, nil)
	}()

	select {
	case <-publicationReached:
	case <-time.After(10 * time.Second):
		t.Fatal("install did not reach final state publication")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, candidate, "review", "mode", "enable", "--scope", "global")
	command.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("candidate review mode enable succeeded while TUI owned the install-state transaction:\n%s", output)
	}
	intermediate, err := state.Read(home)
	if err != nil {
		t.Fatalf("read state after contended candidate review mode enable: %v", err)
	}
	if intermediate.RDDMode != "off" || intermediate.RDDModeRecordedAt == nil || !intermediate.RDDModeRecordedAt.Equal(initialRecordedAt) {
		t.Fatalf("contended candidate review mode enable changed persisted state: %#v", intermediate)
	}

	release()
	select {
	case result := <-resultCh:
		if result.Err != nil {
			t.Fatalf("Install() error = %v", result.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("install did not finish final state publication")
	}

	command = exec.CommandContext(ctx, candidate, "review", "mode", "enable", "--scope", "global")
	command.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	output, err = command.CombinedOutput()
	if err != nil {
		t.Fatalf("candidate review mode enable retry failed: %v\n%s", err, output)
	}

	final, err := state.Read(home)
	if err != nil {
		t.Fatalf("read final install state: %v", err)
	}
	if final.RDDMode != "on" || final.RDDModeRecordedAt == nil || !final.RDDModeRecordedAt.After(initialRecordedAt) || final.BackgroundIntent != model.OpenCodeBackgroundOn || !final.SelectionConfigured || final.Preset != model.PresetCustom || !slices.Equal(final.InstalledAgents, []string{string(model.AgentOpenCode)}) {
		t.Fatalf("final install state lost retried CLI review-mode mutation or TUI-owned fields: %#v", final)
	}
}

func buildCandidateBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "gentle-ai")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	// A cold build of the whole binary on a shared CI runner can exceed
	// 30s; the cap only guards against a hung toolchain, not build speed.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/gentle-ai")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build candidate binary: %v\n%s", err, output)
	}
	return binary
}
