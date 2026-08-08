package ai

import (
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidateToolArguments validates tool-call args against the tool's JSON Schema.
// Mirrors pi-ai validateToolArguments.
func ValidateToolArguments(tool Tool, args map[string]any) (map[string]any, error) {
	if len(tool.Parameters) == 0 {
		return args, nil
	}
	var schemaDoc any
	if err := json.Unmarshal(tool.Parameters, &schemaDoc); err != nil {
		return nil, fmt.Errorf("invalid tool schema for %s: %w", tool.Name, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", schemaDoc); err != nil {
		return nil, fmt.Errorf("compile schema for %s: %w", tool.Name, err)
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("compile schema for %s: %w", tool.Name, err)
	}
	if err := sch.Validate(args); err != nil {
		return nil, fmt.Errorf("invalid arguments for tool %s: %w", tool.Name, err)
	}
	return args, nil
}

// CalculateCost computes dollar cost from usage and model rates.
func CalculateCost(cost ModelCost, usage Usage) CostBreakdown {
	rates := cost.ModelCostRates
	if len(cost.Tiers) > 0 {
		for _, t := range cost.Tiers {
			if usage.Input > t.InputTokensAbove {
				rates = t.ModelCostRates
			}
		}
	}
	input := float64(usage.Input) * rates.Input / 1_000_000
	output := float64(usage.Output) * rates.Output / 1_000_000
	cacheRead := float64(usage.CacheRead) * rates.CacheRead / 1_000_000
	cacheWrite := float64(usage.CacheWrite) * rates.CacheWrite / 1_000_000
	return CostBreakdown{
		Input:      input,
		Output:     output,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
		Total:      input + output + cacheRead + cacheWrite,
	}
}
