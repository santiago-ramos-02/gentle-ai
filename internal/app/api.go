package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gentleman-programming/gentle-ai/v3/internal/api"
)

// runAPI serves `gentle-ai api <method>`. It never self-updates, prompts, or
// needs a terminal: a terminal on stdin means "no params" rather than a read
// that would block. While the method runs, os.Stdout points at stderr so no
// service output can interleave with the NDJSON stream on the real stdout.
func runAPI(args []string, stdout io.Writer) error {
	stdin := io.Reader(os.Stdin)
	if isattyFn(os.Stdin.Fd()) {
		stdin = strings.NewReader("")
	}
	previousStdout := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = previousStdout }()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return api.Unavailable(stdout, fmt.Errorf("resolve user home directory: %w", err))
	}
	return api.Run(context.Background(), args, stdin, stdout, api.DefaultDeps(Version, homeDir))
}
