// Package renderers is the single place that wires concrete renderer
// implementations (opencode, omp, ...) into the render package's Renderer
// interface. It exists purely to avoid an import cycle: concrete renderer
// packages import internal/render for the contract types, so the contract
// package itself can't also import the concrete renderers back.
package renderers

import (
	"github.com/athal7/agentcfg/internal/render"
	"github.com/athal7/agentcfg/internal/render/codex"
	"github.com/athal7/agentcfg/internal/render/omp"
	"github.com/athal7/agentcfg/internal/render/opencode"
)

// All is the list of every built-in renderer. Adding a renderer means
// adding exactly one line here — nothing else changes.
func All() []render.Renderer {
	return []render.Renderer{
		codex.New(),
		opencode.New(),
		omp.New(),
	}
}
