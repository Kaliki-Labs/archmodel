package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// projectCopy copies the kitchen-sink fixture to a temp dir with the example
// config installed as architecture.yaml.
func projectCopy(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	if out, err := exec.Command("cp", "-R", "../../testdata/kaliki-template/kitchen-sink/.", dst).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	cfg, err := os.ReadFile("../../docs/architecture.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "architecture.yaml"), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func runIn(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRootDiscoveryFromSubdirectory(t *testing.T) {
	proj := projectCopy(t)
	code, want, se := runIn(t, proj, "build", "--out", "-")
	if code != 0 {
		t.Fatalf("root: code=%d %s", code, se)
	}
	code, got, se := runIn(t, filepath.Join(proj, "backend"), "build", "--out", "-")
	if code != 0 {
		t.Fatalf("backend: code=%d %s", code, se)
	}
	if got != want {
		t.Error("model from backend/ differs from model from the root")
	}

	// Default output lands under the discovered root.
	if code, _, se := runIn(t, filepath.Join(proj, "backend"), "build"); code != 0 {
		t.Fatalf("code=%d %s", code, se)
	}
	if _, err := os.Stat(filepath.Join(proj, ".archmodel", "model.json")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(proj, "backend", ".archmodel")); err == nil {
		t.Error("output written under the cwd instead of the root")
	}
	if code, _, se := runIn(t, filepath.Join(proj, "backend"), "explore"); code != 0 {
		t.Fatalf("explore code=%d %s", code, se)
	}
	if _, err := os.Stat(filepath.Join(proj, ".archmodel", "explorer.html")); err != nil {
		t.Error(err)
	}
	// Explicit --out stays cwd-relative.
	if code, _, se := runIn(t, filepath.Join(proj, "backend"), "build", "--out", "m.json"); code != 0 {
		t.Fatalf("code=%d %s", code, se)
	}
	if _, err := os.Stat(filepath.Join(proj, "backend", "m.json")); err != nil {
		t.Error(err)
	}
	if code, _, se := runIn(t, filepath.Join(proj, "backend"), "check"); code > 1 {
		t.Errorf("check code=%d %s", code, se)
	}
}

func TestExplicitRootDisablesDiscovery(t *testing.T) {
	proj := projectCopy(t)
	other := t.TempDir() // no architecture.yaml, no go.mod
	code, _, se := runIn(t, filepath.Join(proj, "backend"), "build", "--root", other, "--out", "-")
	if code == 0 {
		t.Fatal("expected failure: explicit --root should not be replaced by discovery")
	}
	if !strings.Contains(se, "go.mod") {
		t.Errorf("stderr %q", se)
	}
}

func TestMissingConfigFallsBackToDot(t *testing.T) {
	proj := projectCopy(t)
	if err := os.Remove(filepath.Join(proj, "architecture.yaml")); err != nil {
		t.Fatal(err)
	}
	code, _, se := runIn(t, proj, "build", "--out", "-")
	if code != 0 {
		t.Fatalf("defaults-only project from root: code=%d %s", code, se)
	}
}

func TestMissingGoModExitsNonZero(t *testing.T) {
	empty := t.TempDir()
	code, _, se := runIn(t, empty, "build", "--out", "-")
	if code == 0 || !strings.Contains(se, "go.mod") {
		t.Errorf("code=%d stderr=%q", code, se)
	}
}
