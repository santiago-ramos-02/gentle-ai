package reviewtransaction

import (
	"context"
	"slices"
	"strings"
)

// AcknowledgedPassivePredecessor reports whether a live committed base-diff
// candidate only adds passive content to a candidate that was already
// approved and acknowledged (issue #4739). After an acknowledgement, one more
// commit that touches a tracking note changes the target identity, and the
// full base-diff would otherwise be offered for review again although every
// reviewable byte in it was just approved.
//
// The walk finds the reviewed boundary through exact terminal consumption
// evidence. Only a delta from that boundary that classifies as low risk
// (passive content only) qualifies.
//
// An acknowledged candidate may be a committed base-diff or the uncommitted
// workspace candidate the same trees were before their commit: the compact
// store already treats current-changes and base-diff as content-equivalent
// kinds. Besides the walk from the base, two shapes qualify:
//   - base..HEAD itself was acknowledged as a workspace candidate, so
//     committing exactly the reviewed bytes needs no second review;
//   - ancestor..HEAD was acknowledged, typically the workspace candidate
//     reviewed on top of that ancestor, and base..ancestor is passive, such as
//     the feature's task document committed before the reviewed work. When the
//     boundary advanced commit by commit, consecutive acknowledged ranges below
//     the ancestor extend it down, and only the part below them must be passive.
//
// It runs inside STATUS's aggregate budget: a worktree with no terminal facts
// returns before any walk, and terminal facts are read in one pass instead of
// one lookup per candidate identity.
//
// The result may only suppress a fresh review offer. Terminal consumption is
// a deduplication fact, never authority: callers must not derive a receipt or
// a delivery gate from it, must prefer live authority, and must offer the
// review whenever this returns an error.
func AcknowledgedPassivePredecessor(ctx context.Context, repo string, live Snapshot) (bool, error) {
	boundary, ok, err := acknowledgedBoundaryCommit(ctx, repo, live, false)
	if err != nil || !ok {
		return false, err
	}
	return boundary.Passive, nil
}

// AcknowledgedBoundary reports the latest first-parent ancestor covered by
// contiguous acknowledgements from the candidate's base: the reviewed boundary.
type AcknowledgedBoundary struct {
	// Commit and Tree name the boundary commit and its tree: the base a
	// delta-scoped candidate derives from, and the --base-ref a START for it
	// must disclose.
	Commit string
	Tree   string
	// DeltaPaths is the change set from the boundary tree to the live
	// candidate tree. Empty only when every commit since the boundary was
	// empty: the boundary reaches the live content.
	DeltaPaths []string
	// Passive reports whether that delta can be suppressed as low risk. An
	// exact consumed range is selected instead, even if its content is passive.
	// It is also true when the acknowledgements reach the live content, since
	// nothing in the range is unreviewed.
	Passive bool
}

// AcknowledgedBoundaryCommit finds the newest consumed range from the
// candidate's base tree in HEAD's first-parent ancestry. From that checkpoint,
// it prefers an exact consumed range to the live tree before advancing to
// any consumed intermediate delta. ok is false when the bounded walk finds no
// original checkpoint; Git or tombstone faults keep the pre-existing offer.
//
// The result may only reshape or withhold a fresh review offer. Terminal
// consumption is a deduplication fact, never authority: callers must not
// derive a receipt or a delivery gate from it, must prefer live authority, and
// must keep a live lineage visible.
func AcknowledgedBoundaryCommit(ctx context.Context, repo string, live Snapshot) (AcknowledgedBoundary, bool, error) {
	return acknowledgedBoundaryCommit(ctx, repo, live, true)
}

