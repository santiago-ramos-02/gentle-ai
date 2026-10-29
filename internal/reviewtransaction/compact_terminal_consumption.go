package reviewtransaction

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const compactTerminalConsumptionSchema = "gentle-ai.review-terminal-consumption/v1"

// This tombstone is only a deduplication fact. It contains neither authority,
// a receipt usable by delivery gates, nor an acknowledgement replay token.
// It is prepared under the burn locks and becomes effective only once the
// authority directory is absent. A failed burn therefore remains replayable.
type compactTerminalConsumption struct {
	Schema     string `json:"schema"`
	Repository string `json:"repository"`
	Target     string `json:"target"`
	Lineage    string `json:"lineage"`
}

func compactTerminalRepository(root string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(root)))
}

func compactTerminalConsumptionPath(base, root, target string) string {
	key := sha256.Sum256([]byte(compactTerminalRepository(root) + "\n" + target))
	return filepath.Join(base, "terminal-consumption", "v1", fmt.Sprintf("%x.json", key))
}

func prepareCompactTerminalConsumption(base, root string, record CompactRecord) error {
	fact := compactTerminalConsumption{
		Schema: compactTerminalConsumptionSchema, Repository: compactTerminalRepository(root),
		Target: record.State.CurrentSnapshot.Identity, Lineage: record.State.LineageID,
	}
	payload, err := json.Marshal(fact)
	if err != nil {
		return err
	}
	return writeAtomic(compactTerminalConsumptionPath(base, root, fact.Target), payload, 0o644)
}

// CompactTargetConsumed reads terminal evidence for this exact worktree and
// target. It never loads, restores, or grants authority. Callers must prefer
// any live authority before suppressing a fresh offer on this evidence.
func CompactTargetConsumed(ctx context.Context, repo, target string) (bool, error) {
	defer StartStatusPhase(ctx, StatusPhasePredecessorConsumptionAncestry)()
	if !validSHA256(target) {
		return false, errors.New("terminal consumption requires a canonical target identity") // refusal:by-design world-action: the caller must supply the canonical snapshot identity; malformed identities cannot identify consumed targets
	}
	base, root, err := reviewAuthorityRoot(ctx, repo)
	if err != nil {
		return false, err
	}
	path := compactTerminalConsumptionPath(base, root, target)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return false, errors.New("invalid terminal consumption evidence") // refusal:by-design human-authority: a maintainer must inspect unsafe or oversized terminal evidence; automatic repair could erase consumption history
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var fact compactTerminalConsumption
	if json.Unmarshal(payload, &fact) != nil || fact.Schema != compactTerminalConsumptionSchema ||
		fact.Repository != compactTerminalRepository(root) || fact.Target != target || validateLineageID(fact.Lineage) != nil {
		return false, errors.New("terminal consumption evidence does not bind this repository and target") // refusal:by-design human-authority: a maintainer must inspect malformed or foreign terminal evidence; it cannot safely suppress or authorize review
	}
	_, err = os.Lstat(filepath.Join(base, "v2", fact.Lineage))
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

// compactConsumedTargets reads every terminal fact for this exact worktree in
// one directory pass, so a search over many candidate identities costs no Git
// process per lookup. It returns only targets CompactTargetConsumed would
// report consumed; evidence it cannot attribute to this worktree, including
// another worktree's facts and unreadable files, is skipped, which can only
// keep a review offer, never suppress one.
func compactConsumedTargets(ctx context.Context, repo string) (map[string]bool, error) {
	base, root, err := reviewAuthorityRoot(ctx, repo)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(base, "terminal-consumption", "v1"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	repository := compactTerminalRepository(root)
	consumed := map[string]bool{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(base, "terminal-consumption", "v1", entry.Name())
		info, err := entry.Info()
		if err != nil || info.Size() > 4096 {
			continue
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var fact compactTerminalConsumption
		if json.Unmarshal(payload, &fact) != nil || fact.Schema != compactTerminalConsumptionSchema || fact.Repository != repository ||
			!validSHA256(fact.Target) || validateLineageID(fact.Lineage) != nil || path != compactTerminalConsumptionPath(base, root, fact.Target) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(base, "v2", fact.Lineage)); errors.Is(err, os.ErrNotExist) {
			consumed[fact.Target] = true
		}
	}
	return consumed, nil
}
