package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"

	"github.com/athal7/agentcfg/internal/render"
)

// declaredCapabilityValues parses internal/render/renderer.go's AST and
// returns the string value of every Capability constant declared there,
// in source order. This is a real parse of the source, not a second
// hand-maintained list — it's the only way to make the tripwire
// self-verifying instead of one hardcoded list checked against another.
func declaredCapabilityValues(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "../render/renderer.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing internal/render/renderer.go: %v", err)
	}

	var values []string
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
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != "Capability" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquoting Capability literal %s: %v", lit.Value, err)
				}
				values = append(values, unquoted)
			}
		}
	}
	return values
}

// TestAllCapabilities_MatchesRendererGoConstCount is the tripwire
// doctor.go's own comment promises: it fails loudly, naming the exact
// mismatch, whenever allCapabilities falls out of sync with the actual
// Capability constants declared in internal/render/renderer.go — instead
// of doctor silently under-reporting a new capability forever (as
// happened in practice: mcp_tool_allowlist was added to renderer.go
// without this list, and nothing caught it because this test didn't
// exist yet despite the comment above allCapabilities promising it did).
func TestAllCapabilities_MatchesRendererGoConstCount(t *testing.T) {
	declared := declaredCapabilityValues(t)

	listed := make([]string, len(allCapabilities))
	for i, c := range allCapabilities {
		listed[i] = string(c)
	}

	declaredSorted := append([]string{}, declared...)
	listedSorted := append([]string{}, listed...)
	sort.Strings(declaredSorted)
	sort.Strings(listedSorted)

	if len(declaredSorted) != len(listedSorted) {
		t.Fatalf("renderer.go declares %d Capability constants, allCapabilities (doctor.go) has %d entries: declared=%v listed=%v",
			len(declaredSorted), len(listedSorted), declaredSorted, listedSorted)
	}
	for i := range declaredSorted {
		if declaredSorted[i] != listedSorted[i] {
			t.Fatalf("mismatch at sorted index %d: renderer.go declares %q, allCapabilities lacks it or has an extra %q\ndeclared=%v\nlisted=  %v",
				i, declaredSorted[i], listedSorted[i], declaredSorted, listedSorted)
		}
	}
}

// TestAllCapabilities_NoDuplicates guards against a copy-paste duplicate
// entry silently shrinking the effective coverage of allCapabilities
// while its len() still happens to match renderer.go's declared count.
func TestAllCapabilities_NoDuplicates(t *testing.T) {
	seen := make(map[render.Capability]bool, len(allCapabilities))
	for _, c := range allCapabilities {
		if seen[c] {
			t.Errorf("duplicate entry in allCapabilities: %q", c)
		}
		seen[c] = true
	}
}
