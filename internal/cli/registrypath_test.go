package cli

import (
	"path/filepath"
	"testing"
)

func TestResolveRegistryDir_FlagWins(t *testing.T) {
	t.Setenv("AGENTCFG_REGISTRY", "/from-env")
	t.Setenv("XDG_CONFIG_HOME", "/from-xdg")

	got := ResolveRegistryDir("/from-flag")
	if got != "/from-flag" {
		t.Errorf("got %q, want /from-flag", got)
	}
}

func TestResolveRegistryDir_EnvWinsOverXDG(t *testing.T) {
	t.Setenv("AGENTCFG_REGISTRY", "/from-env")
	t.Setenv("XDG_CONFIG_HOME", "/from-xdg")

	got := ResolveRegistryDir("")
	if got != "/from-env" {
		t.Errorf("got %q, want /from-env", got)
	}
}

func TestResolveRegistryDir_XDGWinsOverDefault(t *testing.T) {
	t.Setenv("AGENTCFG_REGISTRY", "")
	t.Setenv("XDG_CONFIG_HOME", "/from-xdg")

	got := ResolveRegistryDir("")
	want := filepath.Join("/from-xdg", "agentcfg")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveRegistryDir_DefaultsToHomeConfig(t *testing.T) {
	t.Setenv("AGENTCFG_REGISTRY", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := ResolveRegistryDir("")
	want := filepath.Join(home, ".config", "agentcfg")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveRegistryDir_ExpandsTildeInFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := ResolveRegistryDir("~/custom-registry")
	want := filepath.Join(home, "custom-registry")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveRegistryDir_ExpandsTildeInEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENTCFG_REGISTRY", "~/env-registry")

	got := ResolveRegistryDir("")
	want := filepath.Join(home, "env-registry")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
