package reviewtransaction

import (
	"context"
	"strings"
	"testing"
)

// acknowledgedPredecessorFixture commits one reviewable change on top of the
// snapshot repository's base commit and records that committed base-diff
// candidate as approved and burned, exactly as acknowledgement leaves it: a
// terminal consumption fact keyed by the candidate identity, with no live
// authority directory.
func acknowledgedPredecessorFixture(t *testing.T) (repo, baseCommit string) {
	t.Helper()
	requireSnapshotGit(t)
	repo = initSnapshotRepo(t)
	baseCommit = strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	writeSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() {}\n")
	gitSnapshot(t, repo, "add", "feature.go")
	gitSnapshot(t, repo, "commit", "-qm", "feature")
	acknowledged := buildCommittedRange(t, repo, baseCommit)
	authorityBase, root, err := reviewAuthorityRoot(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	record := CompactRecord{State: CompactState{LineageID: "review-0123456789abcdef", CurrentSnapshot: acknowledged}}
	if err := prepareCompactTerminalConsumption(authorityBase, root, record); err != nil {
		t.Fatal(err)
	}
	if consumed, err := CompactTargetConsumed(context.Background(), repo, acknowledged.Identity); err != nil || !consumed {
		t.Fatalf("fixture candidate consumed = %v, %v", consumed, err)
	}
	return repo, baseCommit
}

func buildCommittedRange(t *testing.T, repo, baseCommit string) Snapshot {
	t.Helper()
	snapshot, err := (SnapshotBuilder{Repo: repo}).BuildStoredSnapshot(context.Background(), Target{Kind: TargetBaseDiff, BaseRef: baseCommit})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func commitSnapshotFile(t *testing.T, repo, name, content string) {
	t.Helper()
	writeSnapshotFile(t, repo, name, content)
	gitSnapshot(t, repo, "add", name)
	gitSnapshot(t, repo, "commit", "-qm", "update "+name)
}

func requirePassivePredecessor(t *testing.T, repo string, live Snapshot, want bool) {
	t.Helper()
	got, err := AcknowledgedPassivePredecessor(context.Background(), repo, live)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("AcknowledgedPassivePredecessor = %v, want %v (paths %v)", got, want, live.Paths)
	}
}

func TestAcknowledgedPassivePredecessorSuppressesPassiveDelta(t *testing.T) {
	repo, baseCommit := acknowledgedPredecessorFixture(t)
	commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n\n- PR opened.\n")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), true)
	// Several passive commits still trace back to the same acknowledgement.
	commitSnapshotFile(t, repo, "docs/feature.md", "# Feature\n\nUsage notes.\n")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), true)
}

func TestAcknowledgedPassivePredecessorOffersReviewForNonPassiveDelta(t *testing.T) {
	for name, file := range map[string]struct{ path, content string }{
		"code":          {"feature.go", "package feature\n\nfunc Run() { panic(1) }\n"},
		"test":          {"feature_test.go", "package feature\n\nimport \"testing\"\n\nfunc TestRun(t *testing.T) { Run() }\n"},
		"configuration": {"package.json", "{\"name\": \"feature\"}\n"},
	} {
		t.Run(name, func(t *testing.T) {
			repo, baseCommit := acknowledgedPredecessorFixture(t)
			commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n")
			commitSnapshotFile(t, repo, file.path, file.content)
			requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), false)
		})
	}
}

func TestAcknowledgedPassivePredecessorRequiresAnAcknowledgedAncestor(t *testing.T) {
	requireSnapshotGit(t)
	repo := initSnapshotRepo(t)
	baseCommit := strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	commitSnapshotFile(t, repo, "feature.go", "package feature\n")
	commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), false)
}

func TestAcknowledgedPassivePredecessorIgnoresAnotherBase(t *testing.T) {
	repo, baseCommit := acknowledgedPredecessorFixture(t)
	featureCommit := strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n")
	// The acknowledged identity is bound to its own base tree: the same
	// passive commit measured from another base has no acknowledged ancestor.
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, featureCommit), false)
	// A rewritten history drops the acknowledged tree from the ancestry.
	gitSnapshot(t, repo, "reset", "-q", "--hard", baseCommit)
	commitSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() { _ = 1 }\n")
	commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), false)
}

