package tools

import (
	"context"
	"fmt"

	"github.com/mikus/maiku/agent"
	"github.com/mikus/maiku/ai"
)

var miruSchema = []byte(`{"type":"object","properties":{"query":{"type":"string"},"path":{"type":"string"},"limit":{"type":"number"}},"required":["query"]}`)

func CreateMiruTool(cwd string) *agent.AgentTool {
	return CreateMiruToolWithService(NewMiruService(cwd))
}

func CreateMiruToolWithService(service MiruService) *agent.AgentTool {
	return &agent.AgentTool{
		Tool:  ai.Tool{Name: string(ToolMiru), Description: "Search repository code by meaning using bundled Miru.", Parameters: miruSchema},
		Label: string(ToolMiru),
		Execute: func(ctx context.Context, _ string, params map[string]any, _ agent.AgentToolUpdateCallback) (agent.AgentToolResult, error) {
			if err := checkAborted(ctx); err != nil {
				return agent.AgentToolResult{}, err
			}
			limit := 10
			if value := argIntPtr(params, "limit"); value != nil {
				limit = *value
			}
			if limit < 1 {
				limit = 10
			}
			query, _ := argString(params, "query")
			response, err := service.Search(ctx, MiruRequest{Query: query, Path: argStringOr(params, "path", "."), Limit: limit})
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("miru: %w", err)
			}
			encoded, err := encodeMiruResponse(response)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			return agent.AgentToolResult{Content: []ai.ToolResultContent{{Type: "text", Text: encoded}}}, nil
		},
	}
}
