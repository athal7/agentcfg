package registry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"

	"github.com/athal7/agentcfg/internal/registry"
)

func unmarshalValue(t *testing.T, doc string) registry.Value {
	t.Helper()
	var v registry.Value
	if err := yaml.Unmarshal([]byte(doc), &v); err != nil {
		t.Fatalf("unmarshal value: %v", err)
	}
	return v
}

func TestValueResolve_Literal(t *testing.T) {
	v := unmarshalValue(t, `https://example.com/mcp`)

	got, err := v.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := "https://example.com/mcp"; got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
}

func TestValueResolve_Env(t *testing.T) {
	t.Setenv("AGENTCFG_TEST_TOKEN", "secret-value")
	v := unmarshalValue(t, "from: env\nname: AGENTCFG_TEST_TOKEN\n")

	got, err := v.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := "secret-value"; got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
}

func TestValueResolve_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("file-contents\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	v := unmarshalValue(t, "from: file\npath: "+path+"\n")

	got, err := v.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := "file-contents"; got != want {
		t.Errorf("Resolve() = %q, want %q (should be trimmed)", got, want)
	}
}

func TestValueResolve_Command(t *testing.T) {
	v := unmarshalValue(t, "from: command\nrun: [echo, hello]\n")

	got, err := v.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := "hello"; got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
}

func TestValueResolve_FormatWrapsResolvedValue(t *testing.T) {
	t.Setenv("AGENTCFG_TEST_TOKEN", "abc123")
	v := unmarshalValue(t, "from: env\nname: AGENTCFG_TEST_TOKEN\nformat: \"Bearer {}\"\n")

	got, err := v.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if want := "Bearer abc123"; got != want {
		t.Errorf("Resolve() = %q, want %q", got, want)
	}
}

func TestValueResolve_DoesNotResolveAtLoadTime(t *testing.T) {
	// A command source with a command that would fail if executed proves
	// Resolve() isn't called during unmarshal — unmarshaling alone must
	// never shell out.
	v := unmarshalValue(t, "from: command\nrun: [does-not-exist-binary-xyz]\n")
	if v.From != "command" {
		t.Fatalf("expected From to be set from unmarshal alone, got %q", v.From)
	}
}

func TestValueUnmarshalDoesNotExecuteCommand(t *testing.T) {
	// Unmarshaling a Value with from: command must NOT execute the command.
	// Only calling .Resolve() should trigger execution.
	// Use a command that writes a marker file to prove no side effect
	// occurred during unmarshal.
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker.txt")

	v := unmarshalValue(t, "from: command\nrun: [sh, -c, 'echo resolved > "+marker+"']\n")

	// Unmarshal alone must not have executed the command — marker must not exist.
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker file exists after unmarshal alone — command was executed during unmarshal")
	}

	// Now call Resolve() — the command should execute and create the marker.
	_, err := v.Resolve()
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	// After Resolve(), the marker file must exist.
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("marker file not found after Resolve(): %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "resolved" {
		t.Errorf("marker contents = %q, want %q", got, "resolved")
	}
}
