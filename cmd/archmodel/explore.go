package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Kaliki-Labs/archmodel/internal/diff"
	"github.com/Kaliki-Labs/archmodel/internal/explorer"
	"github.com/Kaliki-Labs/archmodel/internal/model"
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
	diffMode := fs.Bool("diff", false, "render in diff mode: highlight what changed since --base")
	basePath := fs.String("base", "", "with --diff: the base model.json to compare against")
	headPath := fs.String("head", "", "with --diff: the head model.json (default: build or read --model like the other flags)")
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
	var m *model.Model
	if *diffMode && *headPath != "" {
		m, err = readModel(*headPath)
	} else {
		m, err = g.loadWith(cfg)
	}
	if err != nil {
		return err
	}
	opts := explorer.Options{SourceURLTemplate: cfg.Source.URLTemplate, Ref: *ref}
	if *diffMode {
		if *basePath == "" {
			fmt.Fprintln(stderr, "archmodel explore: --diff needs --base <model.json>")
			return errUsage
		}
		base, err := readModel(*basePath)
		if err != nil {
			return err
		}
		d := diff.Compute(base, m)
		m = diff.Overlay(base, m)
		opts.Diff = d.ExplorerPayload()
	} else if *basePath != "" || *headPath != "" {
		fmt.Fprintln(stderr, "archmodel explore: --base and --head need --diff")
		return errUsage
	}
	html, err := explorer.Render(m, opts)
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