func TestAcknowledgedPassivePredecessorIgnoresOtherTargetKinds(t *testing.T) {
	repo, _ := acknowledgedPredecessorFixture(t)
	commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n")
	writeSnapshotFile(t, repo, "tracked.txt", "dirty\n")
	live, err := (SnapshotBuilder{Repo: repo}).BuildStoredSnapshot(context.Background(), Target{Kind: TargetCurrentChanges, IntendedUntracked: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	requirePassivePredecessor(t, repo, live, false)
}

// acknowledgeWorkspaceCandidate records the uncommitted workspace candidate as
// approved and burned, exactly as acknowledgement leaves it, and returns it.
func acknowledgeWorkspaceCandidate(t *testing.T, repo string, intendedUntracked ...string) Snapshot {
	t.Helper()
	acknowledged, err := (SnapshotBuilder{Repo: repo}).BuildStoredSnapshot(context.Background(), Target{Kind: TargetCurrentChanges, IntendedUntracked: append([]string{}, intendedUntracked...)})
	if err != nil {
		t.Fatal(err)
	}
	authorityBase, root, err := reviewAuthorityRoot(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	record := CompactRecord{State: CompactState{LineageID: "review-fedcba9876543210", CurrentSnapshot: acknowledged}}
	if err := prepareCompactTerminalConsumption(authorityBase, root, record); err != nil {
		t.Fatal(err)
	}
	return acknowledged
}

func TestAcknowledgedPassivePredecessorCoversCommittedWorkspaceCandidate(t *testing.T) {
	requireSnapshotGit(t)
	repo := initSnapshotRepo(t)
	baseCommit := strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	writeSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() {}\n")
	acknowledged := acknowledgeWorkspaceCandidate(t, repo, "feature.go")
	gitSnapshot(t, repo, "add", "feature.go")
	gitSnapshot(t, repo, "commit", "-qm", "feature")
	live := buildCommittedRange(t, repo, baseCommit)
	if live.BaseTree != acknowledged.BaseTree || live.CandidateTree != acknowledged.CandidateTree || live.Identity == acknowledged.Identity {
		t.Fatalf("fixture must commit exactly the reviewed trees under a new identity: live %+v, acknowledged %+v", live, acknowledged)
	}
	requirePassivePredecessor(t, repo, live, true)
	// A passive commit after the reviewed one still traces back to it.
	commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n\n- PR opened.\n")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), true)
}

func TestAcknowledgedPassivePredecessorOffersReviewWhenCommittedBytesDiffer(t *testing.T) {
	requireSnapshotGit(t)
	repo := initSnapshotRepo(t)
	baseCommit := strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	writeSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() {}\n")
	acknowledgeWorkspaceCandidate(t, repo, "feature.go")
	// One byte changes between the approved review and the commit.
	writeSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() {}\n\n")
	gitSnapshot(t, repo, "add", "feature.go")
	gitSnapshot(t, repo, "commit", "-qm", "feature")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), false)
}

func TestAcknowledgedPassivePredecessorCoversPassiveBaseBeforeReviewedWork(t *testing.T) {
	requireSnapshotGit(t)
	repo := initSnapshotRepo(t)
	baseCommit := strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	// The feature's task document lands first, then the reviewed work on top.
	commitSnapshotFile(t, repo, "odd/tasks/feature.md", "# Feature\n\n## Specs\n")
	writeSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() {}\n")
	acknowledgeWorkspaceCandidate(t, repo, "feature.go")
	gitSnapshot(t, repo, "add", "feature.go")
	gitSnapshot(t, repo, "commit", "-qm", "feature")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), true)
}

func TestAcknowledgedPassivePredecessorOffersReviewForUnreviewedCodeBeforeReviewedWork(t *testing.T) {
	requireSnapshotGit(t)
	repo := initSnapshotRepo(t)
	baseCommit := strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	commitSnapshotFile(t, repo, "helper.go", "package feature\n\nfunc helper() {}\n")
	writeSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() { helper() }\n")
	acknowledgeWorkspaceCandidate(t, repo, "feature.go")
	gitSnapshot(t, repo, "add", "feature.go")
	gitSnapshot(t, repo, "commit", "-qm", "feature")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), false)
}

