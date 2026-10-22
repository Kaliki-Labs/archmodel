// Package model defines the Architecture model and its JSON format (v1,
// ADR-0002): flat, ID-keyed, deterministic.
package model

import (
	"bytes"
	"encoding/json"
	"sort"
)

// Version is the model JSON format version. Additive changes keep it.
const Version = 1

// Module kinds (see CONTEXT.md).
const (
	KindCompositionRoot = "composition_root"
	KindDomain          = "domain"
	KindInfrastructure  = "infrastructure"
	KindFoundation      = "foundation"
	KindTestOnly        = "test_only"
)

// Model is the whole Architecture model.
type Model struct {
	Version      int          `json:"version"`
	Levels       []string     `json:"levels"`
	Modules      []Module     `json:"modules"`
	Dependencies []Dependency `json:"dependencies"`
	Operations   []Operation  `json:"operations"`
	Schemas      []Schema     `json:"schemas"`
	Tables       []Table      `json:"tables"`
	Queries      []Query      `json:"queries"`
	Symbols      []Symbol     `json:"symbols"`
	Functions    []Function   `json:"functions"`
	Warnings     []Warning    `json:"warnings"`
}

// Module is a node in the model.
type Module struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Level   *int   `json:"level,omitempty"` // index into Model.Levels; absent for test-only
	Purpose string `json:"purpose"`         // package doc comment
	Dir     string `json:"dir"`             // relative to the project root
	// Present is false when the package is imported but has no source on disk.
	Present bool `json:"present"`
	// Generated is true when the package is missing (assumed generated) or
	// every source file carries a "Code generated" header.
	Generated   bool         `json:"generated"`
	Subpackages []Subpackage `json:"subpackages"`
	Metrics     *Metrics     `json:"metrics,omitempty"` // absent for test-only modules
}

// Subpackage is a package that belongs to a Module but is not its root.
type Subpackage struct {
	Dir       string `json:"dir"` // relative to the project root
	Name      string `json:"name"`
	Present   bool   `json:"present"`
	Generated bool   `json:"generated"`
	Purpose   string `json:"purpose,omitempty"`
}

// Metrics are package metrics (production graph only) and function rollups.
type Metrics struct {
	Ca                 int     `json:"ca"`
	Ce                 int     `json:"ce"`
	Instability        float64 `json:"instability"`
	InstabilityDefined bool    `json:"instability_defined"` // false when Ca+Ce=0 (I recorded as 0)
	ExportedTypes      int     `json:"exported_types"`
	AbstractTypes      int     `json:"abstract_types"`
	Abstractness       float64 `json:"abstractness"`
	Distance           float64 `json:"distance"`

	WorstCC           int      `json:"worst_cc"`
	WorstCCFunction   string   `json:"worst_cc_function,omitempty"`
	Coverage          *float64 `json:"coverage,omitempty"`
	WorstCRAP         *float64 `json:"worst_crap,omitempty"`
	WorstCRAPFunction string   `json:"worst_crap_function,omitempty"`
}

// Dependency is a module-to-module import edge.
type Dependency struct {
	From     string `json:"from"`
	To       string `json:"to"`
	TestOnly bool   `json:"test_only"`
	// Files are the source files (relative to the project root) with the import.
	Files []string `json:"files"`
}

// Operation is one OpenAPI operation.
type Operation struct {
	ID             string          `json:"id"` // operationId
	Owner          string          `json:"owner"`
	Method         string          `json:"method"`
	Path           string          `json:"path"`
	Summary        string          `json:"summary,omitempty"`
	Tags           []string        `json:"tags"`
	Security       []string        `json:"security"` // scheme names; empty = public
	RateLimit      map[string]any  `json:"rate_limit,omitempty"`
	Parameters     []Parameter     `json:"parameters"`
	RequestSchemas []string        `json:"request_schemas"`
	Responses      []Response      `json:"responses"`
	Spec           string          `json:"spec"` // spec file, relative to the project root
	Line           int             `json:"line"` // line of the operation in the spec
	Implementation *Implementation `json:"implementation,omitempty"`
}

// Parameter is an operation parameter.
type Parameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
	Type     string `json:"type,omitempty"`
}

// Response is one documented status code.
type Response struct {
	Status      string   `json:"status"`
	Description string   `json:"description"`
	Schemas     []string `json:"schemas"`
}

// Implementation links an operation to the Go method implementing it.
type Implementation struct {
	Function string `json:"function"` // Function.ID
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// Schema is a components.schemas entry.
type Schema struct {
	ID          string     `json:"id"` // <owner>.<Name>
	Owner       string     `json:"owner"`
	Name        string     `json:"name"`
	Type        string     `json:"type,omitempty"`
	Description string     `json:"description,omitempty"`
	Properties  []Property `json:"properties"`
	Refs        []string   `json:"refs"` // schemas referenced from anywhere inside this one
	Spec        string     `json:"spec"`
}

// Property is a schema property.
type Property struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Format   string   `json:"format,omitempty"`
	Required bool     `json:"required"`
	Refs     []string `json:"refs,omitempty"`
}

// Table is a table built from goose migrations (Up sections).
type Table struct {
	Name       string   `json:"name"`
	Owners     []string `json:"owners"` // Modules whose queries touch it
	Columns    []Column `json:"columns"`
	Migrations []string `json:"migrations"` // files (relative) that create or alter it
}

