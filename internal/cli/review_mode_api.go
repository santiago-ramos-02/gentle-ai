package cli

import (
	"context"
	"fmt"
)

// ReviewMode reads the review mode (operation "status") or enables or disables
// it for scope ("global" or "clone") and reports the gentle-ai.review-mode/v1
// result. Clone writes replace whatever revision is current.
func ReviewMode(ctx context.Context, cwd, operation, scope string) (ReviewModeResult, error) {
	switch operation {
	case "enable", "disable", "status":
	default:
		return ReviewModeResult{}, fmt.Errorf("unknown review mode command %q", operation)
	}
	switch scope {
	case reviewModeScopeGlobal, reviewModeScopeClone:
	default:
		return ReviewModeResult{}, fmt.Errorf("unknown review mode scope %q", scope)
	}
	result := ReviewModeResult{Schema: ReviewModeSchema, Operation: operation, Scope: scope}
	var err error
	switch {
	case operation == "status":
		result.Scope = reviewModeScopeBoth
		result.Status, err = ReviewModeStatus(ctx, cwd)
	case scope == reviewModeScopeGlobal:
		result.Status, err = SetGlobalReviewMode(ctx, cwd, operation == "enable")
	default:
		result.Status, err = applyReviewMode(ctx, cwd, operation, scope, "", false)
	}
	return result, err
}
