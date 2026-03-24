package agent

import (
	"context"

	"github.com/SciMate-AI/scicli/internal/history"
	"github.com/SciMate-AI/scicli/internal/llm/tools"
	"github.com/SciMate-AI/scicli/internal/lsp"
	"github.com/SciMate-AI/scicli/internal/message"
	"github.com/SciMate-AI/scicli/internal/permission"
	"github.com/SciMate-AI/scicli/internal/research"
	"github.com/SciMate-AI/scicli/internal/session"
	"github.com/SciMate-AI/scicli/internal/skills"
	"github.com/SciMate-AI/scicli/internal/taskrun"
)

func CoderAgentTools(
	permissions permission.Service,
	sessions session.Service,
	messages message.Service,
	history history.Service,
	lspClients map[string]*lsp.Client,
	skillsSvc skills.Service,
	taskRuns taskrun.Service,
	researchSvc research.Service,
) []tools.BaseTool {
	ctx := context.Background()
	otherTools := GetMcpTools(ctx, permissions)
	if len(lspClients) > 0 {
		otherTools = append(otherTools, tools.NewDiagnosticsTool(lspClients))
	}
	if skillsSvc != nil {
		otherTools = append(otherTools, NewActivateSkillTool(skillsSvc))
	}
	return append(
		[]tools.BaseTool{
			tools.NewBashTool(permissions),
			tools.NewEditTool(lspClients, permissions, history),
			tools.NewFetchTool(permissions),
			tools.NewGlobTool(),
			tools.NewGrepTool(),
			tools.NewLsTool(),
			tools.NewSourcegraphTool(),
			tools.NewViewTool(lspClients),
			tools.NewPatchTool(lspClients, permissions, history),
			tools.NewWriteTool(lspClients, permissions, history),
			NewAgentTool(permissions, sessions, messages, lspClients, skillsSvc, taskRuns, researchSvc),
		}, otherTools...,
	)
}

func TaskAgentTools(lspClients map[string]*lsp.Client, skillsSvc skills.Service) []tools.BaseTool {
	toolsList := []tools.BaseTool{
		tools.NewGlobTool(),
		tools.NewGrepTool(),
		tools.NewLsTool(),
		tools.NewSourcegraphTool(),
		tools.NewViewTool(lspClients),
	}
	if skillsSvc != nil {
		toolsList = append(toolsList, NewActivateSkillTool(skillsSvc))
	}
	return toolsList
}
