package reviewtransaction

import (
	"context"
	"strings"
)

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
	boundary, ok, err := AcknowledgedBoundaryCommit(ctx, repo, live)
	if err != nil || !ok {
		return false, err
	}
	return boundary.Passive, nil
}

// AcknowledgedBoundary reports the nearest acknowledged first-parent ancestor
// of a live committed base-diff candidate: the reviewed boundary (issue #5638).
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
	// Passive reports whether the part of the live range no acknowledgement
	// covers classifies as low risk (the #4739 passive suppression, folded
	// into the same walk).
	Passive bool
}

// AcknowledgedBoundaryCommit walks HEAD's first-parent ancestry back to the
// candidate's base tree, recomputes each ancestor's base-diff identity against
// that same base, and stops at the nearest ancestor with terminal consumption
// evidence: the reviewed boundary. ok is false when no ancestor in the window
// was acknowledged (or the walk reaches the base first); err is non-nil for
// any Git or tombstone fault, and callers must keep the pre-existing offer.
//
// The result may only reshape or withhold a fresh review offer. Terminal
// consumption is a deduplication fact, never authority: callers must not
// derive a receipt or a delivery gate from it, must prefer live authority, and
// must keep a live lineage visible.
func AcknowledgedBoundaryCommit(ctx context.Context, repo string, live Snapshot) (AcknowledgedBoundary, bool, error) {
	if live.Kind != TargetBaseDiff || len(live.IntendedUntracked) != 0 || len(live.Paths) == 0 {
		return AcknowledgedBoundary{}, false, nil
	}
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
	if acknowledgedCandidate(consumed, live.Projection, live.BaseTree, live.CandidateTree, live.Paths) {
		// The whole range was reviewed, typically as the workspace candidate
		// before its commit: the boundary reaches the live content.
		head, err := runGit(ctx, root, nil, nil, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			return AcknowledgedBoundary{}, false, err
		}
		return AcknowledgedBoundary{Commit: strings.TrimSpace(string(head)), Tree: headTree, Passive: true}, true, nil
	}
	ancestors, err := selectorlessCommittedBaseDiffAncestors(ctx, root)
	if err != nil {
		return AcknowledgedBoundary{}, false, err
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
		return AcknowledgedBoundary{}, false, err
	}
	for index, ancestor := range ancestors {
		beforePaths, afterPaths := paths[2*index], paths[2*index+1]
		boundary := AcknowledgedBoundary{Commit: ancestor.commit, Tree: ancestor.tree, DeltaPaths: afterPaths}
		if acknowledgedCandidate(consumed, live.Projection, live.BaseTree, ancestor.tree, beforePaths) {
			if len(afterPaths) == 0 {
				// Every commit since the boundary was empty: the boundary reaches
				// the live content, so there is no delta to classify.
				return boundary, true, nil
			}
			boundary.Passive, err = builder.passiveDelta(ctx, live, ancestor.tree, live.CandidateTree, afterPaths)
			if err != nil {
				return AcknowledgedBoundary{}, false, err
			}
			return boundary, true, nil
		}
		if acknowledgedCandidate(consumed, live.Projection, ancestor.tree, live.CandidateTree, afterPaths) {
			// The reviewed work sits on top of the boundary, so the part no
			// acknowledgement covers is base..boundary. Only a passive one
			// suppresses the offer; otherwise the full range stays offered.
			passive, err := builder.passiveDelta(ctx, live, live.BaseTree, ancestor.tree, beforePaths)
			if err != nil || !passive {
				return AcknowledgedBoundary{}, false, err
			}
			boundary.Passive = true
			return boundary, true, nil
		}
	}
	return AcknowledgedBoundary{}, false, nil
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
