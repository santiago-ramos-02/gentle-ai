package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gentleman-programming/gentle-ai/v4/internal/reviewtransaction"
)

// ReviewConsentSchema identifies the machine-readable `review consent` result.
const ReviewConsentSchema = "gentle-ai.review-consent/v1"

// ReviewConsent is the clone-local answer to the per-candidate review question.
type ReviewConsent string

const (
	// ReviewConsentAsk relays the consent question for every medium- or high-risk candidate.
	ReviewConsentAsk ReviewConsent = "ask"
	// ReviewConsentAlways grants every relayed start in this clone without asking.
	ReviewConsentAlways ReviewConsent = "always"
)

// ReviewConsentResult reports the clone-local consent after a read or write.
type ReviewConsentResult struct {
	Schema    string        `json:"schema"`
	Operation string        `json:"operation"`
	Consent   ReviewConsent `json:"consent"`
}

// ReviewConsentApply runs one consent operation (always, ask, or status) for a
// clone and returns the resulting consent; the headless API shares it.
func ReviewConsentApply(ctx context.Context, cwd, operation string) (ReviewConsentResult, error) {
	switch operation {
	case "always", "ask":
		if err := reviewtransaction.SetRDDConsentAlways(ctx, cwd, operation == "always"); err != nil {
			return ReviewConsentResult{}, err
		}
	case "status":
	default:
		return ReviewConsentResult{}, fmt.Errorf("unknown review consent command %q; %s", operation, reviewConsentUsage)
	}
	always, err := reviewtransaction.RDDConsentAlways(ctx, cwd)
	if err != nil {
		return ReviewConsentResult{}, err
	}
	result := ReviewConsentResult{Schema: ReviewConsentSchema, Operation: operation, Consent: ReviewConsentAsk}
	if always {
		result.Consent = ReviewConsentAlways
	}
	return result, nil
}

const reviewConsentUsage = "Usage: gentle-ai review consent <always|ask|status> [--cwd <repo>] [--json]"

// RunReviewConsent reads or sets the clone-local standing grant. It never
// overrides `review mode`: with reviews off, nothing runs regardless of consent.
func RunReviewConsent(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		_, _ = fmt.Fprintln(stdout, reviewConsentUsage)
		_, _ = fmt.Fprintln(stdout, "always: review every medium- or high-risk change in this clone without asking first. ask (the default): ask for each change. The setting is local to this clone, is never committed, and 'gentle-ai review mode disable' still turns reviews off.")
		return nil
	}
	operation := args[0]
	switch operation {
	case "always", "ask", "status":
	default:
		return fmt.Errorf("unknown review consent command %q; %s", operation, reviewConsentUsage)
	}

	flags := newReviewFlagSet("review consent "+operation, stdout, "Read or set whether this clone asks before each review.")
	cwd := flags.String("cwd", ".", "repository path")
	emitJSON := flags.Bool("json", false, "emit the machine-readable review consent result")
	if err := parseReviewFlags(flags, args[1:]); err != nil {
		return err
	}
	if reviewHelpRequested(args[1:]) {
		return nil
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected review consent argument %q", flags.Arg(0))
	}

	result, err := ReviewConsentApply(context.Background(), *cwd, operation)
	if err != nil {
		return err
	}
	if *emitJSON {
		return encodeReviewJSON(stdout, result)
	}
	if result.Consent == ReviewConsentAlways {
		_, err = fmt.Fprintln(stdout, "review consent: always (this clone reviews medium- and high-risk changes without asking)")
	} else {
		_, err = fmt.Fprintln(stdout, "review consent: ask (each medium- or high-risk change asks first)")
	}
	return err
}
