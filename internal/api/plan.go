package api

import (
	"context"

	"github.com/gentleman-programming/gentle-ai/v4/internal/cli"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/planner"
	"github.com/gentleman-programming/gentle-ai/v4/internal/service"
	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
)

// selectionParams is the installer selection. Omitted components and skills
// derive from the preset exactly as the installer derives them.
type selectionParams struct {
	Agents     []string `json:"agents"`
	Components []string `json:"components"`
	Skills     []string `json:"skills"`
	Persona    string   `json:"persona"`
	Preset     string   `json:"preset"`
}

// toSelection validates the selection through the same normalization the
// install command uses, then applies the installer's Pi-only rule.
func (p selectionParams) toSelection(detection system.DetectionResult) (model.Selection, error) {
	if len(p.Agents) == 0 {
		return model.Selection{}, invalidParams("selection.agents must name at least one agent")
	}
	input, err := cli.NormalizeInstallFlags(cli.InstallFlags{
		Agents:     p.Agents,
		Components: p.Components,
		Skills:     p.Skills,
		Persona:    p.Persona,
		Preset:     p.Preset,
	}, detection)
	if err != nil {
		return model.Selection{}, invalidParams("selection: %v", err)
	}
	selection := input.Selection
	if p.Components == nil && service.IsPiOnlyAgents(selection.Agents) {
		selection.Components = service.PiOnlyComponents()
	}
	return selection, nil
}

func resolvePlan(selection model.Selection) (planner.ResolvedPlan, error) {
	resolved, err := planner.NewResolver(planner.MVPGraph()).Resolve(selection)
	if err != nil {
		return planner.ResolvedPlan{}, invalidParams("selection: %v", err)
	}
	return resolved, nil
}

type planParams struct {
	Selection *selectionParams `json:"selection"`
}

type planResult struct {
	Agents            []string   `json:"agents"`
	UnsupportedAgents []string   `json:"unsupportedAgents"`
	Components        []string   `json:"components"`
	AddedDependencies []string   `json:"addedDependencies"`
	Steps             []planStep `json:"steps"`
	Questions         []string   `json:"questions"`
}

type planStep struct {
	ID string `json:"id"`
}

func plan(ctx context.Context, env *env, params planParams) (any, error) {
	if params.Selection == nil {
		return nil, invalidParams("selection is required")
	}
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	selection, err := params.Selection.toSelection(detection)
	if err != nil {
		return nil, err
	}
	resolved, err := resolvePlan(selection)
	if err != nil {
		return nil, err
	}
	current, err := readState(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	questions, err := service.InstallQuestions(selection, current.BackgroundIntent, current.PiBackgroundIntent)
	if err != nil {
		return nil, errorf(CodeFailed, "resolve installer questions: %v", err)
	}
	stepIDs := service.InstallStepIDs(resolved, nil)
	steps := make([]planStep, 0, len(stepIDs))
	for _, id := range stepIDs {
		steps = append(steps, planStep{ID: id})
	}
	return planResult{
		Agents:            strs(resolved.Agents),
		UnsupportedAgents: strs(resolved.UnsupportedAgents),
		Components:        strs(resolved.OrderedComponents),
		AddedDependencies: strs(resolved.AddedDependencies),
		Steps:             steps,
		Questions:         strs(questions),
	}, nil
}
