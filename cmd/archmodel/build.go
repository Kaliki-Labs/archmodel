package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/extract"
)

// errUsage means the flag package already printed the problem.
var errUsage = errors.New("usage")

func init() {
	register("build", command{summary: "extract the Architecture model and write it as JSON", run: runBuild})
}

func runBuild(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("config", "architecture.yaml", "config file; a missing default file falls back to the built-in defaults, a missing explicit one is an error")
	root := fs.String("root", ".", "project root")
	out := fs.String("out", ".archmodel/model.json", `output file ("-" for stdout)`)
	cover := fs.String("coverprofile", "", "Go coverprofile; enables coverage and CRAP")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			explicit = true
		}
	})
	cfg, err := config.Load(*cfgPath, !explicit)
	if err != nil {
		return err
	}
	opts := extract.Options{Root: *root, Config: cfg}
	if *cover != "" {
		f, err := os.Open(*cover)
		if err != nil {
			return err
		}
		defer f.Close()
		opts.Coverage = f
	}
	m, err := extract.Build(opts)
	if err != nil {
		return err
	}
	data, err := m.JSON()
	if err != nil {
		return err
	}
	for _, w := range m.Warnings {
		if w.Path != "" {
			fmt.Fprintf(stderr, "warning: [%s] %s: %s\n", w.Code, w.Path, w.Message)
		} else {
			fmt.Fprintf(stderr, "warning: [%s] %s\n", w.Code, w.Message)
		}
	}
	if *out == "-" {
		_, err = stdout.Write(data)
		return err
	}
	dest := *out
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "wrote %s (%d modules, %d dependencies, %d operations, %d tables, %d warnings)\n",
		dest, len(m.Modules), len(m.Dependencies), len(m.Operations), len(m.Tables), len(m.Warnings))
	return nil
}
