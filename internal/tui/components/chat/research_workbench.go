package chat

import (
	"strings"

	"github.com/SciMate-AI/scicli/internal/research"
)

type researchWorkbenchAction struct {
	ID        string
	Label     string
	CommandID string
	Command   string
	ArgNames  []string
	Enabled   bool
	Reason    string
}

func promotedExperiment(state research.SessionState) (research.ExperimentPlan, bool) {
	if strings.TrimSpace(state.PromotedExperimentID) == "" {
		return research.ExperimentPlan{}, false
	}
	for _, plan := range state.Experiments {
		if plan.ID == state.PromotedExperimentID {
			return plan, true
		}
	}
	return research.ExperimentPlan{}, false
}

func experimentFlags(state research.SessionState, plan research.ExperimentPlan) []string {
	flags := make([]string, 0, 3)
	if plan.ID == state.ActiveExperimentID {
		flags = append(flags, "ACTIVE")
	}
	if plan.ID == state.PromotedExperimentID {
		flags = append(flags, "PROMOTED")
	}
	if plan.EvolutionDecision != "" {
		flags = append(flags, strings.ToUpper(string(plan.EvolutionDecision)))
	}
	return flags
}

func queuedExperiments(state research.SessionState) []research.ExperimentPlan {
	out := make([]research.ExperimentPlan, 0, len(state.Experiments))
	for _, plan := range state.Experiments {
		if plan.ID == state.ActiveExperimentID || plan.ID == state.PromotedExperimentID {
			continue
		}
		if plan.Status == research.ExperimentPlanned || plan.Status == research.ExperimentRunning {
			out = append(out, plan)
			continue
		}
		if plan.LatestEval == nil {
			out = append(out, plan)
			continue
		}
		if plan.LatestEval.Decision == research.DecisionMutate || plan.LatestEval.Decision == research.DecisionBranch {
			out = append(out, plan)
		}
	}
	return out
}

func nextResearchAction(state research.SessionState) string {
	if strings.TrimSpace(state.Objective) == "" {
		return "/research set <objective>"
	}
	if len(state.Experiments) == 0 {
		return "/experiment add <title>"
	}

	active, ok := activeExperiment(state)
	if ok {
		switch {
		case active.LatestEval != nil && (active.LatestEval.Decision == research.DecisionMutate || active.LatestEval.Decision == research.DecisionBranch):
			return "/experiment evolve [title]"
		case active.LatestEval == nil && len(active.Runs) == 0:
			return "Run the active candidate in chat or a delegated task"
		case active.LatestEval == nil && active.Status == research.ExperimentRunning:
			return "Monitor the active run in /tasks or the inspector"
		case active.LatestEval == nil:
			return "/experiment evaluate <score> <keep|discard|mutate|branch> <summary>"
		}
	}

	if promoted, ok := promotedExperiment(state); ok && promoted.LatestEval != nil {
		if promoted.LatestEval.Decision == research.DecisionMutate || promoted.LatestEval.Decision == research.DecisionBranch {
			return "/experiment evolve [title]"
		}
	}

	for _, plan := range state.Experiments {
		if plan.LatestEval != nil && plan.LatestEval.Decision != research.DecisionDiscard {
			return "/experiment promote " + shortResearchID(plan.ID)
		}
	}

	if len(research.ArtifactIndex(state)) > 0 {
		return "/artifact list"
	}
	return "/experiment compare"
}

func lineageContainsExperiment(group research.LineageGroup, experimentID string) bool {
	experimentID = strings.TrimSpace(experimentID)
	if experimentID == "" {
		return false
	}
	for _, plan := range group.Plans {
		if plan.ID == experimentID {
			return true
		}
	}
	return false
}

func bestExperimentInLineage(group research.LineageGroup) (research.ExperimentPlan, bool) {
	var best research.ExperimentPlan
	found := false
	for _, plan := range group.Plans {
		if plan.LatestEval == nil {
			continue
		}
		if !found || plan.LatestEval.Score > best.LatestEval.Score || (plan.LatestEval.Score == best.LatestEval.Score && plan.Generation > best.Generation) {
			best = plan
			found = true
		}
	}
	if found {
		return best, true
	}
	if len(group.Plans) == 0 {
		return research.ExperimentPlan{}, false
	}
	return group.Plans[len(group.Plans)-1], true
}

func currentExperimentInLineage(state research.SessionState, group research.LineageGroup) (research.ExperimentPlan, bool) {
	if lineageContainsExperiment(group, state.ActiveExperimentID) {
		for _, plan := range group.Plans {
			if plan.ID == state.ActiveExperimentID {
				return plan, true
			}
		}
	}
	if lineageContainsExperiment(group, state.PromotedExperimentID) {
		for _, plan := range group.Plans {
			if plan.ID == state.PromotedExperimentID {
				return plan, true
			}
		}
	}
	if len(group.Plans) == 0 {
		return research.ExperimentPlan{}, false
	}
	return group.Plans[len(group.Plans)-1], true
}

