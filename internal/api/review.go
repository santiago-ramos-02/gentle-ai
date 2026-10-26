package api

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/gentleman-programming/gentle-ai/v4/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v4/internal/reviewtransaction"
)

// requireCwd validates a project directory parameter. It must be absolute:
// the API never resolves paths against its own working directory.
func requireCwd(cwd string) error {
	if cwd == "" {
		return invalidParams("cwd is required")
	}
	if !filepath.IsAbs(cwd) {
		return invalidParams("cwd must be an absolute path, got %q", cwd)
	}
	return nil
}

type cwdParams struct {
	Cwd string `json:"cwd"`
}

// reviewStatus reads the review switch. Without a cwd it reads the global
// setting from the home directory, where no clone override applies.
func reviewStatus(ctx context.Context, env *env, params cwdParams) (any, error) {
	cwd := params.Cwd
	if cwd == "" {
		cwd = env.deps.HomeDir
	} else if err := requireCwd(cwd); err != nil {
		return nil, err
	}
	result, err := env.deps.ReviewMode(ctx, cwd, "status", "global")
	if err != nil {
		return nil, errorf(CodeFailed, "%v", err)
	}
	return result, nil
}

type reviewSetParams struct {
	Cwd     string `json:"cwd"`
	Enabled *bool  `json:"enabled"`
	Scope   string `json:"scope"`
}

// reviewSet flips the receipt-driven-development switch. The default global
// scope is what the TUI's review mode screen changes.
func reviewSet(ctx context.Context, env *env, params reviewSetParams) (any, error) {
	// Only a clone override needs a project; the global switch is set from home.
	cwd := params.Cwd
	if cwd == "" && params.Scope != "clone" {
		cwd = env.deps.HomeDir
	} else if err := requireCwd(cwd); err != nil {
		return nil, err
	}
	if params.Enabled == nil {
		return nil, invalidParams("enabled is required")
	}
	scope := params.Scope
	switch scope {
	case "":
		scope = "global"
	case "global", "clone":
	default:
		return nil, invalidParams("scope must be global or clone, got %q", params.Scope)
	}
	operation := "disable"
	if *params.Enabled {
		operation = "enable"
	}
	result, err := env.deps.ReviewMode(ctx, cwd, operation, scope)
	if err != nil {
		return nil, errorf(CodeFailed, "%v", err)
	}
	return result, nil
}

// reviewConsent reads whether this clone asks before each review. Consent is
// clone-local, so unlike review.status it always needs the project.
func reviewConsent(ctx context.Context, env *env, params cwdParams) (any, error) {
	if err := requireCwd(params.Cwd); err != nil {
		return nil, err
	}
	result, err := env.deps.ReviewConsent(ctx, params.Cwd, "status")
	if err != nil {
		return nil, errorf(CodeFailed, "%v", err)
	}
	return result, nil
}

type reviewConsentSetParams struct {
	Cwd     string `json:"cwd"`
	Consent string `json:"consent"`
}

// reviewConsentSet records ("always") or removes ("ask") the clone's standing grant.
func reviewConsentSet(ctx context.Context, env *env, params reviewConsentSetParams) (any, error) {
	if err := requireCwd(params.Cwd); err != nil {
		return nil, err
	}
	switch params.Consent {
	case string(cli.ReviewConsentAlways), string(cli.ReviewConsentAsk):
	default:
		return nil, invalidParams("consent must be always or ask, got %q", params.Consent)
	}
	result, err := env.deps.ReviewConsent(ctx, params.Cwd, params.Consent)
	if err != nil {
		return nil, errorf(CodeFailed, "%v", err)
	}
	return result, nil
}

func storeResult(report reviewtransaction.StoreResetReport) cli.ReviewStoreResetResult {
	return cli.ReviewStoreResetResult{Schema: cli.ReviewStoreResetSchema, Operation: "review/store-reset", Report: report}
}

// reviewStoreSurvey previews what a reset would remove; it never removes.
func reviewStoreSurvey(ctx context.Context, env *env, params cwdParams) (any, error) {
	if err := requireCwd(params.Cwd); err != nil {
		return nil, err
	}
	report, err := env.deps.SurveyReviewStore(ctx, params.Cwd, reviewtransaction.StoreResetRequest{})
	if err != nil {
		return nil, errorf(CodeFailed, "survey review store: %v", err)
	}
	return storeResult(report), nil
}

type reviewStoreResetParams struct {
	Cwd                   string `json:"cwd"`
	IncludeInFlight       bool   `json:"includeInFlight"`
	IncludeAdapterReviews bool   `json:"includeAdapterReviews"`
}

// reviewStoreReset removes the clone's review lineage state. Reviews that are
// still open are refused with conflict unless includeInFlight is set.
func reviewStoreReset(ctx context.Context, env *env, params reviewStoreResetParams) (any, error) {
	if err := requireCwd(params.Cwd); err != nil {
		return nil, err
	}
	report, err := env.deps.ResetReviewStore(ctx, params.Cwd, reviewtransaction.StoreResetRequest{
		IncludeInFlight:       params.IncludeInFlight,
		IncludeAdapterReviews: params.IncludeAdapterReviews,
	})
	var refused *reviewtransaction.StoreResetInFlightError
	if errors.As(err, &refused) {
		return nil, errorf(CodeConflict, "%v", err)
	}
	if err != nil {
		return nil, errorf(CodeFailed, "reset review store: %v", err)
	}
	return storeResult(report), nil
}
