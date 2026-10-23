package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const jwtRoot = "../../testdata/kaliki-template/jwt-full-otp"

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func edit(t *testing.T, path, old, new string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("%s does not contain %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buildModel writes root's model.json into dir and returns its path.
func buildModel(t *testing.T, root, dir, name string) string {
	t.Helper()
	out := filepath.Join(dir, name)
	var so, se bytes.Buffer
	if code := run([]string{"build", "--root", root, "--config", exampleYA, "--out", out}, &so, &se); code != 0 {
		t.Fatalf("build %s: %s", root, se.String())
	}
	return out
}

// mutatedSink copies kitchen-sink and makes the changes a PR might: a Domain
// importing another Domain (new violation), a new and a changed operation, a
// migration adding a table and a column, and a removed Module.
func mutatedSink(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "kitchen-sink")
	copyTree(t, sinkRoot, root)

	// New violation: items (Domain) imports auth (Domain).
	if err := os.WriteFile(filepath.Join(root, "backend/internal/items/owner.go"),
		[]byte("package items\n\nimport _ \"github.com/example/kitchen-sink-app/backend/internal/auth\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// New operation, and createItem loses its security requirement.
	spec := filepath.Join(root, "api/services/items.yaml")
	edit(t, spec, "      operationId: createItem\n      tags: [items]\n      security:\n        - bearerAuth: []\n",
		"      operationId: createItem\n      tags: [items]\n")
	edit(t, spec, "components:\n", `  /items/{id}/archive:
    post:
      operationId: archiveItem
      tags: [items]
      security:
        - bearerAuth: []
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
            format: uuid
      responses:
        "204":
          description: Archived
        "404":
          description: No such item
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
components:
`)
	// New table and a new column on an existing one.
	if err := os.WriteFile(filepath.Join(root, "backend/sql/schema/0007_widgets.sql"), []byte(`-- +goose Up
CREATE TABLE widgets (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE items ADD COLUMN archived_at TIMESTAMPTZ;

-- +goose Down
DROP TABLE widgets;
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove the push Module.
	if err := os.RemoveAll(filepath.Join(root, "backend/internal/push")); err != nil {
		t.Fatal(err)
	}
	edit(t, filepath.Join(root, "backend/internal/server/server.go"),
		"\t\"github.com/example/kitchen-sink-app/backend/internal/push\"\n", "")
	return root
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	file := filepath.Join(goldenDir, name)
	if *update {
		if err := os.WriteFile(file, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run go test ./cmd/archmodel -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from golden (run go test ./cmd/archmodel -update and review the diff)\n%s", name, got)
	}
}

func runDiffCmd(t *testing.T, args ...string) (string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	if code := run(append([]string{"diff"}, args...), &out, &errb); code != 0 {
		t.Fatalf("diff exit %d: %s", code, errb.String())
	}
	return out.String(), errb.String()
}

func TestDiffGoldenMutatedSink(t *testing.T) {
	tmp := t.TempDir()
	base := buildModel(t, sinkRoot, tmp, "base.json")
	head := buildModel(t, mutatedSink(t), tmp, "head.json")
	if dir := os.Getenv("ARCHMODEL_SAMPLES"); dir != "" { // keep the models for manual inspection
		os.MkdirAll(dir, 0o755)
		for _, n := range []string{"base.json", "head.json"} {
			data, _ := os.ReadFile(filepath.Join(tmp, n))
			os.WriteFile(filepath.Join(dir, n), data, 0o644)
		}
	}
	img := filepath.Join(tmp, "out", "diff")
	md, _ := runDiffCmd(t, "--base", base, "--head", head, "--json", filepath.Join(tmp, "diff.json"), "--image", img+".png")

	golden(t, "kitchen-sink.mutated.diff.md", []byte(md))
	js, _ := os.ReadFile(filepath.Join(tmp, "diff.json"))
	golden(t, "kitchen-sink.mutated.diff.json", js)
	svg, err := os.ReadFile(img + ".svg")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "kitchen-sink.mutated.diff.svg", svg)

	for _, want := range []string{"<!-- archmodel-diff -->", "no_domain_to_domain", "`items -> auth`", "archiveItem",
		"`widgets`", "archived_at", "removed `push`", "![changed subgraph](ARCHMODEL_IMAGE_URL)", "<details>", "```mermaid"} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, md)
		}
	}
	if n := len(strings.Split(md, "\n")); n > 60 {
		t.Errorf("comment is %d lines; it should stay short", n)
	}

	// The PNG decodes and respects the width budget.
	f, err := os.Open(img + ".png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width < 600 || cfg.Width > 1200 || cfg.Height < 300 {
		t.Errorf("PNG is %dx%d", cfg.Width, cfg.Height)
	}
	// Deterministic: a second run is byte-identical.
	md2, _ := runDiffCmd(t, "--base", base, "--head", head, "--image", filepath.Join(tmp, "again"))
	if md2 != md {
		t.Error("Markdown differs between runs")
	}
	a, _ := os.ReadFile(img + ".png")
	b, _ := os.ReadFile(filepath.Join(tmp, "again.png"))
	if !bytes.Equal(a, b) {
		t.Error("PNG differs between runs")
	}
}

func TestDiffGoldenLarge(t *testing.T) {
	tmp := t.TempDir()
	base := buildModel(t, jwtRoot, tmp, "base.json")
	head := buildModel(t, sinkRoot, tmp, "head.json")
	img := filepath.Join(tmp, "large.svg")
	md, _ := runDiffCmd(t, "--base", base, "--head", head, "--image", img)
	golden(t, "jwt-full-otp-to-kitchen-sink.diff.md", []byte(md))
	if n := len(strings.Split(md, "\n")); n > 90 {
		t.Errorf("large comment is %d lines", n)
	}
	for _, p := range []string{"large.svg", "large.png"} {
		if _, err := os.Stat(filepath.Join(tmp, p)); err != nil {
			t.Errorf("%s not written", p)
		}
	}
}

func TestDiffNoChanges(t *testing.T) {
	tmp := t.TempDir()
	m := buildModel(t, sinkRoot, tmp, "m.json")
	md, _ := runDiffCmd(t, "--base", m, "--head", m, "--image", filepath.Join(tmp, "x", "diff"))
	if md != "<!-- archmodel-diff -->\nNo architecture changes.\n" {
		t.Errorf("got %q", md)
	}
	if _, err := os.Stat(filepath.Join(tmp, "x")); err == nil {
		t.Error("image files written for an empty diff")
	}
}

func TestDiffImageNaming(t *testing.T) {
	tmp := t.TempDir()
	base := buildModel(t, sinkRoot, tmp, "base.json")
	head := buildModel(t, mutatedSink(t), tmp, "head.json")
	for _, arg := range []string{"a", "b.svg", "c.PNG"} {
		runDiffCmd(t, "--base", base, "--head", head, "--markdown", filepath.Join(tmp, "m.md"), "--image", filepath.Join(tmp, arg))
	}
	for _, f := range []string{"a.svg", "a.png", "b.svg", "b.png", "c.svg", "c.png"} {
		if _, err := os.Stat(filepath.Join(tmp, f)); err != nil {
			t.Errorf("%s missing", f)
		}
	}
}

func TestDiffCustomImageURLAndNoMermaid(t *testing.T) {
	tmp := t.TempDir()
	base := buildModel(t, sinkRoot, tmp, "base.json")
	head := buildModel(t, mutatedSink(t), tmp, "head.json")
	md, _ := runDiffCmd(t, "--base", base, "--head", head, "--image", filepath.Join(tmp, "i"), "--image-url", "https://x/y.png", "--mermaid=false")
	if !strings.Contains(md, "![changed subgraph](https://x/y.png)") || strings.Contains(md, "mermaid") {
		t.Errorf("unexpected Markdown:\n%s", md)
	}
}

func TestExploreDiffMode(t *testing.T) {
	tmp := t.TempDir()
	base := buildModel(t, sinkRoot, tmp, "base.json")
	head := buildModel(t, mutatedSink(t), tmp, "head.json")
	out := filepath.Join(tmp, "diff.html")
	var so, se bytes.Buffer
	if code := run([]string{"explore", "--diff", "--base", base, "--head", head, "--config", exampleYA, "--out", out}, &so, &se); code != 0 {
		t.Fatalf("explore --diff: %s", se.String())
	}
	html, _ := os.ReadFile(out)
	s := string(html)
	for _, want := range []string{`id="archmodel-diff"`, `"headline"`, "archiveItem", `"removed":{"edges"`, "no_domain_to_domain"} {
		if !strings.Contains(s, want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
	if len(html) > 2<<20 {
		t.Errorf("%d bytes", len(html))
	}
	if code := run([]string{"explore", "--diff", "--config", exampleYA, "--model", head, "--out", "-"}, &so, &se); code != 2 {
		t.Errorf("--diff without --base: exit %d", code)
	}
}
