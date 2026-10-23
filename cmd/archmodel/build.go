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
	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// errUsage means the flag package already printed the problem.
var errUsage = errors.New("usage")

// errFailed means the command already printed its result and exits 1.
var errFailed = errors.New("failed")

func init() {
	register("build", command{summary: "extract the Architecture model and write it as JSON", run: runBuild})
}

// inputFlags are the flags shared by every command that needs a model.
type inputFlags struct {
	cfgPath, root, cover string
	explicit             bool
	fs                   *flag.FlagSet
}

func addInputFlags(fs *flag.FlagSet) *inputFlags {
	in := &inputFlags{fs: fs}
	fs.StringVar(&in.cfgPath, "config", "architecture.yaml", "config file; a missing default file falls back to the built-in defaults, a missing explicit one is an error")
	fs.StringVar(&in.root, "root", ".", "project root")
	fs.StringVar(&in.cover, "coverprofile", "", "Go coverprofile; enables coverage and CRAP")
	return in
}

// build extracts a fresh model from the flags. Call after fs.Parse.
func (in *inputFlags) build() (*model.Model, error) {
	in.fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			in.explicit = true
		}
	})
	cfg, err := config.Load(in.cfgPath, !in.explicit)
	if err != nil {
		return nil, err
	}
	opts := extract.Options{Root: in.root, Config: cfg}
	if in.cover != "" {
		f, err := os.Open(in.cover)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		opts.Coverage = f
	}
	return extract.Build(opts)
}

func runBuild(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := addInputFlags(fs)
	out := fs.String("out", ".archmodel/model.json", `output file ("-" for stdout)`)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	m, err := in.build()
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
	fmt.Fprintf(stderr, "wrote %s (%d modules, %d dependencies, %d operations, %d tables, %d violations, %d warnings)\n",
		dest, len(m.Modules), len(m.Dependencies), len(m.Operations), len(m.Tables), len(m.Violations), len(m.Warnings))
	return nil
}
