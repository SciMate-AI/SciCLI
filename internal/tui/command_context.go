package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/tui/components/dialog"
)

type researchCommandContext struct {
	sessionID     string
	state         research.SessionState
	artifactCount int
	active        research.ExperimentPlan
	hasActive     bool
}

func (a *appModel) contextualCommands() []dialog.Command {
	ctx := a.loadResearchCommandContext(context.Background())
	commands := make([]dialog.Command, 0, len(a.commands))
	for _, cmd := range a.commands {
		commands = append(commands, contextualizeCommand(cmd, ctx))
	}
	slices.SortFunc(commands, compareContextualCommands)
	return commands
}

func (a *appModel) loadResearchCommandContext(ctx context.Context) researchCommandContext {
	if a.app == nil || a.app.Research == nil || a.selectedSession.ID == "" {
		return researchCommandContext{}
	}

	researchSession, err := resolveResearchCommandSession(ctx, a.app.Sessions, a.selectedSession)
	if err != nil {
		return researchCommandContext{}
	}

	state, err := a.app.Research.Get(ctx, researchSession.ID)
	if err != nil {
		state = research.SessionState{SessionID: researchSession.ID, Stage: research.StageObjective}
	}

	active, ok := activeResearchCommandExperiment(state)
	return researchCommandContext{
		sessionID:     researchSession.ID,
		state:         state,
		artifactCount: len(research.ArtifactIndex(state)),
		active:        active,
		hasActive:     ok,
	}
}

func contextualizeCommand(cmd dialog.Command, ctx researchCommandContext) dialog.Command {
	cmd.Boost = 0
	cmd.Recommended = false
	cmd.Disabled = false
	cmd.Reason = ""

	switch cmd.ID {
	case "set-research-objective":
		if strings.TrimSpace(ctx.state.Objective) == "" {
			cmd.Recommended = true
			cmd.Boost = 320
			cmd.Description = "No research objective yet. Start the loop here."
		} else {
			cmd.Description = "Current objective: " + truncateCommandText(ctx.state.Objective, 72)
		}
	case "show-research-state":
		if !ctx.state.HasContent() {
			cmd.Description = "No persisted research state yet. Set an objective to start."
		} else {
			cmd.Description = fmt.Sprintf(
				"Stage %s | %d experiments | %d artifacts",
				strings.ToUpper(string(ctx.state.Stage)),
				len(ctx.state.Experiments),
				ctx.artifactCount,
			)
		}
	case "add-experiment-plan":
		if strings.TrimSpace(ctx.state.Objective) == "" {
			cmd.Disabled = true
			cmd.Reason = "Set a research objective first."
		} else if len(ctx.state.Experiments) == 0 {
			cmd.Recommended = true
			cmd.Boost = 280
			cmd.Description = "Objective set. Create the first experiment candidate."
		} else {
			cmd.Description = fmt.Sprintf("Current queue has %d experiment candidates.", len(ctx.state.Experiments))
		}
	case "evaluate-active-experiment":
		if !ctx.hasActive {
			cmd.Disabled = true
			cmd.Reason = "No active experiment. Add or activate a candidate first."
		} else if ctx.active.LatestEval != nil {
			cmd.Disabled = true
			cmd.Reason = "The active experiment is already evaluated."
			cmd.Description = evaluationSummary(ctx.active)
		} else if len(ctx.active.Runs) == 0 {
			cmd.Disabled = true
			cmd.Reason = "Run the active candidate before evaluating it."
		} else if ctx.active.Status == research.ExperimentRunning {
			cmd.Disabled = true
			cmd.Reason = "Wait for the active run to finish before evaluating it."
		} else {
			cmd.Recommended = true
			cmd.Boost = 300
			cmd.Description = fmt.Sprintf("Evaluate %s after %d run(s).", shortCommandResearchID(ctx.active.ID), len(ctx.active.Runs))
		}
	case "promote-active-experiment":
		if !ctx.hasActive {
			cmd.Disabled = true
			cmd.Reason = "No active experiment. Add or activate a candidate first."
		} else if ctx.active.ID == ctx.state.PromotedExperimentID {
			cmd.Disabled = true
			cmd.Reason = "The active experiment is already promoted."
			cmd.Description = evaluationSummary(ctx.active)
		} else if ctx.active.LatestEval == nil {
			cmd.Disabled = true
			cmd.Reason = "Evaluate the active experiment before promoting it."
		} else if ctx.active.LatestEval.Decision == research.DecisionDiscard {
			cmd.Disabled = true
			cmd.Reason = "Discarded experiments cannot be promoted."
			cmd.Description = evaluationSummary(ctx.active)
		} else {
			cmd.Recommended = true
			cmd.Boost = 340
			cmd.Description = "Promote " + shortCommandResearchID(ctx.active.ID) + " as the current best candidate. " + evaluationSummary(ctx.active)
		}
	case "propose-next-experiment":
		if strings.TrimSpace(ctx.state.Objective) == "" {
			cmd.Disabled = true
			cmd.Reason = "Set a research objective before asking for a proposed next experiment."
		} else if len(ctx.state.Experiments) == 0 {
			cmd.Disabled = true
			cmd.Reason = "Create or run at least one experiment first."
		} else {
			cmd.Description = fmt.Sprintf("Use %d prior experiments to suggest the next candidate.", len(ctx.state.Experiments))
			cmd.Boost = 120
		}
	case "evolve-active-experiment":
		if !ctx.hasActive {
			cmd.Disabled = true
			cmd.Reason = "No active experiment. Add or activate a candidate first."
		} else if ctx.active.LatestEval == nil {
			cmd.Disabled = true
			cmd.Reason = "Evaluate the active experiment before evolving it."
		} else if ctx.active.LatestEval.Decision != research.DecisionMutate && ctx.active.LatestEval.Decision != research.DecisionBranch {
			cmd.Disabled = true
			cmd.Reason = fmt.Sprintf("The active decision is %s. Use rerun or promote instead.", strings.ToUpper(string(ctx.active.LatestEval.Decision)))
			cmd.Description = evaluationSummary(ctx.active)
		} else {
			cmd.Recommended = true
			cmd.Boost = 360
			cmd.Description = "Create the next generation from " + shortCommandResearchID(ctx.active.ID) + ". " + evaluationSummary(ctx.active)
		}
	case "rerun-active-experiment":
		if !ctx.hasActive {
			cmd.Disabled = true
			cmd.Reason = "No active experiment. Add or activate a candidate first."
		} else {
			cmd.Description = "Clone " + shortCommandResearchID(ctx.active.ID) + " into a fresh rerun candidate."
			cmd.Boost = 80
		}
	case "compare-experiments":
		if len(ctx.state.Experiments) < 2 {
			cmd.Disabled = true
			cmd.Reason = "Need at least two experiments to compare lineage and outcomes."
		} else {
			cmd.Description = fmt.Sprintf("Compare %d experiments across the current lineage board.", len(ctx.state.Experiments))
			cmd.Boost = 140
		}
	case "list-experiment-artifacts":
		if ctx.artifactCount == 0 {
			cmd.Disabled = true
			cmd.Reason = "No captured artifacts yet. Finish a delegated run first."
		} else {
			cmd.Description = fmt.Sprintf("Inspect %d captured provenance artifacts.", ctx.artifactCount)
			cmd.Boost = 160
		}
	}

	return cmd
}

