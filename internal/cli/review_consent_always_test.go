package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/reviewtransaction"
)

func runReviewConsentCommand(t *testing.T, args ...string) ReviewConsentResult {
	t.Helper()
	var output bytes.Buffer
	if err := RunReviewConsent(append(args, "--json"), &output); err != nil {
		t.Fatalf("review consent %v: %v\n%s", args, err, output.String())
	}
	var result ReviewConsentResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode review consent %v: %v\n%s", args, err, output.String())
	}
	return result
}

func TestCloneConsentAlwaysGrantsEveryRelayedStartUntilAskIsRestored(t *testing.T) {
	reviewEnabledHome(t)
	repo := initReviewCLIRepo(t)
	stubReviewConsole(t, false, "")

	if status := runReviewConsentCommand(t, "status", "--cwd", repo); status.Consent != ReviewConsentAsk {
		t.Fatalf("default consent = %q, want %q", status.Consent, ReviewConsentAsk)
	}
	if set := runReviewConsentCommand(t, "always", "--cwd", repo); set.Consent != ReviewConsentAlways {
		t.Fatalf("always consent = %q", set.Consent)
	}
	if status := runReviewConsentCommand(t, "status", "--cwd", repo); status.Consent != ReviewConsentAlways {
		t.Fatalf("status after always = %q", status.Consent)
	}

	writeReviewStartCandidate(t, repo, "scripts/deploy.sh", "echo deploy\n", 0o644)
	started := decodeNegotiatedReviewStart(t, runConsentRelayStart(t, boundNegotiatedStartArgs(t, []string{
		"start", "--contract", ReviewIntegrationContractV2, "--cwd", repo,
		"--lineage", "review-consent-always-a", "--consent", "relay",
	})).Bytes())
	if started.Action != "created" || started.RiskLevel != reviewtransaction.RiskHigh || len(started.SelectedLenses) != 4 {
		t.Fatalf("always consent did not start the review: %#v", started)
	}
	if asked, err := reviewtransaction.RDDConsentAsked(context.Background(), repo); err != nil || asked {
		t.Fatalf("always consent must not touch the legacy latch: asked=%v err=%v", asked, err)
	}

	if ask := runReviewConsentCommand(t, "ask", "--cwd", repo); ask.Consent != ReviewConsentAsk {
		t.Fatalf("ask consent = %q", ask.Consent)
	}
	writeReviewStartCandidate(t, repo, "scripts/second.sh", "echo second\n", 0o644)
	question := decodeConsentQuestion(t, runConsentRelayStart(t, boundNegotiatedStartArgs(t, []string{
		"start", "--contract", ReviewIntegrationContractV2, "--cwd", repo,
		"--lineage", "review-consent-always-b", "--consent", "relay",
	})).Bytes())
	if question.Action != reviewConsentActionRequired {
		t.Fatalf("restored ask did not return the consent question: %#v", question)
	}
}

func TestCloneConsentAlwaysNeverOverridesTheKillSwitch(t *testing.T) {
	reviewEnabledHome(t)
	repo := initReviewCLIRepo(t)
	stubReviewConsole(t, false, "")
	runReviewConsentCommand(t, "always", "--cwd", repo)
	writeReviewStartCandidate(t, repo, "scripts/deploy.sh", "echo deploy\n", 0o644)
	status := negotiatedStartStatusForContract(t, repo, ReviewIntegrationContractV2, "--lineage", "review-consent-always-off")
	var modeOutput bytes.Buffer
	if err := RunReviewMode([]string{"disable", "--cwd", repo, "--json"}, &modeOutput); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := RunReview(transitionStartArgs(repo, status), &output); err == nil {
		t.Fatalf("always consent bypassed review mode off:\n%s", output.String())
	}
}

func TestCloneConsentAlwaysKeepsAnExplicitDecline(t *testing.T) {
	reviewEnabledHome(t)
	repo := initReviewCLIRepo(t)
	stubReviewConsole(t, false, "")
	writeReviewStartCandidate(t, repo, "scripts/deploy.sh", "echo deploy\n", 0o644)
	question := decodeConsentQuestion(t, runConsentRelayStart(t, boundNegotiatedStartArgs(t, []string{
		"start", "--contract", ReviewIntegrationContractV2, "--cwd", repo,
		"--lineage", "review-consent-always-decline", "--consent", "relay",
	})).Bytes())
	runReviewConsentCommand(t, "always", "--cwd", repo)

	var declined ReviewFacadeStartResult
	decodeStrictReviewJSON(t, runConsentRelayStart(t, invocationArgs(t, question.Choices[1].Invocation)).Bytes(), &declined)
	if declined.Action != "declined" || declined.Consent != ReviewStartConsentDeclinedThisCandidate || declined.LineageID != "" {
		t.Fatalf("always consent turned an explicit decline into a review: %#v", declined)
	}
}

func TestReviewConsentRejectsUnknownVerb(t *testing.T) {
	var output bytes.Buffer
	if err := RunReviewConsent([]string{"sometimes"}, &output); err == nil {
		t.Fatal("unknown consent verb was accepted")
	}
}
