package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
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
