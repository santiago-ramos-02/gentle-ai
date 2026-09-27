package backup

import (
	"os"
	"path/filepath"
	"sort"
)

// List returns every readable backup manifest under homeDir, newest first.
// Unreadable entries are skipped so one damaged backup never hides the rest.
func List(homeDir string) []Manifest {
	backupRoot := filepath.Join(homeDir, ".gentle-ai", "backups")
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return nil
	}

	manifests := make([]Manifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := ReadManifest(filepath.Join(backupRoot, entry.Name(), ManifestFilename))
		if err != nil {
			continue
		}
		manifests = append(manifests, manifest)
	}
	sort.SliceStable(manifests, func(i, j int) bool {
		return manifests[i].CreatedAt.After(manifests[j].CreatedAt)
	})
	return manifests
}
