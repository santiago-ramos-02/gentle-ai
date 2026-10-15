package reviewtransaction

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The predecessor walk reads every pair's paths from one batched diff, so the
// batch must agree with changedPaths pair by pair, including empty pairs,
// deletions, and paths that need quoting.
func TestChangedPathsBatchMatchesChangedPaths(t *testing.T) {
	requireSnapshotGit(t)
	repo := initSnapshotRepo(t)
	builder := SnapshotBuilder{Repo: repo}
	tree := func() string {
		t.Helper()
		resolved, err := builder.resolveTree(context.Background(), "HEAD")
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}
	trees := []string{tree()}
	commitSnapshotFile(t, repo, "src/a.go", "package src\n")
	trees = append(trees, tree())
	commitSnapshotFile(t, repo, "docs/with space.md", "# Notes\n")
	commitSnapshotFile(t, repo, "docs/ñandú.md", "# Unicode\n")
	trees = append(trees, tree())
	gitSnapshot(t, repo, "rm", "-q", "src/a.go")
	gitSnapshot(t, repo, "commit", "-qm", "remove a")
	trees = append(trees, tree())

	pairs := [][2]string{}
	for _, from := range trees {
		for _, to := range trees {
			pairs = append(pairs, [2]string{from, to})
		}
	}
	batched, err := builder.changedPathsBatch(context.Background(), pairs)
	if err != nil {
		t.Fatal(err)
	}
	if len(batched) != len(pairs) {
		t.Fatalf("batched %d pairs, want %d", len(batched), len(pairs))
	}
	for index, pair := range pairs {
		want, err := builder.changedPaths(context.Background(), pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if len(want) == 0 && len(batched[index]) == 0 {
			continue
		}
		if !reflect.DeepEqual(batched[index], want) {
			t.Fatalf("pair %d paths = %q, want %q", index, batched[index], want)
		}
	}
}

// compactConsumedTargets must report exactly what CompactTargetConsumed
// reports for this worktree: another worktree's facts, facts whose authority
// is still live, and unreadable files never count as consumed.
func TestCompactConsumedTargetsMatchesExactLookup(t *testing.T) {
	repo, baseCommit := acknowledgedPredecessorFixture(t)
	acknowledged := buildCommittedRange(t, repo, baseCommit).Identity
	authorityBase, root, err := reviewAuthorityRoot(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	foreign := "sha256:" + strings.Repeat("1", 64)
	foreignRecord := CompactRecord{State: CompactState{LineageID: "review-1111111111111111", CurrentSnapshot: Snapshot{Identity: foreign}}}
	if err := prepareCompactTerminalConsumption(authorityBase, filepath.Join(root, "other-worktree"), foreignRecord); err != nil {
		t.Fatal(err)
	}
	live := "sha256:" + strings.Repeat("2", 64)
	liveRecord := CompactRecord{State: CompactState{LineageID: "review-2222222222222222", CurrentSnapshot: Snapshot{Identity: live}}}
	if err := prepareCompactTerminalConsumption(authorityBase, root, liveRecord); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(authorityBase, "v2", liveRecord.State.LineageID), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authorityBase, "terminal-consumption", "v1", "malformed.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	consumed, err := compactConsumedTargets(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(consumed, map[string]bool{acknowledged: true}) {
		t.Fatalf("consumed targets = %v, want only %s", consumed, acknowledged)
	}
	for _, target := range []string{foreign, live} {
		if exact, err := CompactTargetConsumed(context.Background(), repo, target); err != nil || exact {
			t.Fatalf("exact lookup for %s = %v, %v", target, exact, err)
		}
	}
}

// A worktree without terminal facts has nothing to cover, so the lookup must
// not walk the ancestry at all.
func TestAcknowledgedPassivePredecessorWithoutFactsOffersReview(t *testing.T) {
	requireSnapshotGit(t)
	repo := initSnapshotRepo(t)
	baseCommit := strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), false)
}
