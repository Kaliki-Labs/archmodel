package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

func init() {
	register("check", command{summary: "evaluate the Guardrails rules; exit 1 on an error-severity violation", run: runCheck})
}

// checkResult is the stable --json output.
type checkResult struct {
	Version    int               `json:"version"`
	Rules      []model.Rule      `json:"rules"`
	Violations []model.Violation `json:"violations"`
	OK         bool              `json:"ok"`
}

func runCheck(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := addGuardFlags(fs)
	asJSON := fs.Bool("json", false, "print JSON for agents")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "archmodel check: unexpected argument %q\n", fs.Arg(0))
		return errUsage
	}
	m, err := g.load()
	if err != nil {
		return err
	}
	res := checkResult{Version: 1, Rules: m.Rules, Violations: m.Violations, OK: true}
	for _, v := range m.Violations {
		if v.Severity == "error" {
			res.OK = false
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return err
		}
	} else {
		writeHuman(stdout, res)
	}
	if !res.OK {
		return errFailed
	}
	return nil
}

func writeHuman(w io.Writer, res checkResult) {
	errs, warns := 0, 0
	for _, v := range res.Violations {
		label := "WARN "
		if v.Severity == "error" {
			label = "ERROR"
			errs++
		} else {
			warns++
		}
		fmt.Fprintf(w, "%s  %s  [%s]\n", label, v.Subject, v.Rule)
		fmt.Fprintf(w, "  %s\n", v.Message)
		for _, s := range v.Sites {
			if s.Line > 0 {
				fmt.Fprintf(w, "  at %s:%d\n", s.File, s.Line)
			} else {
				fmt.Fprintf(w, "  at %s\n", s.File)
			}
		}
		fmt.Fprintf(w, "  Why: %s\n", wrap(v.Rationale, 76, "       "))
		fmt.Fprintf(w, "  Fix: %s\n\n", wrap(v.Fix, 76, "       "))
	}
	n := len(res.Violations)
	summary := fmt.Sprintf("%d rules, %d %s", len(res.Rules), n, plural(n, "violation"))
	if n > 0 {
		summary += fmt.Sprintf(" (%d %s, %d %s)", errs, plural(errs, "error"), warns, plural(warns, "warning"))
	}
	fmt.Fprintln(w, summary)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// wrap folds text to width columns; continuation lines start with indent.
func wrap(text string, width int, indent string) string {
	var b strings.Builder
	col := 0
	for i, word := range strings.Fields(text) {
		if i > 0 {
			if col+1+len(word) > width {
				b.WriteString("\n" + indent)
				col = 0
			} else {
				b.WriteByte(' ')
				col++
			}
		}
		b.WriteString(word)
		col += len(word)
	}
	return b.String()
}
