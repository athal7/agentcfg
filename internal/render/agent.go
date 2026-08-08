package render

import "github.com/athal7/agentcfg/internal/registry"

// PrimaryAgent returns the registry's role:primary agent, or nil if none is
// set (0 primary agents is valid; registry.Validate rejects only >1).
func PrimaryAgent(reg *registry.Registry) *registry.Agent {
	for i := range reg.Agents {
		if reg.Agents[i].Role == "primary" {
			return &reg.Agents[i]
		}
	}
	return nil
}
