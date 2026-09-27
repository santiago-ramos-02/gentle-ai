package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOddFeaturesListsDocumentsWithProgress(t *testing.T) {
	deps := testDeps(t)
	cwd := t.TempDir()
	if got := result[oddFeaturesResult](t, deps, "odd.features", `{"cwd":`+quote(cwd)+`}`); len(got.Features) != 0 {
		t.Fatalf("a project without odd/tasks has no features: %+v", got)
	}

	dir := filepath.Join(cwd, "odd", "tasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string, age time.Duration) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		when := time.Now().Add(-age)
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	write("todo-due-dates.md", `# Feature: Todo due dates

## Objective

Users can set a due date on a todo and see which todos are overdue.

## Tasks

- [x] **T1 — Store: accept dueDate.**
- [X] **T2 — Store: set or clear it.**
- [ ] **T3 — Overdue derivation.**

## Next step

Start T3 on the feature branch.
`, time.Minute)
	write("older.md", "# Older\n\n- [ ] only task\n", time.Hour)
	write("notes.txt", "not a feature", 0)

	got := result[oddFeaturesResult](t, deps, "odd.features", `{"cwd":`+quote(cwd)+`}`)
	if len(got.Features) != 2 {
		t.Fatalf("features = %+v", got.Features)
	}
	first := got.Features[0]
	if first.Name != "todo-due-dates" || first.Path != "odd/tasks/todo-due-dates.md" || first.Title != "Todo due dates" ||
		first.Objective != "Users can set a due date on a todo and see which todos are overdue." ||
		first.TasksDone != 2 || first.TasksTotal != 3 || first.NextStep != "Start T3 on the feature branch." {
		t.Fatalf("most recent feature = %+v", first)
	}
	if got.Features[1].Name != "older" || got.Features[1].TasksTotal != 1 {
		t.Fatalf("older feature = %+v", got.Features[1])
	}
	failure(t, deps, []string{"odd.features"}, `{}`, CodeInvalidParams)
}
