package apply

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/athal7/agentcfg/internal/render"
)

// applyRunCommand runs r.Argv directly (no shell interpolation — Argv is
// already a literal argv slice). When r.Secret is true, neither Argv nor
// the command's captured output is ever included in a returned error or
// applied description, on success or failure.
func applyRunCommand(r render.RunCommand) (applied, skipped string, err error) {
	if len(r.Argv) == 0 {
		return "", "", fmt.Errorf("run command: argv is empty")
	}
	cmd := exec.Command(r.Argv[0], r.Argv[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if runErr := cmd.Run(); runErr != nil {
		if r.Secret {
			return "", "", fmt.Errorf("run command failed (output redacted: marked secret)")
		}
		return "", "", fmt.Errorf("running %v: %w: %s", r.Argv, runErr, out.String())
	}

	if r.Secret {
		return fmt.Sprintf("ran command (%s) [output redacted]", r.Why), "", nil
	}
	return fmt.Sprintf("ran %v (%s)", r.Argv, r.Why), "", nil
}
