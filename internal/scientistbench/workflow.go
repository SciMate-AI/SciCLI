package scientistbench

import "strings"

type workflowPhaseTemplate struct {
	ID    string
	Label string
	Nodes []string
}

func defaultWorkflowPhaseTemplates() []workflowPhaseTemplate {
	return []workflowPhaseTemplate{
		{ID: "planning", Label: "Planning", Nodes: []string{"node-case-intake"}},
		{ID: "research", Label: "Research", Nodes: []string{"node-research-plan", "node-corpus-retrieval"}},
		{ID: "ideation", Label: "Ideation", Nodes: []string{"node-idea-gate"}},
		{ID: "method", Label: "Method", Nodes: []string{"node-method-plan"}},
		{ID: "implementation", Label: "Implementation", Nodes: []string{"node-implementation", "node-execution", "node-analysis-figures"}},
		{ID: "paper_writing", Label: "Paper Writing", Nodes: []string{"node-paper-draft"}},
		{ID: "peer_review", Label: "Peer Review", Nodes: []string{"node-advisor-review", "node-judge-review", "node-domain-review", "node-paper-compare", "node-revision-gate"}},
		{ID: "aggregation", Label: "Aggregation", Nodes: []string{"node-aggregate"}},
	}
}

func SyncWorkflowState(item Case) Case {
	templates := defaultWorkflowPhaseTemplates()
	completedSet := make(map[string]struct{}, len(item.GraphState.CompletedNodes))
	for _, nodeID := range item.GraphState.CompletedNodes {
		completedSet[strings.TrimSpace(nodeID)] = struct{}{}
	}
	blockedSet := make(map[string]struct{}, len(item.GraphState.BlockedNodes))
	for _, nodeID := range item.GraphState.BlockedNodes {
		blockedSet[strings.TrimSpace(nodeID)] = struct{}{}
	}

	currentPhase := workflowPhaseIDForNode(item.GraphState.ActiveNode)
	phases := make([]WorkflowPhase, 0, len(templates))
	currentIndex := workflowPhaseIndex(currentPhase)
	isTerminal := item.Termination.Signal != ""

	for idx, template := range templates {
		startedAt, finishedAt, updates := workflowPhaseRunWindow(item.Runs, template.Nodes)
		allCompleted := workflowPhaseAllNodesCompleted(template.Nodes, completedSet)
		hasBlocked := workflowPhaseHasBlockedNode(template.Nodes, blockedSet)
		status := WorkflowPhasePending
		switch {
		case currentPhase == template.ID && !isTerminal:
			status = WorkflowPhaseActive
		case hasBlocked && currentPhase == template.ID:
			status = WorkflowPhaseBlocked
		case allCompleted:
			status = WorkflowPhaseCompleted
		case currentIndex >= 0 && idx < currentIndex && startedAt > 0 && !hasBlocked:
			status = WorkflowPhaseCompleted
		case hasBlocked:
			status = WorkflowPhaseBlocked
		}
		if status != WorkflowPhaseCompleted {
			finishedAt = 0
		}
		activeNode := ""
		if currentPhase == template.ID {
			activeNode = strings.TrimSpace(item.GraphState.ActiveNode)
		}
		phases = append(phases, WorkflowPhase{
			ID:                  template.ID,
			Label:               template.Label,
			Nodes:               append([]string(nil), template.Nodes...),
			Status:              status,
			ActiveNode:          activeNode,
			AppliedStateUpdates: updates,
			StartedAt:           startedAt,
			FinishedAt:          finishedAt,
		})
	}

	item.Workflow = WorkflowState{
		CurrentPhase: currentPhase,
		Phases:       phases,
	}
	return item
}

func workflowPhaseIDForNode(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ""
	}
	for _, template := range defaultWorkflowPhaseTemplates() {
		for _, candidate := range template.Nodes {
			if candidate == nodeID {
				return template.ID
			}
		}
	}
	return ""
}

func workflowPhaseIndex(phaseID string) int {
	for i, template := range defaultWorkflowPhaseTemplates() {
		if template.ID == phaseID {
			return i
		}
	}
	return -1
}

func workflowPhaseAllNodesCompleted(nodeIDs []string, completed map[string]struct{}) bool {
	if len(nodeIDs) == 0 {
		return false
	}
	for _, nodeID := range nodeIDs {
		if _, ok := completed[nodeID]; !ok {
			return false
		}
	}
	return true
}

func workflowPhaseHasBlockedNode(nodeIDs []string, blocked map[string]struct{}) bool {
	for _, nodeID := range nodeIDs {
		if _, ok := blocked[nodeID]; ok {
			return true
		}
	}
	return false
}

func workflowPhaseRunWindow(runs []RunRecord, nodeIDs []string) (int64, int64, []string) {
	nodeSet := make(map[string]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeSet[nodeID] = struct{}{}
	}
	var startedAt int64
	var finishedAt int64
	updates := []string{}
	for _, run := range runs {
		if _, ok := nodeSet[run.NodeID]; !ok {
			continue
		}
		if run.StartedAt > 0 && (startedAt == 0 || run.StartedAt < startedAt) {
			startedAt = run.StartedAt
		}
		if run.FinishedAt > 0 && run.FinishedAt > finishedAt {
			finishedAt = run.FinishedAt
		}
		updates = append(updates, run.StateUpdates...)
	}
	return startedAt, finishedAt, normalizeStrings(updates)
}
