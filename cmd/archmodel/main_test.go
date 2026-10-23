package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"frobnicate"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "build") {
		t.Errorf("code=%d stderr=%q", code, errb.String())
	}
}

func TestBuildToStdoutAndFile(t *testing.T) {
	root := "../../testdata/kaliki-template/minimal"
	cfg := "../../docs/architecture.example.yaml"
	var out, errb bytes.Buffer
	if code := run([]string{"build", "--root", root, "--config", cfg, "--out", "-"}, &out, &errb); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	var m struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(out.Bytes(), &m); err != nil || m.Version != 1 {
		t.Fatalf("bad JSON (%v): %.200s", err, out.String())
	}

	dest := filepath.Join(t.TempDir(), "sub", "model.json")
	out.Reset()
	if code := run([]string{"build", "--root", root, "--config", cfg, "--out", dest}, &out, &errb); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(dest); err != nil {
		t.Error(err)
	}
}

func TestExplicitMissingConfigIsAnError(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"build", "--config", "nope.yaml", "--out", "-"}, &out, &errb); code != 1 {
		t.Errorf("code=%d", code)
	}
}