// acknowledgeCommittedRange records fromCommit..HEAD as approved and burned,
// the way an acknowledged `review assess --base-ref <boundary> --committed-only`
// candidate leaves it.
func acknowledgeCommittedRange(t *testing.T, repo, fromCommit, lineage string) {
	t.Helper()
	acknowledged := buildCommittedRange(t, repo, fromCommit)
	authorityBase, root, err := reviewAuthorityRoot(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	record := CompactRecord{State: CompactState{LineageID: lineage, CurrentSnapshot: acknowledged}}
	if err := prepareCompactTerminalConsumption(authorityBase, root, record); err != nil {
		t.Fatal(err)
	}
}

func headCommit(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(gitSnapshot(t, repo, "rev-parse", "HEAD"))
}

// chainedReviewedWork commits first, then two code commits reviewed as two
// consecutive committed ranges, and returns the branch's base commit.
func chainedReviewedWork(t *testing.T, firstName, firstContent string) (repo, baseCommit string) {
	t.Helper()
	requireSnapshotGit(t)
	repo = initSnapshotRepo(t)
	baseCommit = headCommit(t, repo)
	commitSnapshotFile(t, repo, firstName, firstContent)
	firstCommit := headCommit(t, repo)
	commitSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() {}\n")
	featureCommit := headCommit(t, repo)
	acknowledgeCommittedRange(t, repo, firstCommit, "review-1111111111111111")
	commitSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() { _ = 1 }\n")
	acknowledgeCommittedRange(t, repo, featureCommit, "review-2222222222222222")
	return repo, baseCommit
}

func TestAcknowledgedPassivePredecessorCoversPassiveBaseBeforeChainedReviewedWork(t *testing.T) {
	repo, baseCommit := chainedReviewedWork(t, "odd/tasks/feature.md", "# Feature\n\n## Specs\n")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), true)
}

func TestAcknowledgedPassivePredecessorOffersReviewForUnreviewedCodeBeforeChainedReviewedWork(t *testing.T) {
	repo, baseCommit := chainedReviewedWork(t, "helper.go", "package feature\n\nfunc helper() {}\n")
	requirePassivePredecessor(t, repo, buildCommittedRange(t, repo, baseCommit), false)
}

func TestAcknowledgedBoundaryReachesHeadWhenChainedReviewsCoverTheRange(t *testing.T) {
	repo, baseCommit := chainedReviewedWork(t, "helper.go", "package feature\n\nfunc helper() {}\n")
	// Review base..first too: base..first, first..feature and feature..HEAD
	// cover the whole branch without one range spanning it.
	gitSnapshot(t, repo, "checkout", "-q", "HEAD~2")
	acknowledgeCommittedRange(t, repo, baseCommit, "review-3333333333333333")
	gitSnapshot(t, repo, "checkout", "-q", "-")
	boundary, ok, err := AcknowledgedBoundaryCommit(context.Background(), repo, buildCommittedRange(t, repo, baseCommit))
	if err != nil || !ok {
		t.Fatalf("AcknowledgedBoundaryCommit ok = %v, err = %v", ok, err)
	}
	if len(boundary.DeltaPaths) != 0 || !boundary.Passive {
		t.Fatalf("boundary = %+v, want it to reach HEAD with no delta", boundary)
	}
}

func TestAcknowledgedBoundaryReachesHeadWhenReviewedWorkFollowsAReviewedBase(t *testing.T) {
	repo, baseCommit := acknowledgedPredecessorFixture(t)
	featureCommit := headCommit(t, repo)
	commitSnapshotFile(t, repo, "feature.go", "package feature\n\nfunc Run() { _ = 1 }\n")
	acknowledgeCommittedRange(t, repo, featureCommit, "review-4444444444444444")
	boundary, ok, err := AcknowledgedBoundaryCommit(context.Background(), repo, buildCommittedRange(t, repo, baseCommit))
	if err != nil || !ok {
		t.Fatalf("AcknowledgedBoundaryCommit ok = %v, err = %v", ok, err)
	}
	if len(boundary.DeltaPaths) != 0 || !boundary.Passive {
		t.Fatalf("boundary = %+v, want it to reach HEAD with no delta", boundary)
	}
}