// Column is a table column.
type Column struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primary_key,omitempty"`
	References string `json:"references,omitempty"`
}

// Query is a sqlc query.
type Query struct {
	ID     string   `json:"id"`   // sqlc query name
	Kind   string   `json:"kind"` // one, many, exec, ... (the :kind without the colon)
	Module string   `json:"module"`
	File   string   `json:"file"`
	Line   int      `json:"line"`
	Tables []string `json:"tables"`
}

// Symbol is an exported type, function or method (the public surface).
type Symbol struct {
	Module   string `json:"module"`
	Kind     string `json:"kind"` // struct, interface, type, func, method
	Name     string `json:"name"`
	Receiver string `json:"receiver,omitempty"`
	File     string `json:"file"`
	Line     int    `json:"line"`
}

// Function is a function or method with its metrics.
type Function struct {
	ID         string   `json:"id"`
	Module     string   `json:"module"`
	Package    string   `json:"package"` // package dir relative to the project root
	Name       string   `json:"name"`
	Receiver   string   `json:"receiver,omitempty"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	EndLine    int      `json:"end_line"`
	CC         int      `json:"cc"`
	Statements *int     `json:"statements,omitempty"`
	Covered    *int     `json:"covered,omitempty"`
	Coverage   *float64 `json:"coverage,omitempty"`
	CRAP       *float64 `json:"crap,omitempty"`
}

// Warning is a non-fatal problem found during the build.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// Normalize sorts every array and replaces nil slices with empty ones so the
// output is deterministic and never contains null arrays.
func (m *Model) Normalize() {
	m.Levels = nonNil(m.Levels)
	sort.Slice(m.Modules, func(i, j int) bool { return m.Modules[i].ID < m.Modules[j].ID })
	for i := range m.Modules {
		mod := &m.Modules[i]
		sort.Slice(mod.Subpackages, func(a, b int) bool { return mod.Subpackages[a].Dir < mod.Subpackages[b].Dir })
		if mod.Subpackages == nil {
			mod.Subpackages = []Subpackage{}
		}
	}
	sort.Slice(m.Dependencies, func(i, j int) bool {
		a, b := m.Dependencies[i], m.Dependencies[j]
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	for i := range m.Dependencies {
		sort.Strings(m.Dependencies[i].Files)
	}
	sort.Slice(m.Operations, func(i, j int) bool { return m.Operations[i].ID < m.Operations[j].ID })
	for i := range m.Operations {
		o := &m.Operations[i]
		sort.Strings(o.Tags)
		o.Tags = nonNil(o.Tags)
		o.Security = nonNil(o.Security)
		o.RequestSchemas = nonNil(o.RequestSchemas)
		if o.Parameters == nil {
			o.Parameters = []Parameter{}
		}
		sort.Slice(o.Responses, func(a, b int) bool { return o.Responses[a].Status < o.Responses[b].Status })
		if o.Responses == nil {
			o.Responses = []Response{}
		}
		for j := range o.Responses {
			o.Responses[j].Schemas = nonNil(o.Responses[j].Schemas)
		}
	}
	sort.Slice(m.Schemas, func(i, j int) bool { return m.Schemas[i].ID < m.Schemas[j].ID })
	for i := range m.Schemas {
		m.Schemas[i].Refs = nonNil(m.Schemas[i].Refs)
		if m.Schemas[i].Properties == nil {
			m.Schemas[i].Properties = []Property{}
		}
	}
	sort.Slice(m.Tables, func(i, j int) bool { return m.Tables[i].Name < m.Tables[j].Name })
	for i := range m.Tables {
		sort.Strings(m.Tables[i].Owners)
		m.Tables[i].Owners = nonNil(m.Tables[i].Owners)
		m.Tables[i].Migrations = nonNil(m.Tables[i].Migrations)
		if m.Tables[i].Columns == nil {
			m.Tables[i].Columns = []Column{}
		}
	}
	sort.Slice(m.Queries, func(i, j int) bool { return m.Queries[i].ID < m.Queries[j].ID })
	for i := range m.Queries {
		sort.Strings(m.Queries[i].Tables)
		m.Queries[i].Tables = nonNil(m.Queries[i].Tables)
	}
	sort.SliceStable(m.Symbols, func(i, j int) bool {
		a, b := m.Symbols[i], m.Symbols[j]
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	sort.Slice(m.Functions, func(i, j int) bool { return m.Functions[i].ID < m.Functions[j].ID })
	sort.Slice(m.Warnings, func(i, j int) bool {
		a, b := m.Warnings[i], m.Warnings[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Message < b.Message
	})
	if m.Modules == nil {
		m.Modules = []Module{}
	}
	if m.Dependencies == nil {
		m.Dependencies = []Dependency{}
	}
	if m.Operations == nil {
		m.Operations = []Operation{}
	}
	if m.Schemas == nil {
		m.Schemas = []Schema{}
	}
	if m.Tables == nil {
		m.Tables = []Table{}
	}
	if m.Queries == nil {
		m.Queries = []Query{}
	}
	if m.Symbols == nil {
		m.Symbols = []Symbol{}
	}
	if m.Functions == nil {
		m.Functions = []Function{}
	}
	if m.Warnings == nil {
		m.Warnings = []Warning{}
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// JSON normalizes the model and encodes it: indented, trailing newline, no
// HTML escaping.
func (m *Model) JSON() ([]byte, error) {
	m.Normalize()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
