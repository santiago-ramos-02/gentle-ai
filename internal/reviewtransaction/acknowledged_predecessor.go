package reviewtransaction

import "context"

// AcknowledgedPassivePredecessor reports whether a live committed base-diff
// candidate only adds passive content to a candidate that was already
// approved and acknowledged (issue #4739). After an acknowledgement, one more
// commit that touches a tracking note changes the target identity, and the
// full base-diff would otherwise be offered for review again although every
// reviewable byte in it was just approved.
//
// The acknowledged candidate may be a committed base-diff or the uncommitted
// workspace candidate the same trees were before their commit: the compact
// store already treats current-changes and base-diff as content-equivalent
// kinds. Three shapes qualify:
//   - base..HEAD itself was acknowledged as a workspace candidate, so
//     committing exactly the reviewed bytes needs no second review;
//   - base..ancestor was acknowledged and ancestor..HEAD is passive;
//   - ancestor..HEAD was acknowledged, typically the workspace candidate
//     reviewed on top of that ancestor, and base..ancestor is passive, such as
//     the feature's task document committed before the reviewed work.
//
// The walk follows HEAD's first-parent ancestry back to the candidate's base
// tree, recomputes each candidate identity exactly, and stops at the nearest
// ancestor with terminal consumption evidence. Only a remaining delta that
// classifies as low risk (passive content only) qualifies.
//
// It runs inside STATUS's aggregate budget, so it costs a fixed number of Git
// processes however deep the ancestry is: a worktree with no terminal facts
// returns before any walk, and every pair's paths come from one batched diff.
//
// The result may only suppress a fresh review offer. Terminal consumption is
// a deduplication fact, never authority: callers must not derive a receipt or
// a delivery gate from it, must prefer live authority, and must offer the
// review whenever this returns an error.
func AcknowledgedPassivePredecessor(ctx context.Context, repo string, live Snapshot) (bool, error) {
	if live.Kind != TargetBaseDiff || len(live.IntendedUntracked) != 0 || len(live.Paths) == 0 {
		return false, nil
	}
	builder := SnapshotBuilder{Repo: repo}
	root, err := builder.repositoryRoot(ctx)
	if err != nil {
		return false, err
	}
	builder.Repo = root
	consumed, err := compactConsumedTargets(ctx, root)
	if err != nil || len(consumed) == 0 {
		return false, err
	}
	// The ancestry is HEAD's, so the candidate must be exactly HEAD's tree.
	headTree, err := builder.resolveTree(ctx, "HEAD")
	if err != nil || headTree != live.CandidateTree {
		return false, err
	}
	if acknowledgedCandidate(consumed, live.Projection, live.BaseTree, live.CandidateTree, live.Paths) {
		return true, nil
	}
	ancestors, err := selectorlessCommittedBaseDiffAncestors(ctx, root)
	if err != nil {
		return false, err
	}
	for index, ancestor := range ancestors {
		if ancestor.tree == live.BaseTree {
			ancestors = ancestors[:index]
			break
		}
	}
	pairs := make([][2]string, 0, 2*len(ancestors))
	for _, ancestor := range ancestors {
		pairs = append(pairs, [2]string{live.BaseTree, ancestor.tree}, [2]string{ancestor.tree, live.CandidateTree})
	}
	paths, err := builder.changedPathsBatch(ctx, pairs)
	if err != nil {
		return false, err
	}
	for index, ancestor := range ancestors {
		beforePaths, afterPaths := paths[2*index], paths[2*index+1]
		if acknowledgedCandidate(consumed, live.Projection, live.BaseTree, ancestor.tree, beforePaths) {
			return builder.passiveDelta(ctx, live, ancestor.tree, live.CandidateTree, afterPaths)
		}
		if acknowledgedCandidate(consumed, live.Projection, ancestor.tree, live.CandidateTree, afterPaths) {
			return builder.passiveDelta(ctx, live, live.BaseTree, ancestor.tree, beforePaths)
		}
	}
	return false, nil
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
