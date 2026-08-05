package apply

import (
	"strings"
	"testing"

	"github.com/athal7/agentcfg/internal/render"
)

func TestApplyRunCommand_SuccessReturnsAppliedDescription(t *testing.T) {
	applied, skipped, err := applyRunCommand(render.RunCommand{
		Argv: []string{"true"},
		Why:  "smoke test",
	})
	if err != nil {
		t.Fatalf("applyRunCommand returned error: %v", err)
	}
	if skipped != "" {
		t.Errorf("skipped = %q, want empty", skipped)
	}
	if !strings.Contains(applied, "smoke test") {
		t.Errorf("applied = %q, want it to mention Why", applied)
	}
}

func TestApplyRunCommand_FailureIncludesOutputWhenNotSecret(t *testing.T) {
	_, _, err := applyRunCommand(render.RunCommand{
		Argv: []string{"sh", "-c", "echo boom-marker >&2; exit 1"},
		Why:  "deliberate failure",
	})
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "boom-marker") {
		t.Errorf("error = %q, want it to include captured output for a non-secret command", err.Error())
	}
}

func TestApplyRunCommand_SecretFailureRedactsArgvAndOutput(t *testing.T) {
	const secretArg = "sk-super-secret-token-marker"
	_, _, err := applyRunCommand(render.RunCommand{
		Argv:   []string{"sh", "-c", "echo " + secretArg + " >&2; exit 1"},
		Why:    "sync secret",
		Secret: true,
	})
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	if strings.Contains(err.Error(), secretArg) {
		t.Errorf("error = %q, must not contain the secret argv/output", err.Error())
	}
	if strings.Contains(err.Error(), "sh") || strings.Contains(err.Error(), "-c") {
		t.Errorf("error = %q, must not contain the argv at all when Secret", err.Error())
	}
}

func TestApplyRunCommand_SecretFailureWithFakeBinaryRedactsEverything(t *testing.T) {
	_, _, err := applyRunCommand(render.RunCommand{
		Argv:   []string{"/bin/false"},
		Why:    "sync secret",
		Secret: true,
	})
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	if strings.Contains(err.Error(), "/bin/false") {
		t.Errorf("error = %q, must not contain the argv when Secret", err.Error())
	}
}

func TestApplyRunCommand_SecretSuccessRedactsArgvAndOutputToo(t *testing.T) {
	const secretArg = "sk-another-secret-marker"
	applied, _, err := applyRunCommand(render.RunCommand{
		Argv:   []string{"sh", "-c", "echo " + secretArg},
		Why:    "sync secret",
		Secret: true,
	})
	if err != nil {
		t.Fatalf("applyRunCommand returned error: %v", err)
	}
	if strings.Contains(applied, secretArg) {
		t.Errorf("applied = %q, must not contain the secret output on success", applied)
	}
	if strings.Contains(applied, "sh") || strings.Contains(applied, "-c") {
		t.Errorf("applied = %q, must not contain the argv at all when Secret", applied)
	}
	if !strings.Contains(applied, "sync secret") {
		t.Errorf("applied = %q, want it to still mention Why", applied)
	}
}
