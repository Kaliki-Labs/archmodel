package main

import (
	"bytes"
	"encoding/json"
	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// project builds a tiny kaliki-style project in a temp dir. spec lists Domain
// names (api/services/<name>.yaml); pkgs maps a module dir under
// backend/internal or backend/cmd to "doc|imports..." where doc is "doc" or
// "nodoc" and imports are module IDs (internal/<id>).
func project(t *testing.T, domains []string, pkgs map[string]string, configYAML string) (root string) {
	t.Helper()
	root = t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("backend/go.mod", "module example.com/app\n\ngo 1.26\n")
	for _, d := range domains {
		write("api/services/"+d+".yaml", "openapi: 3.0.3\ninfo: {title: "+d+", version: \"1\"}\npaths: {}\n")
	}
	for dir, spec := range pkgs {
		parts := strings.Split(spec, "|")
		name := filepath.Base(dir)
		src := ""
		if parts[0] == "doc" {
			src += "// Package " + name + " is a test package.\n"
		}
		src += "package " + name + "\n"
		if len(parts) > 1 && parts[1] != "" {
			src += "import (\n"
			for _, imp := range strings.Split(parts[1], ",") {
				src += "\t_ \"example.com/app/internal/" + imp + "\"\n"
			}
			src += ")\n"
		}
		write("backend/"+dir+"/x.go", src)
	}
	if configYAML != "" {
		write("architecture.yaml", configYAML)
	}
	return root
}

func runCheckCmd(t *testing.T, root string, extra ...string) (int, string, string) {
	t.Helper()
	args := append([]string{"check", "--root", root}, extra...)
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestCheckCleanProject(t *testing.T) {
	root := project(t, []string{"auth"}, map[string]string{
		"internal/auth":   "doc|config",
		"internal/config": "doc",
		"internal/server": "doc|auth,config",
	}, "")
	code, out, _ := runCheckCmd(t, root)
	if code != 0 || !strings.Contains(out, "5 rules, 0 violations") {
		t.Errorf("code=%d out=%s", code, out)
	}
}

func TestCheckEachRule(t *testing.T) {
	cases := []struct {
		name    string
		domains []string
		pkgs    map[string]string
		cfg     string
		rule    string
		subject string
		site    string // file:line
	}{
		{"inward_only", nil, map[string]string{
			"internal/config": "doc|cache",
			"internal/cache":  "doc",
		}, "", "inward_only", "config -> cache", "backend/internal/config/x.go:4"},
		{"no_domain_to_domain", []string{"auth", "items"}, map[string]string{
			"internal/auth":  "doc|items",
			"internal/items": "doc",
		}, "", "no_domain_to_domain", "auth -> items", "backend/internal/auth/x.go:4"},
		{"no_import_composition_root", nil, map[string]string{
			"internal/mail":   "doc|server",
			"internal/server": "doc",
		}, "", "no_import_composition_root", "mail -> server", "backend/internal/mail/x.go:4"},
		{"test_only_out_of_prod", nil, map[string]string{
			"internal/mail":        "doc|testhelpers",
			"internal/testhelpers": "doc",
		}, "kinds:\n  overrides: {testhelpers: test_only}\n", "test_only_out_of_prod", "mail -> testhelpers", "backend/internal/mail/x.go:4"},
		{"package_doc", nil, map[string]string{
			"internal/mail": "nodoc",
		}, "", "package_doc", "mail", "backend/internal/mail"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := project(t, c.domains, c.pkgs, c.cfg)
			cfgArgs := []string{}
			if c.cfg != "" {
				cfgArgs = []string{"--config", filepath.Join(root, "architecture.yaml")}
			}
			code, out, _ := runCheckCmd(t, root, cfgArgs...)
			if code != 1 {
				t.Fatalf("exit = %d\n%s", code, out)
			}
			cfg, err := loadDefaultRule(c.rule)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"ERROR", c.subject, "[" + c.rule + "]", c.site, "Why: " + firstWords(cfg.Rationale), "Fix: " + firstWords(cfg.Fix)} {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
			// The same result as JSON for agents.
			code, out, _ = runCheckCmd(t, root, append(cfgArgs, "--json")...)
			var res checkResult
			if err := json.Unmarshal([]byte(out), &res); err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			found := false
			for _, v := range res.Violations {
				if v.Rule == c.rule && v.Subject == c.subject {
					found = true
					if v.Rationale != cfg.Rationale || v.Fix != cfg.Fix || len(v.Sites) == 0 {
						t.Errorf("incomplete violation: %+v", v)
					}
				}
			}
			if code != 1 || res.OK || !found || res.Version != 1 || len(res.Rules) != 5 {
				t.Errorf("code=%d ok=%v found=%v\n%s", code, res.OK, found, out)
			}
		})
	}
}

func TestDomainToDomainFixHintNamesInterface(t *testing.T) {
	root := project(t, []string{"auth", "items"}, map[string]string{
		"internal/auth": "doc|items", "internal/items": "doc",
	}, "")
	_, out, _ := runCheckCmd(t, root)
	if !strings.Contains(out, "small interface") || !strings.Contains(out, "Composition root") {
		t.Errorf("fix hint:\n%s", out)
	}
}

func TestWarningsPrintButDoNotFail(t *testing.T) {
	root := project(t, nil, map[string]string{"internal/mail": "nodoc"},
		"rules:\n  package_doc:\n    severity: warn\n")
	code, out, _ := runCheckCmd(t, root, "--config", filepath.Join(root, "architecture.yaml"))
	if code != 0 || !strings.Contains(out, "WARN") || !strings.Contains(out, "1 violation (0 errors, 1 warning)") {
		t.Errorf("code=%d\n%s", code, out)
	}
}

func TestTestOnlyEdgesNeverViolate(t *testing.T) {
	root := project(t, nil, map[string]string{
		"internal/mail":  "doc",
		"internal/cache": "doc",
	}, "")
	// A test file imports the Composition root and an outward module.
	src := "package mail_test\nimport (\n\t_ \"example.com/app/internal/cache\"\n)\n"
	if err := os.WriteFile(filepath.Join(root, "backend/internal/mail/x_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runCheckCmd(t, root)
	if code != 0 {
		t.Errorf("code=%d\n%s", code, out)
	}
}

func TestCheckUsageAndMissingModel(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"check", "--bogus"}, &out, &errb); code != 2 {
		t.Errorf("bad flag exit = %d", code)
	}
	if code := run([]string{"check", "extra"}, &out, &errb); code != 2 {
		t.Errorf("extra arg exit = %d", code)
	}
	if code := run([]string{"check", "--model", "nope.json"}, &out, &errb); code != 1 {
		t.Errorf("missing model exit = %d", code)
	}
}

func TestCheckFromModelFile(t *testing.T) {
	root := project(t, nil, map[string]string{"internal/mail": "nodoc"}, "")
	var out, errb bytes.Buffer
	if code := run([]string{"build", "--root", root, "--out", "-"}, &out, &errb); code != 0 {
		t.Fatal(errb.String())
	}
	mp := filepath.Join(t.TempDir(), "model.json")
	if err := os.WriteFile(mp, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	var out2 bytes.Buffer
	code := run([]string{"check", "--model", mp}, &out2, &errb)
	if code != 1 || !strings.Contains(out2.String(), "[package_doc]") {
		t.Errorf("code=%d\n%s", code, out2.String())
	}
}

func loadDefaultRule(id string) (model.Rule, error) {
	cfg, err := config.Defaults()
	if err != nil {
		return model.Rule{}, err
	}
	r := cfg.Rules[id]
	return model.Rule{ID: id, Severity: r.Severity, Rationale: r.Rationale, Fix: r.Fix}, nil
}

// firstWords is the start of a long text, short enough to survive line wrapping.
func firstWords(s string) string {
	f := strings.Fields(s)
	return strings.Join(f[:3], " ")
}