func compareContextualCommands(a, b dialog.Command) int {
	if a.Recommended != b.Recommended {
		if a.Recommended {
			return -1
		}
		return 1
	}
	if a.Disabled != b.Disabled {
		if !a.Disabled {
			return -1
		}
		return 1
	}
	if a.Boost != b.Boost {
		if a.Boost > b.Boost {
			return -1
		}
		return 1
	}
	return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
}

func resolveResearchCommandSession(ctx context.Context, sessions session.Service, current session.Session) (session.Session, error) {
	for current.ParentSessionID != "" {
		parent, err := sessions.Get(ctx, current.ParentSessionID)
		if err != nil {
			return session.Session{}, err
		}
		current = parent
	}
	return current, nil
}

func activeResearchCommandExperiment(state research.SessionState) (research.ExperimentPlan, bool) {
	if strings.TrimSpace(state.ActiveExperimentID) != "" {
		for _, plan := range state.Experiments {
			if plan.ID == state.ActiveExperimentID {
				return plan, true
			}
		}
	}
	if len(state.Experiments) == 0 {
		return research.ExperimentPlan{}, false
	}
	return state.Experiments[0], true
}

func shortCommandResearchID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func truncateCommandText(text string, width int) string {
	text = strings.TrimSpace(text)
	if width <= 0 || len(text) <= width {
		return text
	}
	if width <= 3 {
		return text[:width]
	}
	return text[:width-3] + "..."
}

func evaluationSummary(plan research.ExperimentPlan) string {
	if plan.LatestEval == nil {
		return "No evaluation recorded yet."
	}
	return fmt.Sprintf("Score %.2f %s.", plan.LatestEval.Score, strings.ToUpper(string(plan.LatestEval.Decision)))
}
