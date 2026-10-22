package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/reviewerprovider"
)

func readReviewerActivity(t *testing.T, path string) []reviewerActivityEvent {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var events []reviewerActivityEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event reviewerActivityEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("line %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	return events
}

func TestReviewerActivityReportsAFinishedRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.ndjson")
	t.Setenv(reviewerActivityFileEnv, path)
	adapter := withReviewerActivity(providerTestAdapter{raw: []byte(`{"findings":[]}`)}, "review-risk", "opus")
	raw, err := adapter.Review(context.Background(), reviewerprovider.NewInvocation([]byte("prompt")))
	if err != nil || string(raw) != `{"findings":[]}` {
		t.Fatalf("review = %q, %v; the report must not change the result", raw, err)
	}
	events := readReviewerActivity(t, path)
	if len(events) != 2 || events[0].Type != "started" || events[1].Type != "finished" {
		t.Fatalf("events = %+v", events)
	}
	if events[0].ID == "" || events[0].ID != events[1].ID {
		t.Fatalf("one run must keep one id: %+v", events)
	}
	if events[0].Agent != "review-risk" || events[0].Model != "opus" || events[1].Text != `{"findings":[]}` {
		t.Fatalf("events = %+v", events)
	}
}

func TestReviewerActivityReportsFailureAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.ndjson")
	t.Setenv(reviewerActivityFileEnv, path)
	failure := errors.New("claude exited 1")
	_, err := withReviewerActivity(providerTestAdapter{err: failure}, "review-refuter", "").Review(context.Background(), reviewerprovider.NewInvocation([]byte("prompt")))
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v, want the reviewer's own error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = withReviewerActivity(providerTestAdapter{err: ctx.Err()}, "review-risk", "").Review(ctx, reviewerprovider.NewInvocation([]byte("prompt")))
	events := readReviewerActivity(t, path)
	if len(events) != 4 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Model != "default" || events[1].Type != "failed" || events[1].Error != "claude exited 1" || events[1].Cancelled {
		t.Fatalf("failed run = %+v", events[:2])
	}
	if events[3].Type != "failed" || !events[3].Cancelled {
		t.Fatalf("cancelled run = %+v", events[2:])
	}
}

func TestReviewerActivityIsOffWithoutAHostFile(t *testing.T) {
	t.Setenv(reviewerActivityFileEnv, "")
	inner := &providerTestAdapter{raw: []byte("x")}
	if adapter := withReviewerActivity(inner, "review-risk", ""); adapter != reviewerprovider.Adapter(inner) {
		t.Fatalf("adapter = %#v, want the reviewer unchanged", adapter)
	}
}

func TestReviewProviderAdapterNamesTheReviewerRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity.ndjson")
	t.Setenv(reviewerActivityFileEnv, path)
	previous := reviewProviderAdapterFor
	reviewProviderAdapterFor = func(reviewerprovider.Contract, model.AgentID) (reviewerprovider.Adapter, error) {
		return providerTestAdapter{raw: []byte("ok")}, nil
	}
	t.Cleanup(func() { reviewProviderAdapterFor = previous })
	for _, run := range []struct {
		role string
		lens []string
	}{{reviewProviderRoleLens, []string{"review-resilience"}}, {reviewProviderRoleRefuter, nil}, {reviewProviderRoleTargetedValidator, nil}} {
		adapter, err := reviewProviderAdapter(run.role, model.AgentClaudeCode, run.lens...)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := adapter.Review(context.Background(), reviewerprovider.NewInvocation([]byte("prompt"))); err != nil {
			t.Fatal(err)
		}
	}
	var agents []string
	for _, event := range readReviewerActivity(t, path) {
		if event.Type == "started" {
			agents = append(agents, event.Agent)
		}
	}
	if want := []string{"review-resilience", "review-refuter", "review-validator"}; len(agents) != 3 || agents[0] != want[0] || agents[1] != want[1] || agents[2] != want[2] {
		t.Fatalf("agents = %v, want %v", agents, want)
	}
}
