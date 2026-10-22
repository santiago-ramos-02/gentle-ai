package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/gentleman-programming/gentle-ai/v4/internal/reviewerprovider"
)

// reviewerActivityFileEnv names the file a host app sets so it can show reviewer runs
// as agents. Reviewers run inside one `gentle-ai review` command, so the host
// would otherwise see only that command. Each run appends one "started" line
// and one "finished" or "failed" line as NDJSON.
const reviewerActivityFileEnv = "GENTLE_AI_REVIEWER_ACTIVITY_FILE"

// reviewerActivityTextLimit bounds the reviewer reply copied into the report; the
// review itself always admits the full reply.
const reviewerActivityTextLimit = 64 * 1024

// reviewerActivityEvent is one NDJSON line of the reviewer activity file.
type reviewerActivityEvent struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Agent     string `json:"agent,omitempty"`
	Model     string `json:"model,omitempty"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
	At        int64  `json:"at"`
}

// withReviewerActivity reports inner's runs to the host's activity file, or returns
// inner unchanged when no host asked for them. agent names the reviewer role
// (such as "review-risk") and model the model it runs, empty for the default.
func withReviewerActivity(inner reviewerprovider.Adapter, agent, model string) reviewerprovider.Adapter {
	path := os.Getenv(reviewerActivityFileEnv)
	if path == "" {
		return inner
	}
	return reviewerActivityAdapter{inner: inner, path: path, agent: agent, model: model, now: time.Now}
}

type reviewerActivityAdapter struct {
	inner        reviewerprovider.Adapter
	path         string
	agent, model string
	now          func() time.Time
}

func (adapter reviewerActivityAdapter) Review(ctx context.Context, invocation reviewerprovider.Invocation) ([]byte, error) {
	id := adapter.runID()
	model := adapter.model
	if model == "" {
		model = "default"
	}
	adapter.report(reviewerActivityEvent{Type: "started", ID: id, Agent: adapter.agent, Model: model})
	raw, err := adapter.inner.Review(ctx, invocation)
	if err != nil {
		cancelled := errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled)
		adapter.report(reviewerActivityEvent{Type: "failed", ID: id, Error: err.Error(), Cancelled: cancelled})
		return raw, err
	}
	text := raw
	if len(text) > reviewerActivityTextLimit {
		text = text[:reviewerActivityTextLimit]
	}
	adapter.report(reviewerActivityEvent{Type: "finished", ID: id, Text: string(text)})
	return raw, nil
}

func (adapter reviewerActivityAdapter) runID() string {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "review-" + adapter.now().Format("20060102T150405.000000000")
	}
	return "review-" + hex.EncodeToString(bytes[:])
}

// report is display only: a host that cannot be told never changes the review.
func (adapter reviewerActivityAdapter) report(event reviewerActivityEvent) {
	event.At = adapter.now().UnixMilli()
	line, err := json.Marshal(event)
	if err != nil {
		return
	}
	file, err := os.OpenFile(adapter.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	// One write per line keeps concurrent reviewers' lines whole in append mode.
	_, _ = file.Write(append(line, '\n'))
}
