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
	resolved             bool
	fs                   *flag.FlagSet
}

func addInputFlags(fs *flag.FlagSet) *inputFlags {
	in := &inputFlags{fs: fs}
	fs.StringVar(&in.cfgPath, "config", "", "config file, relative to the current directory (default <root>/architecture.yaml; a missing default file falls back to the built-in defaults, a missing explicit one is an error)")
	fs.StringVar(&in.root, "root", "", "project root (default: the nearest directory above the current one that holds architecture.yaml, else .)")
	fs.StringVar(&in.cover, "coverprofile", "", "Go coverprofile; enables coverage and CRAP")
	return in
}

// resolve fills in the project root and config path the flags leave open. It
// is the one place root discovery happens, so every command agrees. An explicit
// --root disables discovery. Idempotent; call after fs.Parse.
func (in *inputFlags) resolve() {
	if in.resolved {
		return
	}
	in.resolved = true
	in.fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			in.explicit = true
		}
	})
	if in.root == "" {
		in.root = discoverRoot(".")
	}
	if !in.explicit {
		in.cfgPath = filepath.Join(in.root, "architecture.yaml")
	}
}

// discoverRoot walks up from start to the first directory holding
// architecture.yaml and returns it, or start when there is none.
func discoverRoot(start string) string {
	abs, err := filepath.Abs(start)
	if err != nil {
		return start
	}
	for dir := abs; ; {
		if st, err := os.Stat(filepath.Join(dir, "architecture.yaml")); err == nil && !st.IsDir() {
			if dir == abs {
				return start
			}
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

// outPath is where a default output lands: an explicit --out stays relative
// to the current directory, the default is relative to the project root.
func (in *inputFlags) outPath(out, def string) string {
	if out != "" {
		return out
	}
	in.resolve()
	return filepath.Join(in.root, def)
}

// config loads the configuration named by the flags. Call after fs.Parse.
func (in *inputFlags) config() (*config.Config, error) {
	in.resolve()
	return config.Load(in.cfgPath, !in.explicit)
}

// build extracts a fresh model from the flags. Call after fs.Parse.
func (in *inputFlags) build() (*model.Model, error) {
	cfg, err := in.config()
	if err != nil {
		return nil, err
	}
	return in.buildWith(cfg)
}

func (in *inputFlags) buildWith(cfg *config.Config) (*model.Model, error) {
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
	out := fs.String("out", "", `output file ("-" for stdout; default <root>/.archmodel/model.json)`)
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
	dest := in.outPath(*out, ".archmodel/model.json")
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
