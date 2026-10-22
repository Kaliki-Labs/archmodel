package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

const (
	sinkRoot  = "../../testdata/kaliki-template/kitchen-sink"
	exampleYA = "../../docs/architecture.example.yaml"
	goldenDir = "../../testdata/golden"
)

// Golden tests for the agent-facing JSON on kitchen-sink. check exits 1 here
// because of the known cmd/server package-doc defect (see the rules tests).
func TestGoldenGuardrails(t *testing.T) {
	cases := []struct {
		file string
		args []string
		code int
	}{
		{"kitchen-sink.check.json", []string{"check", "--json"}, 1},
		{"kitchen-sink.query-module-auth.json", []string{"query", "module", "auth"}, 0},
		{"kitchen-sink.query-module-events.json", []string{"query", "module", "events"}, 0},
		{"kitchen-sink.query-who-depends-config.json", []string{"query", "who-depends", "config"}, 0},
		{"kitchen-sink.query-depends-on-server.json", []string{"query", "depends-on", "server"}, 0},
		{"kitchen-sink.query-operation-login.json", []string{"query", "operation", "login"}, 0},
		{"kitchen-sink.query-table-outbox.json", []string{"query", "table", "outbox"}, 0},
		{"kitchen-sink.query-rules.json", []string{"query", "rules"}, 0},
		{"kitchen-sink.query-unknown-module.json", []string{"query", "module", "auht"}, 1},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			args := append(append([]string{}, c.args...), "--root", sinkRoot, "--config", exampleYA)
			var out, errb bytes.Buffer
			if code := run(args, &out, &errb); code != c.code {
				t.Fatalf("exit %d, want %d: %s", code, c.code, errb.String())
			}
			file := filepath.Join(goldenDir, c.file)
			if *update {
				if err := os.WriteFile(file, out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v (run go test ./cmd/archmodel -update)", err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Errorf("%s differs from golden (run go test ./cmd/archmodel -update and review the diff)\n%s", c.file, out.String())
			}
		})
	}
}
