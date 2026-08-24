package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/registry"
	"github.com/athal7/agentcfg/internal/renderers"
)

// TestAllCapabilities_MatchesRendererGoConstCount is the tripwire promised
// by allCapabilities' doc comment: it parses internal/render/renderer.go's
// actual Capability const block and fails, naming the exact mismatch, if
// the hand-maintained allCapabilities list has drifted from it — a
// constant added, removed, or never listed in the first place — rather
// than doctor silently under- or over-reporting a capability forever.
func TestAllCapabilities_MatchesRendererGoConstCount(t *testing.T) {
	fset := token.NewFileSet()
	path := filepath.Join("..", "render", "renderer.go")
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var wantValues []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Only the block of constants explicitly typed `Capability`
			// counts — a future GapKind-style const block in the same
			// file must not be swept in here.
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != "Capability" {
				continue
			}
			for i := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquoting %s: %v", lit.Value, err)
				}
				wantValues = append(wantValues, val)
			}
		}
	}

	if len(wantValues) == 0 {
		t.Fatal("found zero Capability constants in renderer.go — parser or type-match logic is broken")
	}

	got := make(map[string]bool, len(allCapabilities))
	var duplicates []string
	for _, c := range allCapabilities {
		value := string(c)
		if got[value] {
			duplicates = append(duplicates, value)
		}
		got[value] = true
	}

	seen := make(map[string]bool, len(wantValues))
	var missing []string
	for _, v := range wantValues {
		seen[v] = true
		if !got[v] {
			missing = append(missing, v)
		}
	}
	var extra []string
	for v := range got {
		if !seen[v] {
			extra = append(extra, v)
		}
	}

	if len(missing) > 0 || len(extra) > 0 || len(duplicates) > 0 {
		sort.Strings(missing)
		sort.Strings(extra)
		sort.Strings(duplicates)
		t.Fatalf("allCapabilities is out of sync with renderer.go's Capability constants — missing: %v, extra (no longer a real constant): %v, duplicates: %v", missing, extra, duplicates)
	}
}

func TestPrintCapabilityMatrixMarkdown_GroupsCapabilities(t *testing.T) {
	var output strings.Builder
	printCapabilityMatrix(&output, renderers.All(), allCapabilities, true)
	got := output.String()

	previous := -1
	for _, heading := range []string{
		"## Agents",
		"## Permissions",
		"## Model bindings",
		"## Bash policies",
		"## MCP servers",
		"## Project policy",
		"## Commands",
	} {
		position := strings.Index(got, heading)
		if position < 0 {
			t.Errorf("Markdown capability matrix does not contain heading %q", heading)
		}
		if position <= previous {
			t.Errorf("Markdown capability heading %q is out of order", heading)
		}
		previous = position
	}

	if got, want := strings.Count(got, "\n| capability |"), len(capabilityGroups); got != want {
		t.Errorf("Markdown capability table count = %d, want %d", got, want)
	}
	if !strings.Contains(got, "## Equivalent capabilities\n\nAn `≈` indicates the same registry feature uses a different native mechanism.\n\n| harness | capability | expressed via |") {
		t.Error("Markdown capability matrix does not structure equivalent capabilities as a table")
	}
}

func TestPrintRegistryGapsMarkdown_GroupsGapsByHarness(t *testing.T) {
	registryRoot := filepath.Join("..", "..", "examples", "registry")
	reg, validationErrors, _, err := registry.Load(registryRoot)
	if err != nil {
		t.Fatalf("loading example registry: %v", err)
	}
	if len(validationErrors) > 0 {
		t.Fatalf("example registry has validation errors: %v", validationErrors)
	}

	var output strings.Builder
	printRegistryGaps(&output, renderers.All(), reg, true)
	got := output.String()

	for _, heading := range []string{"## Registry gaps", "### claude", "### codex", "### opencode", "### omp"} {
		if !strings.Contains(got, heading) {
			t.Errorf("Markdown registry gaps does not contain heading %q", heading)
		}
	}
	if !strings.Contains(got, "- **Skipped `per_agent_bash_policy` for `lead`**.") {
		t.Error("Markdown registry gaps does not render a structured gap item")
	}
}
