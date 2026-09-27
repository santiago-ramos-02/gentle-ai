package uninstall

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v3/internal/agents"
	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
	"github.com/gentleman-programming/gentle-ai/v3/internal/state"
)

// Footprint is what Gentle AI added to one agent's configuration, as removing
// it from that agent would undo it: the paths it deletes, and the shared files
// it rewrites with their content afterwards. A host can build the agent's
// configuration without Gentle AI from it without changing anything on disk.
type Footprint struct {
	Agent     string          `json:"agent"`
	Removed   []string        `json:"removed"`
	Rewritten []RewrittenFile `json:"rewritten"`
	// Unsimulated lists paths the uninstall would touch that resolve outside
	// the user's home, such as a config directory set by XDG_CONFIG_HOME. They
	// were not simulated, so the footprint is incomplete for them.
	Unsimulated []string `json:"unsimulated"`
}

// RewrittenFile is a shared file with Gentle AI's parts taken out.
type RewrittenFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// AgentFootprint simulates removing every Gentle AI component from one agent.
// The uninstall plan is built against the real home only to learn which paths
// it touches; those paths and the install state are copied into a scratch
// home, where the same plan runs for real. Comparing the scratch copies with
// the originals gives the footprint. Nothing outside the scratch home is
// written: operations whose paths resolve elsewhere are reported as
// unsimulated instead of applied.
func AgentFootprint(homeDir string, agent model.AgentID) (Footprint, error) {
	registry, err := agents.NewDefaultRegistry()
	if err != nil {
		return Footprint{}, fmt.Errorf("create adapter registry: %w", err)
	}
	if _, ok := registry.Get(agent); !ok {
		return Footprint{}, fmt.Errorf("unsupported agent %q", agent)
	}
	components := footprintComponents()
	real := &Service{homeDir: homeDir, workspaceDir: homeDir, registry: registry}
	realPlan, err := real.buildPlan([]model.AgentID{agent}, components)
	if err != nil {
		return Footprint{}, err
	}

	scratch, err := os.MkdirTemp("", "gentle-ai-footprint-*")
	if err != nil {
		return Footprint{}, fmt.Errorf("create scratch home: %w", err)
	}
	defer os.RemoveAll(scratch)

	result := Footprint{Agent: string(agent), Removed: []string{}, Rewritten: []RewrittenFile{}, Unsimulated: []string{}}
	// Operations read files besides their own, such as an ownership ledger;
	// the plan's backup targets name everything it touches or reads.
	copyTargets := []string{state.Path(homeDir)}
	for _, target := range realPlan.backupTargets {
		if _, ok := relativeTo(homeDir, target); ok {
			copyTargets = append(copyTargets, target)
		}
	}
	for _, op := range realPlan.operations {
		if op.typeID == opRemoveIfEmpty {
			continue
		}
		if _, ok := relativeTo(homeDir, op.path); !ok {
			result.Unsimulated = append(result.Unsimulated, op.path)
			continue
		}
		copyTargets = append(copyTargets, op.path)
	}
	for _, target := range copyTargets {
		rel, _ := relativeTo(homeDir, target)
		if err := copyPath(target, filepath.Join(scratch, rel)); err != nil {
			return Footprint{}, fmt.Errorf("copy %q into the scratch home: %w", target, err)
		}
	}

	simulated := &Service{homeDir: scratch, workspaceDir: scratch, registry: registry}
	simPlan, err := simulated.buildPlan([]model.AgentID{agent}, components)
	if err != nil {
		return Footprint{}, err
	}
	for _, op := range simPlan.operations {
		if op.typeID == opRemoveIfEmpty {
			continue
		}
		rel, ok := relativeTo(scratch, op.path)
		if !ok {
			// Resolved outside the scratch home (from the environment), so it
			// would reach the user's real files. Already reported above.
			continue
		}
		if _, _, err := op.apply(op.path); err != nil {
			result.Unsimulated = append(result.Unsimulated, filepath.Join(homeDir, rel))
			continue
		}
		original := filepath.Join(homeDir, rel)
		if _, err := os.Lstat(original); err != nil {
			continue
		}
		after, err := os.ReadFile(op.path)
		switch {
		case os.IsNotExist(err):
			result.Removed = append(result.Removed, original)
		case err != nil:
			// A directory that survived its operation is unchanged as a whole.
			continue
		default:
			before, err := os.ReadFile(original)
			if err == nil && !bytes.Equal(before, after) {
				result.Rewritten = append(result.Rewritten, RewrittenFile{Path: original, Content: string(after)})
			}
		}
	}
	slices.Sort(result.Removed)
	result.Removed = slices.Compact(result.Removed)
	slices.Sort(result.Unsimulated)
	result.Unsimulated = slices.Compact(result.Unsimulated)
	slices.SortFunc(result.Rewritten, func(a, b RewrittenFile) int { return strings.Compare(a.Path, b.Path) })
	return result, nil
}

// footprintComponents is every component an uninstall can remove from an
// agent, including visual polish, but not retired SDD.
func footprintComponents() []model.ComponentID {
	components := expandVisualPolishUninstallComponents(slices.Clone(allManagedComponents))
	for _, component := range fullAgentRemovalComponents {
		if component != model.ComponentSDD && !slices.Contains(components, component) {
			components = append(components, component)
		}
	}
	return components
}

// relativeTo reports path relative to root when path is inside root.
func relativeTo(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// copyPath copies a file or directory tree, following links, and does nothing
// when source does not exist.
func copyPath(source, target string) error {
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(source, target, info.Mode().Perm())
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		info, err := os.Stat(path)
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		return copyFile(path, destination, info.Mode().Perm())
	})
}

func copyFile(source, target string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
