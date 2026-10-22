package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Kaliki-Labs/archmodel/internal/explorer"
)

func init() {
	register("explore", command{summary: "write the self-contained Explorer HTML", run: runExplore})
}

func runExplore(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("explore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	g := addGuardFlags(fs)
	out := fs.String("out", ".archmodel/explorer.html", `output file ("-" for stdout)`)
	ref := fs.String("ref", "", "git ref for {ref} in source.url_template (branch, tag or commit)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "archmodel explore: unexpected argument %q\n", fs.Arg(0))
		return errUsage
	}
	cfg, err := g.config()
	if err != nil {
		return err
	}
	m, err := g.loadWith(cfg)
	if err != nil {
		return err
	}
	html, err := explorer.Render(m, explorer.Options{SourceURLTemplate: cfg.Source.URLTemplate, Ref: *ref})
	if err != nil {
		return err
	}
	if *out == "-" {
		_, err = stdout.Write(html)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, html, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "wrote %s (%.1f KB, %d modules)\n", *out, float64(len(html))/1024, len(m.Modules))
	return nil
}
