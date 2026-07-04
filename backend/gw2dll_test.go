package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInspectGraphwar2DLLDetectsGDExtensionOnly(t *testing.T) {
	path := graphwar2DLLTestPath(t)
	info, err := InspectGraphwar2DLL(path)
	if err != nil {
		t.Fatalf("inspect graphwarrust.dll: %v", err)
	}
	if !info.Found {
		t.Fatalf("expected dll to be found")
	}
	if info.Capability != GW2DLLCapabilityGDExtensionOnly {
		t.Fatalf("capability=%q, want %q; exports=%#v", info.Capability, GW2DLLCapabilityGDExtensionOnly, info.Exports)
	}
	if info.DirectCallable {
		t.Fatalf("GDExtension-only dll must not be marked direct callable")
	}
	if !info.RequiresGodotHost {
		t.Fatalf("GDExtension-only dll should require a Godot host")
	}
	if len(info.Exports) != 1 || info.Exports[0].Name != gw2GDExtensionEntry {
		t.Fatalf("exports=%#v, want only %s", info.Exports, gw2GDExtensionEntry)
	}
}

func TestFindGraphwar2DLLHonorsExplicitPath(t *testing.T) {
	path := graphwar2DLLTestPath(t)
	got, err := FindGraphwar2DLL(path)
	if err != nil {
		t.Fatalf("find explicit graphwarrust.dll: %v", err)
	}
	if got != path {
		t.Fatalf("got %q, want %q", got, path)
	}
}

func graphwar2DLLTestPath(t *testing.T) string {
	t.Helper()
	candidates := []string{}
	if env := os.Getenv("GW2_DLL"); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates,
		filepath.Join("..", "..", "graphwar2", "graphwarrust.dll"),
		filepath.Join("..", "..", "graphwar2", "rust", "target", "x86_64-pc-windows-gnu", "release", "graphwarrust.dll"),
	)
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err == nil && !st.IsDir() {
			return abs
		}
	}
	t.Skip("graphwarrust.dll not available; set GW2_DLL to run this test")
	return ""
}