func queuedExperimentCount(group research.LineageGroup) int {
	total := 0
	for _, plan := range group.Plans {
		if plan.Status == research.ExperimentPlanned || plan.Status == research.ExperimentRunning || plan.LatestEval == nil {
			total++
		}
	}
	return total
}

func lineageActionHint(state research.SessionState, group research.LineageGroup) string {
	if current, ok := currentExperimentInLineage(state, group); ok {
		switch {
		case current.ID == state.ActiveExperimentID && current.LatestEval != nil && (current.LatestEval.Decision == research.DecisionMutate || current.LatestEval.Decision == research.DecisionBranch):
			return "Next /experiment evolve [title]"
		case current.ID == state.ActiveExperimentID && current.LatestEval == nil && len(current.Runs) == 0:
			return "Next run the active candidate"
		case current.ID == state.ActiveExperimentID && current.LatestEval == nil && current.Status == research.ExperimentRunning:
			return "Run in progress"
		case current.ID == state.ActiveExperimentID && current.LatestEval == nil:
			return "Next /experiment evaluate <score> <decision> <summary>"
		}
	}

	if best, ok := bestExperimentInLineage(group); ok && best.LatestEval != nil {
		if state.PromotedExperimentID == "" && best.LatestEval.Decision != research.DecisionDiscard {
			return "Next /experiment promote " + shortResearchID(best.ID)
		}
		if best.LatestEval.Decision == research.DecisionMutate || best.LatestEval.Decision == research.DecisionBranch {
			return "Next /experiment evolve [title]"
		}
	}

	if queuedExperimentCount(group) > 0 {
		return "Pending queued candidates"
	}
	return "Review in /experiment compare"
}

func researchWorkbenchActions(state research.SessionState) []researchWorkbenchAction {
	actions := []researchWorkbenchAction{
		{
			ID:        "evaluate",
			Label:     "Evaluate",
			CommandID: "evaluate-active-experiment",
			Command:   "/experiment evaluate $score $decision $summary",
			ArgNames:  []string{"score", "decision", "summary"},
			Enabled:   false,
			Reason:    "No active experiment. Add or activate a candidate first.",
		},
		{
			ID:        "promote",
			Label:     "Promote",
			CommandID: "promote-active-experiment",
			Command:   "/experiment promote",
			Enabled:   false,
			Reason:    "No active experiment. Add or activate a candidate first.",
		},
		{
			ID:        "evolve",
			Label:     "Evolve",
			CommandID: "evolve-active-experiment",
			Command:   "/experiment evolve",
			Enabled:   false,
			Reason:    "No active experiment. Add or activate a candidate first.",
		},
		{
			ID:        "compare",
			Label:     "Compare",
			CommandID: "compare-experiments",
			Command:   "/experiment compare",
			Enabled:   false,
			Reason:    "Need at least two experiments to compare.",
		},
	}

	active, hasActive := activeExperiment(state)
	for i := range actions {
		switch actions[i].ID {
		case "evaluate":
			switch {
			case !hasActive:
			case active.LatestEval != nil:
				actions[i].Reason = "The active experiment is already evaluated."
			case len(active.Runs) == 0:
				actions[i].Reason = "Run the active candidate before evaluating it."
			case active.Status == research.ExperimentRunning:
				actions[i].Reason = "Wait for the active run to finish before evaluating it."
			default:
				actions[i].Enabled = true
				actions[i].Reason = ""
			}
		case "promote":
			switch {
			case !hasActive:
			case active.ID == state.PromotedExperimentID:
				actions[i].Reason = "The active experiment is already promoted."
			case active.LatestEval == nil:
				actions[i].Reason = "Evaluate the active experiment before promoting it."
			case active.LatestEval.Decision == research.DecisionDiscard:
				actions[i].Reason = "Discarded experiments cannot be promoted."
			default:
				actions[i].Enabled = true
				actions[i].Reason = ""
			}
		case "evolve":
			switch {
			case !hasActive:
			case active.LatestEval == nil:
				actions[i].Reason = "Evaluate the active experiment before evolving it."
			case active.LatestEval.Decision != research.DecisionMutate && active.LatestEval.Decision != research.DecisionBranch:
				actions[i].Reason = "Only mutate/branch decisions can create the next generation."
			default:
				actions[i].Enabled = true
				actions[i].Reason = ""
			}
		case "compare":
			if len(state.Experiments) >= 2 {
				actions[i].Enabled = true
				actions[i].Reason = ""
			}
		}
	}
	return actions
}
