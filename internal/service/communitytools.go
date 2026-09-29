package service

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/gentleman-programming/gentle-ai/v4/internal/components/communitytool"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
	"github.com/gentleman-programming/gentle-ai/v4/internal/statecoord"
)

// CommunityToolStatuses reports every offered community tool's status.
func CommunityToolStatuses(status func(model.CommunityToolID) communitytool.Status) []communitytool.Status {
	definitions := communitytool.Definitions()
	statuses := make([]communitytool.Status, 0, len(definitions))
	for _, definition := range definitions {
		statuses = append(statuses, status(definition.ID))
	}
	return statuses
}

// InstallCommunityTools installs tools in order and stops at the first
// failure. The failing tool's result is kept when it carries context (the
// commands it ran or the manual actions it left).
func InstallCommunityTools(tools []model.CommunityToolID, install func(model.CommunityToolID) (communitytool.Result, error)) ([]communitytool.Result, error) {
	results := make([]communitytool.Result, 0, len(tools))
	for _, tool := range tools {
		result, err := install(tool)
		if err != nil {
			if result.Tool != "" || len(result.CommandsRun) > 0 || len(result.ManualActions) > 0 {
				results = append(results, result)
			}
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// RecordCommunityTools adds tools to the persisted community tool selection,
// the record sync uses to keep their guidance current.
func RecordCommunityTools(homeDir string, tools []model.CommunityToolID) error {
	return statecoord.WithLock(homeDir, func() error {
		current, err := state.Read(homeDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read install state: %w", err)
		}
		for _, tool := range tools {
			if !slices.Contains(current.CommunityTools, string(tool)) {
				current.CommunityTools = append(current.CommunityTools, string(tool))
			}
		}
		current.CommunityToolsConfigured = true
		return state.Write(homeDir, current)
	})
}