func acknowledgedBoundaryCommit(ctx context.Context, repo string, live Snapshot, preferExact bool) (AcknowledgedBoundary, bool, error) {
	if live.Kind != TargetBaseDiff || len(live.IntendedUntracked) != 0 || len(live.Paths) == 0 {
		return AcknowledgedBoundary{}, false, nil
	}
	defer StartStatusPhase(ctx, StatusPhasePredecessorConsumptionAncestry)()
	builder := SnapshotBuilder{Repo: repo}
	root, err := builder.repositoryRoot(ctx)
	if err != nil {
		return AcknowledgedBoundary{}, false, err
	}
	builder.Repo = root
	consumed, err := compactConsumedTargets(ctx, root)
	if err != nil || len(consumed) == 0 {
		return AcknowledgedBoundary{}, false, err
	}
	// The ancestry is HEAD's, so the candidate must be exactly HEAD's tree.
	headTree, err := builder.resolveTree(ctx, "HEAD")
	if err != nil || headTree != live.CandidateTree {
		return AcknowledgedBoundary{}, false, err
	}
	// The boundary reaches the live content: nothing in the range is unreviewed.
	reachesHead := func() (AcknowledgedBoundary, bool, error) {
		head, err := runGit(ctx, root, nil, nil, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			return AcknowledgedBoundary{}, false, err
		}
		return AcknowledgedBoundary{Commit: strings.TrimSpace(string(head)), Tree: headTree, Passive: true}, true, nil
	}
	if !preferExact && acknowledgedCandidate(consumed, live.Projection, live.BaseTree, live.CandidateTree, live.Paths) {
		// The whole range was reviewed, typically as the workspace candidate
		// before its commit. Boundary derivation leaves an exact acknowledgement
		// to the caller, which reports the target as already acknowledged.
		return reachesHead()
	}
	ancestors, err := selectorlessCommittedBaseDiffAncestors(ctx, root)
	if err != nil {
		return AcknowledgedBoundary{}, false, err
	}
	if len(ancestors) == 0 {
		return AcknowledgedBoundary{}, false, nil
	}
	// Terminal facts were read once above, so a range lookup costs one batched
	// path diff and no store access. Resolve the Git identity once for the walk
	// and revalidate it before using a matched boundary; a changed repository
	// must not suppress review.
	lease, err := openRepositoryIdentityLeaseAtRoot(ctx, root)
	if err != nil {
		return AcknowledgedBoundary{}, false, err
	}
	confirm := func(boundary AcknowledgedBoundary, ok bool, err error) (AcknowledgedBoundary, bool, error) {
		if err != nil || !ok {
			return AcknowledgedBoundary{}, false, err
		}
		if err := lease.Validate(ctx); err != nil {
			return AcknowledgedBoundary{}, false, err
		}
		return boundary, true, nil
	}
	consumedRange := func(baseTree, candidateTree string) ([]string, bool, error) {
		paths, err := builder.changedPaths(ctx, baseTree, candidateTree)
		if err != nil {
			return nil, false, err
		}
		return paths, acknowledgedCandidate(consumed, live.Projection, baseTree, candidateTree, paths), nil
	}
	// The walk starts at the branch point. Later slices may START from any
	// reached boundary (A..B, A..C, B..E): check every reached boundary newest
	// first and advance only on its first exact acknowledgement.
	// A branch point older than the window still starts the walk by its tree.
	ancestor := selectorlessCommittedBaseDiffAncestor{tree: live.BaseTree}
	start := slices.IndexFunc(ancestors, func(entry selectorlessCommittedBaseDiffAncestor) bool { return entry.tree == live.BaseTree })
	if start < 0 {
		start = len(ancestors)
	} else {
		ancestor = ancestors[start]
	}
	reached := []selectorlessCommittedBaseDiffAncestor{ancestor}
	for next, checked := start-1, live.BaseTree; ; next-- {
		// A legacy acknowledgement may cover HEAD from this boundary,
		// even when a shorter prefix was also consumed. Check once per
		// boundary before advancing, and keep the exact matched base.
		if preferExact && checked != ancestor.tree && ancestor.tree != live.CandidateTree {
			checked = ancestor.tree
			paths, covered, err := consumedRange(ancestor.tree, live.CandidateTree)
			if err != nil {
				return AcknowledgedBoundary{}, false, err
			}
			if covered {
				return confirm(AcknowledgedBoundary{Commit: ancestor.commit, Tree: ancestor.tree, DeltaPaths: paths}, true, nil)
			}
		}
		if next < 0 {
			break
		}
		descendant := ancestors[next]
		if descendant.tree == ancestor.tree {
			continue
		}
		for i := len(reached) - 1; i >= 0; i-- {
			if base := reached[i]; base.tree == descendant.tree {
				continue
			}
			_, covered, err := consumedRange(reached[i].tree, descendant.tree)
			if err != nil {
				return AcknowledgedBoundary{}, false, err
			}
			if covered {
				reached, ancestor = append(reached, descendant), descendant
				break
			}
		}
	}
	if ancestor.tree == live.BaseTree {
		// No acknowledgement starts at the base, but reviewed work may sit on
		// top of a passive prefix of the branch.
		return confirm(builder.acknowledgedWorkAbovePassiveBase(ctx, consumed, live, ancestors[:start], reachesHead))
	}
	deltaPaths, err := builder.changedPaths(ctx, ancestor.tree, live.CandidateTree)
	if err != nil {
		return AcknowledgedBoundary{}, false, err
	}
	if len(deltaPaths) == 0 {
		// Every commit since the boundary was empty: the boundary reaches
		// the live content, so there is no delta to classify.
		return confirm(AcknowledgedBoundary{Commit: ancestor.commit, Tree: ancestor.tree, Passive: true}, true, nil)
	}
	// guard:population acknowledged-passive-delta too-loose: only a committed HEAD base-diff whose first-parent boundary is reached by contiguous, exact consumed identities from the same base tree is reported; the shared risk assessor classifies the remaining delta, and lookup failures keep the offer
	// A delta that was itself reviewed as a range, such as the workspace
	// candidate committed afterwards, leaves nothing unreviewed.
	passive := acknowledgedCandidate(consumed, live.Projection, ancestor.tree, live.CandidateTree, deltaPaths)
	if !passive {
		passive, err = builder.passiveDelta(ctx, live, ancestor.tree, live.CandidateTree, deltaPaths)
		if err != nil {
			return AcknowledgedBoundary{}, false, err
		}
	}
	return confirm(AcknowledgedBoundary{
		Commit: ancestor.commit, Tree: ancestor.tree,
		DeltaPaths: deltaPaths, Passive: passive,
	}, true, nil)
}

