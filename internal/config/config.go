// Package config loads the architecture configuration: the embedded defaults
// (template conventions) merged with a consumer's architecture.yaml.
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yaml
var defaultsYAML []byte

// Config is the merged configuration.
type Config struct {
	Version    int             `yaml:"version"`
	Levels     []string        `yaml:"levels"`
	Go         GoConfig        `yaml:"go"`
	Kinds      KindsConfig     `yaml:"kinds"`
	API        APIConfig       `yaml:"api"`
	SQL        SQLConfig       `yaml:"sql"`
	Rules      map[string]Rule `yaml:"rules"`
	Thresholds map[string]any  `yaml:"thresholds"`
}

// GoConfig says where the Go code and its Modules live.
type GoConfig struct {
	Dir       string      `yaml:"dir"`
	Roots     []Root      `yaml:"roots"`
	Generated []Generated `yaml:"generated"`
}

// Root is a directory (relative to go.dir) whose children are Modules.
type Root struct {
	Dir      string `yaml:"dir"`
	IDPrefix string `yaml:"id_prefix"`
}

// Generated maps generated packages to the Module that owns them.
type Generated struct {
	Dir    string `yaml:"dir"`
	Module string `yaml:"module"`
}

// KindsConfig drives Module kind classification by convention.
type KindsConfig struct {
	CompositionRoot []string          `yaml:"composition_root"`
	Foundation      []string          `yaml:"foundation"`
	Overrides       map[string]string `yaml:"overrides"`
}

// APIConfig locates OpenAPI specs.
type APIConfig struct {
	SpecsDir    string `yaml:"specs_dir"`
	Implementer string `yaml:"implementer"`
}

// SQLConfig locates goose migrations and sqlc query files.
type SQLConfig struct {
	SchemaDir    string            `yaml:"schema_dir"`
	QueriesDir   string            `yaml:"queries_dir"`
	QueryModules map[string]string `yaml:"query_modules"`
}

// Rule is a Guardrails rule's policy: severity, Rationale and fix hint.
type Rule struct {
	Severity  string `yaml:"severity"`
	Rationale string `yaml:"rationale"`
	Fix       string `yaml:"fix"`
}

// Defaults returns the embedded defaults alone.
func Defaults() (*Config, error) { return Merge(nil) }

// Load reads the consumer's file at path and merges it over the defaults. A
// missing file is an error unless optional is true, in which case the defaults
// are returned.
func Load(file string, optional bool) (*Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return Merge(nil)
		}
		return nil, err
	}
	cfg, err := Merge(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return cfg, nil
}

// Merge merges consumer YAML (may be nil) over the embedded defaults. Maps
// merge key by key; scalars and lists replace. Unknown keys are an error.
func Merge(consumer []byte) (*Config, error) {
	var base map[string]any
	if err := yaml.Unmarshal(defaultsYAML, &base); err != nil {
		return nil, fmt.Errorf("embedded defaults: %w", err)
	}
	if len(bytes.TrimSpace(consumer)) > 0 {
		// Strict decode first: gives line numbers for unknown keys.
		var probe Config
		if err := strictDecode(consumer, &probe); err != nil {
			return nil, err
		}
		if probe.Version != 0 && probe.Version != 1 {
			return nil, fmt.Errorf("unsupported config version %d (this tool reads version 1)", probe.Version)
		}
		var over map[string]any
		if err := yaml.Unmarshal(consumer, &over); err != nil {
			return nil, err
		}
		base = mergeMaps(base, over)
	}
	merged, err := yaml.Marshal(base)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := strictDecode(merged, &cfg); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func strictDecode(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return nil
}

func mergeMaps(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		bm, bok := out[k].(map[string]any)
		om, ook := v.(map[string]any)
		if bok && ook {
			out[k] = mergeMaps(bm, om)
		} else {
			out[k] = v
		}
	}
	return out
}

func (c *Config) validate() error {
	if len(c.Go.Roots) == 0 {
		return errors.New("invalid config: go.roots is empty")
	}
	valid := map[string]bool{}
	for _, l := range c.Levels {
		valid[l] = true
	}
	valid["test_only"] = true
	for id, k := range c.Kinds.Overrides {
		if !valid[k] {
			return fmt.Errorf("invalid config: kinds.overrides[%q] = %q is not one of %v", id, k, c.Levels)
		}
	}
	for _, pat := range append(append([]string{}, c.Kinds.CompositionRoot...), c.Kinds.Foundation...) {
		if _, err := path.Match(pat, ""); err != nil {
			return fmt.Errorf("invalid config: bad pattern %q: %w", pat, err)
		}
	}
	return nil
}
