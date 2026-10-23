package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// guardFlags are the inputs of check and query: build fresh by default, so an
// agent never reads a stale model (ADR-0008), or read --model instead.
type guardFlags struct {
	*inputFlags
	modelPath string
}

func addGuardFlags(fs *flag.FlagSet) *guardFlags {
	g := &guardFlags{inputFlags: addInputFlags(fs)}
	fs.StringVar(&g.modelPath, "model", "", "read this model.json instead of building a fresh one")
	return g
}

func (g *guardFlags) load() (*model.Model, error) {
	cfg, err := g.config()
	if err != nil {
		return nil, err
	}
	return g.loadWith(cfg)
}

// loadWith is load for a caller that already has the config.
func (g *guardFlags) loadWith(cfg *config.Config) (*model.Model, error) {
	if g.modelPath == "" {
		return g.buildWith(cfg)
	}
	data, err := os.ReadFile(g.modelPath)
	if err != nil {
		return nil, err
	}
	var m model.Model
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", g.modelPath, err)
	}
	if m.Version != model.Version {
		return nil, fmt.Errorf("%s: model version %d, this tool reads version %d", g.modelPath, m.Version, model.Version)
	}
	if len(m.Rules) == 0 {
		return nil, fmt.Errorf("%s: no rules in the model; rebuild it with this version of archmodel", g.modelPath)
	}
	m.Normalize()
	return &m, nil
}

// parseInterspersed parses flags that may follow positional arguments, so
// "query module auth --model m.json" works.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
