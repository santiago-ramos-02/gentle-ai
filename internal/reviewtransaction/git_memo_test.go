package reviewtransaction

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
)

func countGitSpawns(t *testing.T) *int {
	t.Helper()
	originalCommand := gitCommandContext
	t.Cleanup(func() { gitCommandContext = originalCommand })
	count := 0
	gitCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		count++
		return originalCommand(ctx, name, args...)
	}
	return &count
}

func headTree(t *testing.T, repo string) string {
	t.Helper()
	output, err := runGit(context.Background(), repo, nil, nil, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func TestGitMemoRunsEachStableQueryOncePerOperation(t *testing.T) {
	repo := initSnapshotRepo(t)
	tree := headTree(t, repo)
	const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	queries := [][]string{
		{"rev-parse", "--show-toplevel"},
		{"rev-parse", "--show-toplevel", "--git-common-dir", "--git-dir"},
		{"rev-parse", "--show-object-format"},
		{"diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--no-renames", emptyTree, tree},
		{"diff", "--raw", "-z", "--full-index", "--no-renames", "--no-ext-diff", emptyTree, tree, "--"},
	}
	for _, query := range queries {
		t.Run(strings.Join(query, " "), func(t *testing.T) {
			spawns := countGitSpawns(t)
			ctx := WithGitMemo(context.Background())
			first, err := runGit(ctx, repo, nil, nil, query...)
			if err != nil {
				t.Fatal(err)
			}
			second, err := runGit(ctx, repo, nil, nil, query...)
			if err != nil {
				t.Fatal(err)
			}
			if *spawns != 1 {
				t.Fatalf("spawned Git %d times inside one operation, want 1", *spawns)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("memoized output %q differs from the first run %q", second, first)
			}
		})
	}
}

func TestGitMemoReturnsAnIndependentCopy(t *testing.T) {
	repo := initSnapshotRepo(t)
	ctx := WithGitMemo(context.Background())
	first, err := runGit(ctx, repo, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	want := string(first)
	for index := range first {
		first[index] = 'x'
	}
	second, err := runGit(ctx, repo, nil, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != want {
		t.Fatalf("caller mutation leaked into the memo: got %q, want %q", second, want)
	}
}

func TestGitMemoReRunsQueriesThatCanChange(t *testing.T) {
	repo := initSnapshotRepo(t)
	tree := headTree(t, repo)
	queries := [][]string{
		{"rev-parse", "--verify", "HEAD^{tree}"},
		{"diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "HEAD", tree},
		{"diff", "--raw", "-z", tree},
		{"diff", "-S", tree, tree},
		{"diff", "-G", tree, tree},
		{"ls-files", "--cached", "-z"},
	}
	for _, query := range queries {
		t.Run(strings.Join(query, " "), func(t *testing.T) {
			spawns := countGitSpawns(t)
			ctx := WithGitMemo(context.Background())
			for range 2 {
				if _, err := runGit(ctx, repo, nil, nil, query...); err != nil {
					t.Fatal(err)
				}
			}
			if *spawns != 2 {
				t.Fatalf("spawned Git %d times for a query whose answer can change, want 2", *spawns)
			}
		})
	}
}

func TestGitWithoutMemoRunsEveryQuery(t *testing.T) {
	repo := initSnapshotRepo(t)
	spawns := countGitSpawns(t)
	for range 2 {
		if _, err := runGit(context.Background(), repo, nil, nil, "rev-parse", "--show-toplevel"); err != nil {
			t.Fatal(err)
		}
	}
	if *spawns != 2 {
		t.Fatalf("spawned Git %d times without an operation memo, want 2", *spawns)
	}
}

func TestGitMemoSharesAnswersAcrossIsolationsOfOneRepository(t *testing.T) {
	repo := initSnapshotRepo(t)
	tree := headTree(t, repo)
	const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	ctx := WithGitMemo(context.Background())
	isolations := make([][]string, 2)
	attributesFiles := make([]string, 2)
	for index := range isolations {
		isolation, attributesFile, cleanup, err := isolatedImmutableTreeGitWithAttributesFile(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cleanup() })
		isolations[index], attributesFiles[index] = isolation, attributesFile
	}
	spawns := countGitSpawns(t)
	var outputs [][]byte
	for index, isolation := range isolations {
		output, err := runGitIsolated(ctx, repo, isolation, nil,
			"--no-pager", "-c", "core.attributesFile="+attributesFiles[index],
			"diff", "--raw", "-z", "--full-index", "--no-renames", emptyTree, tree, "--")
		if err != nil {
			t.Fatal(err)
		}
		outputs = append(outputs, output)
	}
	if *spawns != 1 {
		t.Fatalf("spawned Git %d times for one diff through two equivalent isolations, want 1", *spawns)
	}
	if !bytes.Equal(outputs[0], outputs[1]) || len(outputs[0]) == 0 {
		t.Fatalf("isolations answered differently: %q and %q", outputs[0], outputs[1])
	}
}

func TestGitMemoKeepsEachIsolationsOwnDirectory(t *testing.T) {
	repo := initSnapshotRepo(t)
	ctx := WithGitMemo(context.Background())
	var gitDirs []string
	for range 2 {
		isolation, cleanup, err := isolatedImmutableTreeGit(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cleanup() })
		output, err := runGitIsolated(ctx, repo, isolation, nil, "rev-parse", "--git-dir")
		if err != nil {
			t.Fatal(err)
		}
		gitDirs = append(gitDirs, strings.TrimSpace(string(output)))
	}
	if gitDirs[0] == gitDirs[1] {
		t.Fatalf("second isolation was told the first isolation's directory %q", gitDirs[0])
	}
}