// acknowledgedWorkAbovePassiveBase handles a branch with no acknowledgement
// from its base: ancestor..HEAD was acknowledged, typically as the workspace
// candidate reviewed on top of that ancestor, and base..ancestor, the part no
// acknowledgement covers, qualifies only when it is passive. ancestors are the
// first-parent commits above the base, newest first. Its path diffs come from
// one batch, and each further acknowledged range it chains through adds one.
func (b SnapshotBuilder) acknowledgedWorkAbovePassiveBase(ctx context.Context, consumed map[string]bool, live Snapshot, ancestors []selectorlessCommittedBaseDiffAncestor, reachesHead func() (AcknowledgedBoundary, bool, error)) (AcknowledgedBoundary, bool, error) {
	pairs := make([][2]string, 0, 2*len(ancestors))
	for _, ancestor := range ancestors {
		pairs = append(pairs, [2]string{live.BaseTree, ancestor.tree}, [2]string{ancestor.tree, live.CandidateTree})
	}
	paths, err := b.changedPathsBatch(ctx, pairs)
	if err != nil {
		return AcknowledgedBoundary{}, false, err
	}
	for index, ancestor := range ancestors {
		beforePaths, afterPaths := paths[2*index], paths[2*index+1]
		if !acknowledgedCandidate(consumed, live.Projection, ancestor.tree, live.CandidateTree, afterPaths) {
			continue
		}
		// The reviewed work sits on top of the boundary. Reviews that
		// advanced the boundary commit by commit left consecutive reviewed
		// ranges below it, so follow them down: the part no acknowledgement
		// covers is base..bottom. Only a passive one suppresses the offer;
		// otherwise the full range stays offered.
		bottom, uncovered, err := b.acknowledgedChainBottom(ctx, consumed, live, ancestors[index+1:], ancestor.tree, beforePaths)
		if err != nil {
			return AcknowledgedBoundary{}, false, err
		}
		if bottom == live.BaseTree {
			return reachesHead()
		}
		passive, err := b.passiveDelta(ctx, live, live.BaseTree, bottom, uncovered)
		if err != nil || !passive {
			return AcknowledgedBoundary{}, false, err
		}
		return AcknowledgedBoundary{Commit: ancestor.commit, Tree: ancestor.tree, DeltaPaths: afterPaths, Passive: true}, true, nil
	}
	return AcknowledgedBoundary{}, false, nil
}

