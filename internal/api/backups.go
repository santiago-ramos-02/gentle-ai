package api

import (
	"context"
	"strings"
	"time"

	"github.com/gentleman-programming/gentle-ai/v3/internal/backup"
)

type backupInfo struct {
	ID               string    `json:"id"`
	CreatedAt        time.Time `json:"createdAt"`
	Source           string    `json:"source"`
	Description      string    `json:"description"`
	FileCount        int       `json:"fileCount"`
	CreatedByVersion string    `json:"createdByVersion,omitempty"`
	Pinned           bool      `json:"pinned"`
}

type backupsResult struct {
	Backups []backupInfo `json:"backups"`
}

func listBackups(_ context.Context, env *env, _ noParams) (any, error) {
	manifests := backup.List(env.deps.HomeDir)
	out := make([]backupInfo, 0, len(manifests))
	for _, manifest := range manifests {
		out = append(out, backupInfo{
			ID:               manifest.ID,
			CreatedAt:        manifest.CreatedAt,
			Source:           string(manifest.Source),
			Description:      manifest.Description,
			FileCount:        manifest.FileCount,
			CreatedByVersion: manifest.CreatedByVersion,
			Pinned:           manifest.Pinned,
		})
	}
	return backupsResult{Backups: out}, nil
}

type backupIDParams struct {
	ID string `json:"id"`
}

func findBackup(homeDir, id string) (backup.Manifest, error) {
	if strings.TrimSpace(id) == "" {
		return backup.Manifest{}, invalidParams("id is required")
	}
	for _, manifest := range backup.List(homeDir) {
		if manifest.ID == id {
			return manifest, nil
		}
	}
	return backup.Manifest{}, errorf(CodeNotFound, "no backup with id %q", id)
}

type restoreResult struct {
	RestoredFiles []string `json:"restoredFiles"`
}

func restoreBackup(_ context.Context, env *env, params backupIDParams) (any, error) {
	manifest, err := findBackup(env.deps.HomeDir, params.ID)
	if err != nil {
		return nil, err
	}
	if err := env.deps.RestoreBackup(manifest); err != nil {
		return nil, errorf(CodeFailed, "restore backup %s: %v", manifest.ID, err)
	}
	restored := []string{}
	for _, entry := range manifest.Entries {
		if entry.Existed {
			restored = append(restored, entry.OriginalPath)
		}
	}
	return restoreResult{RestoredFiles: restored}, nil
}

type empty struct{}

func deleteBackup(_ context.Context, env *env, params backupIDParams) (any, error) {
	manifest, err := findBackup(env.deps.HomeDir, params.ID)
	if err != nil {
		return nil, err
	}
	if err := backup.DeleteBackup(manifest); err != nil {
		return nil, errorf(CodeFailed, "delete backup %s: %v", manifest.ID, err)
	}
	return empty{}, nil
}

type renameBackupParams struct {
	ID          string  `json:"id"`
	Description *string `json:"description"`
}

func renameBackup(_ context.Context, env *env, params renameBackupParams) (any, error) {
	if params.Description == nil {
		return nil, invalidParams("description is required")
	}
	manifest, err := findBackup(env.deps.HomeDir, params.ID)
	if err != nil {
		return nil, err
	}
	if err := backup.RenameBackup(manifest, *params.Description); err != nil {
		return nil, errorf(CodeFailed, "rename backup %s: %v", manifest.ID, err)
	}
	return empty{}, nil
}

type pinBackupParams struct {
	ID     string `json:"id"`
	Pinned *bool  `json:"pinned"`
}

// pinBackup sets the pin state; TogglePin only flips, so it runs only when
// the requested state differs.
func pinBackup(_ context.Context, env *env, params pinBackupParams) (any, error) {
	if params.Pinned == nil {
		return nil, invalidParams("pinned is required")
	}
	manifest, err := findBackup(env.deps.HomeDir, params.ID)
	if err != nil {
		return nil, err
	}
	if manifest.Pinned != *params.Pinned {
		if err := backup.TogglePin(manifest); err != nil {
			return nil, errorf(CodeFailed, "pin backup %s: %v", manifest.ID, err)
		}
	}
	return empty{}, nil
}
