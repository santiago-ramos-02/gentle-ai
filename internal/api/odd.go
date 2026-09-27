package api

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// oddTasksDir is where ODD keeps a project's feature documents.
var oddTasksDir = filepath.Join("odd", "tasks")

var (
	oddCheckbox = regexp.MustCompile(`^\s*[-*]\s+\[([ xX])\]`)
	oddHeading  = regexp.MustCompile(`^#\s+(?:Feature:\s*)?(.+?)\s*$`)
	oddSection  = regexp.MustCompile(`^##\s+(.+?)\s*$`)
)

type oddFeature struct {
	// Name is the document's file name without .md, as ODD names the feature.
	Name string `json:"name"`
	// Path is relative to the project, with forward slashes.
	Path       string    `json:"path"`
	Title      string    `json:"title"`
	Objective  string    `json:"objective,omitempty"`
	TasksDone  int       `json:"tasksDone"`
	TasksTotal int       `json:"tasksTotal"`
	NextStep   string    `json:"nextStep,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type oddFeaturesResult struct {
	Features []oddFeature `json:"features"`
}

// oddFeatures lists a project's ODD feature documents, most recently changed
// first, with their task progress and next step, so a host can show work in
// flight and hand a feature to a new session.
func oddFeatures(_ context.Context, _ *env, params cwdParams) (any, error) {
	if err := requireCwd(params.Cwd); err != nil {
		return nil, err
	}
	dir := filepath.Join(params.Cwd, oddTasksDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return oddFeaturesResult{Features: []oddFeature{}}, nil
	}
	if err != nil {
		return nil, errorf(CodeFailed, "read %s: %v", dir, err)
	}
	features := []oddFeature{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		feature, err := readOddFeature(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		features = append(features, feature)
	}
	slices.SortFunc(features, func(a, b oddFeature) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return oddFeaturesResult{Features: features}, nil
}

func readOddFeature(path string) (oddFeature, error) {
	info, err := os.Stat(path)
	if err != nil {
		return oddFeature{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return oddFeature{}, err
	}
	defer file.Close()
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	feature := oddFeature{
		Name:      name,
		Path:      filepath.ToSlash(filepath.Join(oddTasksDir, filepath.Base(path))),
		Title:     name,
		UpdatedAt: info.ModTime().UTC(),
	}
	section := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if match := oddHeading.FindStringSubmatch(line); match != nil && section == "" && feature.Title == name {
			feature.Title = match[1]
			continue
		}
		if match := oddSection.FindStringSubmatch(line); match != nil {
			section = strings.ToLower(match[1])
			continue
		}
		if match := oddCheckbox.FindStringSubmatch(line); match != nil {
			feature.TasksTotal++
			if match[1] != " " {
				feature.TasksDone++
			}
			continue
		}
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		switch {
		case section == "objective" && feature.Objective == "":
			feature.Objective = text
		case strings.HasPrefix(section, "next step") && feature.NextStep == "":
			feature.NextStep = strings.TrimSpace(strings.TrimPrefix(text, "- "))
		}
	}
	return feature, scanner.Err()
}
