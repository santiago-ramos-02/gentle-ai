package service

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v3/internal/catalog"
	componentuninstall "github.com/gentleman-programming/gentle-ai/v3/internal/components/uninstall"
	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
)

// UninstallTargets returns what an uninstall mode removes. The full modes
// cover every agent and every uninstallable component; partial removes the
// chosen ones.
func UninstallTargets(mode model.UninstallMode, agents []model.AgentID, components []model.ComponentID) ([]model.AgentID, []model.ComponentID) {
	if mode == model.UninstallModePartial {
		return agents, components
	}
	allAgents := catalog.AllAgents()
	agents = make([]model.AgentID, 0, len(allAgents))
	for _, agent := range allAgents {
		agents = append(agents, agent.ID)
	}
	allComponents := catalog.MVPComponents()
	components = make([]model.ComponentID, 0, len(allComponents))
	for _, component := range allComponents {
		components = append(components, component.ID)
	}
	return agents, components
}

// ProjectEngramDataAvailable reports whether removing Engram can be limited
// to the project in cwd: Engram must be selected and cwd must hold .engram data.
func ProjectEngramDataAvailable(components []model.ComponentID, cwd string, stat func(string) (os.FileInfo, error)) bool {
	if !slices.Contains(components, model.ComponentEngram) || strings.TrimSpace(cwd) == "" {
		return false
	}
	info, err := stat(filepath.Join(cwd, ".engram"))
	return err == nil && info.IsDir()
}

// UninstallFollowUp carries what an uninstall mode does after the managed
// uninstall itself succeeded.
type UninstallFollowUp struct {
	BinaryRemoved bool
	// Synced is true when the clean-install sync ran; SyncErr is its failure,
	// which does not fail the uninstall.
	Synced    bool
	SyncFiles []string
	SyncErr   error
}

// UninstallEffects are the process-level effects the follow-up may need.
type UninstallEffects struct {
	Executable func() (string, error)
	Remove     func(string) error
	Sync       func() ([]string, error)
}

// FinishUninstall applies mode's follow-up: full-remove deletes the running
// gentle-ai binary (Homebrew installs get the brew command instead), and
// clean-install re-syncs every managed asset. Manual actions are appended to
// result.
func FinishUninstall(mode model.UninstallMode, result *componentuninstall.Result, effects UninstallEffects) (UninstallFollowUp, error) {
	var followUp UninstallFollowUp
	switch mode {
	case model.UninstallModeFullRemove:
		execPath, err := effects.Executable()
		if err != nil {
			return followUp, fmt.Errorf("uninstall succeeded but failed to locate binary: %w", err)
		}
		if IsHomebrewManagedBinary(execPath) {
			result.ManualActions = append(result.ManualActions,
				"Homebrew-managed install detected. Run 'brew uninstall gentle-ai' to remove the executable cleanly.")
			return followUp, nil
		}
		if err := effects.Remove(execPath); err != nil {
			return followUp, fmt.Errorf("uninstall succeeded but failed to remove binary at %q: %w", execPath, err)
		}
		followUp.BinaryRemoved = true
	case model.UninstallModeCleanInstall:
		followUp.Synced = true
		if effects.Sync == nil {
			followUp.SyncErr = fmt.Errorf("sync function not configured")
			return followUp, nil
		}
		followUp.SyncFiles, followUp.SyncErr = effects.Sync()
	}
	return followUp, nil
}

// IsHomebrewManagedBinary reports whether execPath lives in a Homebrew prefix,
// where deleting the binary would corrupt the formula's installation.
func IsHomebrewManagedBinary(execPath string) bool {
	path := filepath.ToSlash(filepath.Clean(execPath))
	if strings.Contains(path, "/Cellar/") {
		return true
	}
	return strings.HasPrefix(path, "/opt/homebrew/") ||
		strings.HasPrefix(path, "/usr/local/Homebrew/") ||
		strings.HasPrefix(path, "/home/linuxbrew/.linuxbrew/")
}
