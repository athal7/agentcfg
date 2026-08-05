package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/render"
)

func TestPrintPlanJSON_EmptySlicesAreArrays(t *testing.T) {
	plans := []targetPlan{
		{Scope: "global", Target: "opencode", Plan: &render.Plan{
			Outputs: nil,
			Gaps:    nil,
		}},
	}

	var buf bytes.Buffer
	if err := printPlanJSON(&buf, plans); err != nil {
		t.Fatalf("printPlanJSON: %v", err)
	}

	// Parse the JSON to verify structure.
	var report jsonPlanReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(report.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(report.Targets))
	}

	// Outputs and Gaps must be empty arrays, not null.
	if report.Targets[0].Outputs == nil {
		t.Error("Outputs is nil, want empty array []")
	}
	if report.Targets[0].Gaps == nil {
		t.Error("Gaps is nil, want empty array []")
	}

	// Also verify the raw JSON contains "[]" not "null".
	raw := buf.String()
	if strings.Contains(raw, `"outputs":null`) {
		t.Error(`raw JSON contains "outputs":null, want "outputs":[]`)
	}
	if strings.Contains(raw, `"gaps":null`) {
		t.Error(`raw JSON contains "gaps":null, want "gaps":[]`)
	}
}

func TestPrintApplyJSON_EmptySlicesAreArrays(t *testing.T) {
	outcomes := []applyOutcome{
		{Scope: "global", Target: "opencode", Applied: nil, Skipped: nil, Gaps: nil},
	}

	var buf bytes.Buffer
	if err := printApplyJSON(&buf, outcomes); err != nil {
		t.Fatalf("printApplyJSON: %v", err)
	}

	// Parse the JSON to verify structure.
	var report jsonApplyReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(report.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(report.Targets))
	}

	// Applied, Skipped, Gaps must be empty arrays, not null.
	if report.Targets[0].Applied == nil {
		t.Error("Applied is nil, want empty array []")
	}
	if report.Targets[0].Skipped == nil {
		t.Error("Skipped is nil, want empty array []")
	}
	if report.Targets[0].Gaps == nil {
		t.Error("Gaps is nil, want empty array []")
	}

	// Also verify the raw JSON contains "[]" not "null".
	raw := buf.String()
	if strings.Contains(raw, `"applied":null`) {
		t.Error(`raw JSON contains "applied":null, want "applied":[]`)
	}
	if strings.Contains(raw, `"skipped":null`) {
		t.Error(`raw JSON contains "skipped":null, want "skipped":[]`)
	}
	if strings.Contains(raw, `"gaps":null`) {
		t.Error(`raw JSON contains "gaps":null, want "gaps":[]`)
	}
}

func TestToJSONOutput_TypeFieldIsStable(t *testing.T) {
	tests := []struct {
		name   string
		output render.Output
		want   string
	}{
		{"WriteFile", render.WriteFile{Path: "/tmp/x"}, "write_file"},
		{"MergeJSON", render.MergeJSON{Path: "/tmp/x"}, "merge_json"},
		{"MergeYAML", render.MergeYAML{Path: "/tmp/x"}, "merge_yaml"},
		{"MergeTOML", render.MergeTOML{Path: "/tmp/x"}, "merge_toml"},
		{"RebuildDir", render.RebuildDir{Dir: "/tmp/x"}, "rebuild_dir"},
		{"RunCommand", render.RunCommand{Argv: []string{"echo", "hi"}}, "run_command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jo := toJSONOutput(tt.output)
			if jo.Type != tt.want {
				t.Errorf("type = %q, want %q (was Go type name like render.%s)", jo.Type, tt.want, tt.name)
			}
		})
	}
}

func TestOutputTypeName_StableForAllTypes(t *testing.T) {
	tests := []struct {
		name   string
		output render.Output
		want   string
	}{
		{"WriteFile", render.WriteFile{Path: "/tmp/x"}, "write_file"},
		{"MergeJSON", render.MergeJSON{Path: "/tmp/x"}, "merge_json"},
		{"MergeYAML", render.MergeYAML{Path: "/tmp/x"}, "merge_yaml"},
		{"MergeTOML", render.MergeTOML{Path: "/tmp/x"}, "merge_toml"},
		{"RebuildDir", render.RebuildDir{Dir: "/tmp/x"}, "rebuild_dir"},
		{"RunCommand", render.RunCommand{Argv: []string{"echo", "hi"}}, "run_command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := outputTypeName(tt.output)
			if got != tt.want {
				t.Errorf("outputTypeName(%T) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}
