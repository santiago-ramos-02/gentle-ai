package reviewtransaction

import (
	"context"
	"strconv"
	"strings"
	"sync"
)

// One negotiated operation asks Git the same stable questions many times:
// a lineage-bound STATUS spawned 87 Git processes, 60 of them repeats of the
// repository root or of the same immutable tree diff. Each spawn costs about
// 70ms on Windows and several times that under load, which was enough to
// push STATUS past its 25-second budget. The memo answers a repeat from the
// first run, for the lifetime of the operation's context only.
type gitMemo struct {
	mu      sync.Mutex
	entries map[string]gitMemoEntry
	// isolations maps each isolated immutable-tree Git directory to the
	// repository it views. Isolations of one repository are built identically
	// and differ only in their temporary paths, so they share answers.
	isolations map[string]string
}

type gitMemoEntry struct {
	output []byte
	total  int
}

type gitMemoContextKey struct{}

// WithGitMemo scopes Git query memoization to one operation. Only queries
// whose answer cannot change while the operation runs are memoized.
func WithGitMemo(ctx context.Context) context.Context {
	return context.WithValue(ctx, gitMemoContextKey{}, &gitMemo{entries: map[string]gitMemoEntry{}, isolations: map[string]string{}})
}

func gitMemoFrom(ctx context.Context) *gitMemo {
	memo, _ := ctx.Value(gitMemoContextKey{}).(*gitMemo)
	return memo
}

func registerGitMemoIsolation(ctx context.Context, gitDir, repository string) {
	if memo := gitMemoFrom(ctx); memo != nil {
		memo.mu.Lock()
		defer memo.mu.Unlock()
		memo.isolations[gitDir] = repository
	}
}

func (memo *gitMemo) load(key string) ([]byte, int, bool) {
	memo.mu.Lock()
	defer memo.mu.Unlock()
	entry, ok := memo.entries[key]
	if !ok {
		return nil, 0, false
	}
	return append([]byte(nil), entry.output...), entry.total, true
}

func (memo *gitMemo) store(key string, output []byte, total int) {
	memo.mu.Lock()
	defer memo.mu.Unlock()
	memo.entries[key] = gitMemoEntry{output: append([]byte(nil), output...), total: total}
}

// key shares answers across isolations only for diffs: their output names
// objects and paths in the tree, never the isolation's own directory.
func (memo *gitMemo) key(command, repo string, extraEnv []string, outputOffset, outputLimit int, isolateConfig, rejectStderr, rejectOverflow bool, args []string) string {
	key := gitMemoKey(repo, extraEnv, outputOffset, outputLimit, isolateConfig, rejectStderr, rejectOverflow, args)
	if command != "diff" && command != "diff-tree" {
		return key
	}
	memo.mu.Lock()
	defer memo.mu.Unlock()
	for gitDir, repository := range memo.isolations {
		key = strings.ReplaceAll(key, gitDir, "\x02immutable-tree:"+repository+"\x02")
	}
	return key
}

func gitMemoKey(repo string, extraEnv []string, outputOffset, outputLimit int, isolateConfig, rejectStderr, rejectOverflow bool, args []string) string {
	fields := []string{
		repo,
		strings.Join(extraEnv, "\x00"),
		strconv.Itoa(outputOffset),
		strconv.Itoa(outputLimit),
		strconv.FormatBool(isolateConfig),
		strconv.FormatBool(rejectStderr),
		strconv.FormatBool(rejectOverflow),
	}
	return strings.Join(append(fields, args...), "\x01")
}

var stableRevParseFlags = map[string]bool{
	"--show-toplevel":      true,
	"--git-common-dir":     true,
	"--git-dir":            true,
	"--show-object-format": true,
}

// stableDiffFlags only shape how a tree-to-tree diff is printed. An allowlist,
// because flags such as -S or -G take the next argument as their value, which
// would turn a "two object ids" query into one against the worktree.
var stableDiffFlags = map[string]bool{
	"--always": true, "--binary": true, "--full-index": true, "--name-only": true,
	"--name-status": true, "--no-color": true, "--no-commit-id": true, "--no-ext-diff": true,
	"--no-indent-heuristic": true, "--no-renames": true, "--no-textconv": true, "--numstat": true,
	"--patch": true, "-p": true, "--raw": true, "--stat": true, "--text": true,
	"-M": true, "-r": true, "-z": true,
}

var stableDiffFlagPrefixes = []string{"--abbrev=", "--diff-algorithm=", "--ignore-submodules=", "--unified="}

func isStableDiffFlag(flag string) bool {
	if stableDiffFlags[flag] {
		return true
	}
	for _, prefix := range stableDiffFlagPrefixes {
		if strings.HasPrefix(flag, prefix) {
			return true
		}
	}
	return false
}

// gitMemoQuery returns the Git command and whether its answer is fixed for
// the operation: repository identity, or a diff between two full object ids.
func gitMemoQuery(args []string) (string, bool) {
	// Leading global options are part of the memo key, so they cannot make
	// two different queries share an answer.
	for len(args) > 0 && args[0] == "--no-pager" || len(args) > 1 && args[0] == "-c" {
		if args[0] == "-c" {
			args = args[2:]
		} else {
			args = args[1:]
		}
	}
	if len(args) < 2 {
		return "", false
	}
	command := args[0]
	switch command {
	case "rev-parse":
		for _, arg := range args[1:] {
			if !stableRevParseFlags[arg] {
				return command, false
			}
		}
		return command, true
	case "diff", "diff-tree":
		objects := 0
		for _, arg := range args[1:] {
			if arg == "--" {
				break
			}
			if strings.HasPrefix(arg, "-") {
				if !isStableDiffFlag(arg) {
					return command, false
				}
				continue
			}
			if !isFullGitObjectID(arg) {
				return command, false
			}
			objects++
		}
		return command, objects == 2
	}
	return command, false
}

func isFullGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
