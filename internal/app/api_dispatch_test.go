package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
)

// TestRunArgsAPIDispatchesBeforeDetectionAndSelfUpdate proves the api command
// needs no terminal, never self-updates, and never reaches the platform-bound
// dispatch that runs system detection.
func TestRunArgsAPIDispatchesBeforeDetectionAndSelfUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	origSelfUpdate, origDetect, origIsatty := selfUpdateFn, detectSystem, isattyFn
	t.Cleanup(func() { selfUpdateFn, detectSystem, isattyFn = origSelfUpdate, origDetect, origIsatty })
	selfUpdateFn = func(context.Context, string, system.PlatformProfile, io.Writer) error {
		t.Fatal("api must not self-update")
		return nil
	}
	detectSystem = func(context.Context) (system.DetectionResult, error) {
		return system.DetectionResult{}, errors.New("api must not reach the platform dispatch")
	}
	isattyFn = func(uintptr) bool { return false }

	var stdout bytes.Buffer
	if err := RunArgs([]string{"api", "describe"}, &stdout); err != nil {
		t.Fatalf("RunArgs(api describe) error = %v, output %q", err, stdout.String())
	}
	var final struct {
		Type   string `json:"type"`
		Schema string `json:"schema"`
		Data   struct {
			Methods []string `json:"methods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &final); err != nil {
		t.Fatalf("describe output is not one JSON line: %q: %v", stdout.String(), err)
	}
	if final.Type != "result" || final.Schema != "gentle-ai.api/v1" || len(final.Data.Methods) == 0 {
		t.Fatalf("describe envelope = %+v", final)
	}

	stdout.Reset()
	err := RunArgs([]string{"api", "no-such-method"}, &stdout)
	if err == nil || !strings.Contains(stdout.String(), `"type":"error"`) {
		t.Fatalf("unknown method: err = %v, output %q", err, stdout.String())
	}
}
