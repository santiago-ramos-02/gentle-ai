package reviewerprovider

import (
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// TestRuntimeBudgetCoversEveryRegisteredRuntime pins the per-runtime context
// policy to the closed runtime registry: runtimes whose reviewers run on
// Claude receive the Claude cap and every other identity the provider contract
// admits receives the approved cap. The table makes a newly registered runtime
// fail here until someone decides its cap.
func TestRuntimeBudgetCoversEveryRegisteredRuntime(t *testing.T) {
	want := map[string]int{
		"claude-code": ClaudeRuntimeContextBudget,
		"codex":       ApprovedRuntimeContextBudget,
		"opencode":    ApprovedRuntimeContextBudget,
		"pi":          ClaudeRuntimeContextBudget,
	}
	for _, identity := range RegisteredRuntimeIdentities() {
		t.Run(identity, func(t *testing.T) {
			budget, decided := want[identity]
			if !decided {
				t.Fatalf("runtime %q has no decided context budget", identity)
			}
			if got := RuntimeContextBudget(model.AgentID(identity)); got != budget {
				t.Fatalf("RuntimeContextBudget(%q) = %d, want %d", identity, got, budget)
			}
		})
	}
}

// TestRuntimeBudgetUnknownIdentityFailsClosed pins the fail-closed answer for
// an identity the contract does not register, including the empty identity a
// manual, non-agent review carries: it never receives a larger budget than a
// supported one. Whether an unknown identity may act at all stays with the
// existing runtime validation paths; the budget only makes sure it can never
// buy its way to a wider context than an admitted runtime.
func TestRuntimeBudgetUnknownIdentityFailsClosed(t *testing.T) {
	for _, identity := range []model.AgentID{"", "unknown-runtime", "claude-code-impersonator"} {
		if got := RuntimeContextBudget(identity); got != ApprovedRuntimeContextBudget {
			t.Fatalf("RuntimeContextBudget(%q) = %d, want the fail-closed %d byte cap; an unknown runtime never earns a larger budget", identity, got, ApprovedRuntimeContextBudget)
		}
	}
}