// acknowledgedChainBottom follows consecutive acknowledged ranges down from
// top, whose base..top paths are topPaths, through the older ancestors below
// it. It returns the lowest tree those ranges reach and the base..bottom paths
// no acknowledgement covers; bottom is the base tree when they cover it all.
func (b SnapshotBuilder) acknowledgedChainBottom(ctx context.Context, consumed map[string]bool, live Snapshot, below []selectorlessCommittedBaseDiffAncestor, top string, topPaths []string) (string, []string, error) {
	for {
		if acknowledgedCandidate(consumed, live.Projection, live.BaseTree, top, topPaths) {
			return live.BaseTree, nil, nil
		}
		pairs := make([][2]string, 0, 2*len(below))
		for _, ancestor := range below {
			pairs = append(pairs, [2]string{ancestor.tree, top}, [2]string{live.BaseTree, ancestor.tree})
		}
		paths, err := b.changedPathsBatch(ctx, pairs)
		if err != nil {
			return "", nil, err
		}
		next := -1
		for index, ancestor := range below {
			if acknowledgedCandidate(consumed, live.Projection, ancestor.tree, top, paths[2*index]) {
				next = index
				break
			}
		}
		if next < 0 {
			return top, topPaths, nil
		}
		top, topPaths, below = below[next].tree, paths[2*next+1], below[next+1:]
	}
}

// acknowledgedCandidate reports whether baseTree..candidateTree, whose changed
// paths are paths, carries terminal consumption evidence as either
// content-equivalent kind.
func acknowledgedCandidate(consumed map[string]bool, projection Projection, baseTree, candidateTree string, paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, kind := range []TargetKind{TargetBaseDiff, TargetCurrentChanges} {
		if consumed[IdentityForComponents(kind, projection, baseTree, candidateTree, digestPaths(paths))] {
			return true
		}
	}
	return false
}

// passiveDelta reports whether fromTree..toTree, the part of the live range
// no acknowledgement covers, adds only passive content.
func (b SnapshotBuilder) passiveDelta(ctx context.Context, live Snapshot, fromTree, toTree string, deltaPaths []string) (bool, error) {
	if len(deltaPaths) == 0 {
		return false, nil
	}
	delta := Snapshot{
		Kind: TargetBaseDiff, Projection: live.Projection,
		BaseTree: fromTree, CandidateTree: toTree,
		GeneratedPathInterpretation: live.GeneratedPathInterpretation,
		Paths:                       deltaPaths, PathsDigest: digestPaths(deltaPaths),
	}
	assessment, err := b.AssessSnapshotRisk(ctx, delta)
	if err != nil {
		return false, err
	}
	// guard:population acknowledged-passive-delta too-loose: only a committed HEAD base-diff whose nearest acknowledged first-parent split (exact recomputed identity of a base-diff or its content-equivalent workspace candidate) leaves a low-risk passive remainder; code, tests, configuration, operational markdown, and any lookup failure keep the review offer
	return assessment.Level == RiskLow, nil
}
