package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExploreWritesHTML(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "sub", "explorer.html")
	cfg := filepath.Join(dir, "arch.yaml")
	data, err := os.ReadFile(exampleYA)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\nsource:\n  url_template: \"https://github.com/o/r/blob/{ref}/{path}#L{line}\"\n")...)
	if err := os.WriteFile(cfg, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"explore", "--root", sinkRoot, "--config", cfg, "--ref", "abc123", "--out", out}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	html, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"ref":"abc123"`, "https://github.com/o/r/blob/{ref}/{path}#L{line}", "archmodel-model"} {
		if !strings.Contains(string(html), want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
	if len(html) > 2<<20 {
		t.Errorf("%d bytes", len(html))
	}
}

func TestExploreFromModelFile(t *testing.T) {
	var model, stderr bytes.Buffer
	if code := run([]string{"build", "--root", sinkRoot, "--config", exampleYA, "--out", "-"}, &model, &stderr); code != 0 {
		t.Fatalf("build: %s", stderr.String())
	}
	mp := filepath.Join(t.TempDir(), "model.json")
	os.WriteFile(mp, model.Bytes(), 0o644)
	var out bytes.Buffer
	if code := run([]string{"explore", "--model", mp, "--config", exampleYA, "--out", "-"}, &out, &stderr); code != 0 {
		t.Fatalf("explore: %s", stderr.String())
	}
	if !strings.HasPrefix(out.String(), "<!DOCTYPE html>") {
		t.Error("not HTML")
	}
}
