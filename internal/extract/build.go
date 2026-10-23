// Package extract builds the Architecture model from a project tree: Go
// packages, OpenAPI specs, goose migrations, sqlc queries, metrics and
// coverage.
package extract

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Options configure a build.
type Options struct {
	Root     string // project root
	Config   *config.Config
	Coverage io.Reader // optional Go coverprofile
}

// Build extracts the Architecture model.
func Build(opts Options) (*model.Model, error) {
	cfg := opts.Config
	goDir := filepath.Join(opts.Root, cfg.Go.Dir)
	modulePath, err := readModulePath(goDir)
	if err != nil {
		return nil, err
	}
	b := &builder{
		cfg: cfg, root: opts.Root,
		lay:  &layout{cfg: cfg, modulePath: modulePath},
		mods: map[string]*modInfo{},
		m:    &model.Model{Version: model.Version, Levels: cfg.Levels},
	}
	if opts.Coverage != nil {
		if b.profile, err = ParseCoverProfile(opts.Coverage); err != nil {
			return nil, err
		}
	}
	files, problems, err := loadFiles(opts.Root, b.lay)
	if err != nil {
		return nil, err
	}
	for _, p := range problems {
		b.warn("go-load", p, "")
	}
	b.collectModules(files)
	b.collectDependencies(files)
	b.classify()
	b.collectCode(files)
	b.collectAPI()
	b.collectSQL()
	b.linkOperations()
	b.computeMetrics()
	b.m.Normalize()
	return b.m, nil
}

type modInfo struct {
	id    string
	dir   string
	files []*srcFile
	subs  map[string]*model.Subpackage // by project-relative dir
}

type depKey struct{ from, to string }

type depInfo struct {
	files    map[string]bool // rel path -> test file?
	allTests bool
}

type builder struct {
	cfg     *config.Config
	root    string
	lay     *layout
	profile Profile
	mods    map[string]*modInfo
	deps    map[depKey]*depInfo
	kinds   map[string]string
	m       *model.Model
	funcs   []model.Function
}

func (b *builder) warn(code, msg, p string) {
	b.m.Warnings = append(b.m.Warnings, model.Warning{Code: code, Message: msg, Path: p})
}

func (b *builder) mod(id, dir string) *modInfo {
	mi := b.mods[id]
	if mi == nil {
		mi = &modInfo{id: id, dir: dir, subs: map[string]*model.Subpackage{}}
		b.mods[id] = mi
	}
	return mi
}

func (b *builder) collectModules(files []*srcFile) {
	for _, f := range files {
		mi := b.mod(f.module, f.modDir)
		mi.files = append(mi.files, f)
		if !f.root {
			b.sub(mi, f.pkgDir)
		}
	}
}

func (b *builder) sub(mi *modInfo, dir string) *model.Subpackage {
	sp := mi.subs[dir]
	if sp == nil {
		sp = &model.Subpackage{Dir: dir, Name: path.Base(dir)}
		mi.subs[dir] = sp
	}
	return sp
}

// collectDependencies derives module-to-module imports file by file, and
// creates "missing" Modules and subpackages for imports of packages that are
// under a configured root but not on disk (ADR-0003).
func (b *builder) collectDependencies(files []*srcFile) {
	b.deps = map[depKey]*depInfo{}
	onDisk := map[string]bool{}
	for _, f := range files {
		onDisk[f.pkgDir] = true
	}
	for _, f := range files {
		for _, imp := range importsOf(f.ast) {
			loc, ok := b.lay.locateImport(imp)
			if !ok {
				continue
			}
			target := b.mod(loc.module, loc.modDir)
			if !onDisk[loc.pkgDir] {
				if !loc.root {
					b.sub(target, loc.pkgDir)
				}
			}
			if loc.module == f.module {
				continue
			}
			k := depKey{f.module, loc.module}
			d := b.deps[k]
			if d == nil {
				d = &depInfo{files: map[string]bool{}, allTests: true}
				b.deps[k] = d
			}
			d.files[f.rel] = f.test
			if !f.test {
				d.allTests = false
			}
		}
	}
}

func matchAny(patterns []string, id string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, id); ok {
			return true
		}
	}
	return false
}

// classify assigns a kind to every Module by convention (BRIEF "Kind").
// Precedence: explicit override, Composition root list, Foundation list,
// Test-only (ADR-0007: no prod importer, and a test file imports it or its own
// non-test files import "testing"), Domain (spec exists), else
// Infrastructure.
func (b *builder) classify() {
	prod, test := map[string]bool{}, map[string]bool{}
	for _, mi := range b.mods {
		for _, f := range mi.files {
			if f.test || f.generated {
				continue
			}
			for _, imp := range importsOf(f.ast) {
				if imp == "testing" {
					test[mi.id] = true
				}
			}
		}
	}
	for k, d := range b.deps {
		if d.allTests {
			test[k.to] = true
		} else {
			prod[k.to] = true
		}
	}
	b.kinds = map[string]string{}
	for id := range b.mods {
		switch {
		case b.cfg.Kinds.Overrides[id] != "":
			b.kinds[id] = b.cfg.Kinds.Overrides[id]
		case matchAny(b.cfg.Kinds.CompositionRoot, id):
			b.kinds[id] = model.KindCompositionRoot
		case matchAny(b.cfg.Kinds.Foundation, id):
			b.kinds[id] = model.KindFoundation
		case test[id] && !prod[id]:
			b.kinds[id] = model.KindTestOnly
		case !strings.Contains(id, "/") && b.specExists(id):
			b.kinds[id] = model.KindDomain
		default:
			b.kinds[id] = model.KindInfrastructure
		}
	}
}

func (b *builder) specPath(name string) string {
	return filepath.Join(b.root, b.cfg.API.SpecsDir, name+".yaml")
}

func (b *builder) specExists(name string) bool {
	st, err := os.Stat(b.specPath(name))
	return err == nil && !st.IsDir()
}

func (b *builder) level(kind string) *int {
	for i, l := range b.cfg.Levels {
		if l == kind {
			return &i
		}
	}
	return nil
}

// collectCode builds modules, subpackages, symbols and functions from source.
func (b *builder) collectCode(files []*srcFile) {
	ids := make([]string, 0, len(b.mods))
	for id := range b.mods {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	seenFn := map[string]int{}
	for _, id := range ids {
		mi := b.mods[id]
		mod := model.Module{
			ID: id, Kind: b.kinds[id], Level: b.level(b.kinds[id]), Dir: mi.dir,
			Present: len(mi.files) > 0,
		}
		if b.kinds[id] == model.KindTestOnly {
			mod.Level = nil
		}
		rootProd, rootGen := 0, 0
		subDoc := map[string]string{}
		subSeen := map[string]bool{}
		for _, f := range mi.files {
			if f.root && !f.test {
				rootProd++
				if f.generated {
					rootGen++
				} else if mod.Purpose == "" {
					mod.Purpose = docText(f.ast)
					mod.Summary = new(doc.Package).Synopsis(mod.Purpose)
				}
			}
			if !f.root {
				subSeen[f.pkgDir] = true
				if !f.test && !f.generated && subDoc[f.pkgDir] == "" {
					subDoc[f.pkgDir] = docText(f.ast)
				}
			}
			b.collectFileCode(&mod, mi, f, seenFn)
		}
		// Generated: every root source file carries a "Code generated" header,
		// or the package is missing (assumed generated, ADR-0003).
		mod.Generated = !mod.Present || (rootProd > 0 && rootGen == rootProd)
		for dir, sp := range mi.subs {
			s := *sp
			s.Present = subSeen[dir]
			if s.Present {
				s.Generated = b.subGenerated(mi, dir)
				s.Purpose = subDoc[dir]
			} else {
				s.Generated = true
			}
			mod.Subpackages = append(mod.Subpackages, s)
		}
		b.m.Modules = append(b.m.Modules, mod)
	}
	sort.Slice(b.funcs, func(i, j int) bool { return b.funcs[i].ID < b.funcs[j].ID })
	b.m.Functions = b.funcs
}

// subGenerated: every non-test file of the subpackage is generated, or the
// package is mapped through go.generated.
func (b *builder) subGenerated(mi *modInfo, dir string) bool {
	any := false
	for _, f := range mi.files {
		if f.pkgDir != dir || f.test {
			continue
		}
		if f.genPkg {
			return true
		}
		any = true
		if !f.generated {
			return false
		}
	}
	return any
}

func docText(f *ast.File) string {
	if f.Doc == nil {
		return ""
	}
	return strings.TrimSpace(f.Doc.Text())
}

// collectFileCode records the public surface and function metrics of one file.
// Test files and files in go.generated packages contribute nothing; files with
// a "Code generated" header contribute to the surface but not to function
// metrics.
func (b *builder) collectFileCode(mod *model.Module, mi *modInfo, f *srcFile, seenFn map[string]int) {
	if f.test || f.genPkg {
		return
	}
	pos := func(p token.Pos) int { return f.fset.Position(p).Line }
	pkgLabel := mi.id
	if !f.root {
		pkgLabel = mi.id + "/" + strings.TrimPrefix(f.pkgDir, mi.dir+"/")
	}
	for _, decl := range f.ast.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, s := range d.Specs {
				ts := s.(*ast.TypeSpec)
				if ts.Name.IsExported() {
					b.m.Symbols = append(b.m.Symbols, model.Symbol{
						Module: mi.id, Kind: typeKind(ts), Name: ts.Name.Name, File: f.rel, Line: pos(ts.Name.Pos()),
					})
				}
			}
		case *ast.FuncDecl:
			recv := recvName(d)
			if d.Name.IsExported() && (d.Recv == nil || ast.IsExported(recv)) {
				kind := "func"
				if d.Recv != nil {
					kind = "method"
				}
				b.m.Symbols = append(b.m.Symbols, model.Symbol{
					Module: mi.id, Kind: kind, Name: d.Name.Name, Receiver: recv, File: f.rel, Line: pos(d.Name.Pos()),
				})
			}
			if f.generated || d.Body == nil {
				continue
			}
			id := pkgLabel + "." + d.Name.Name
			if recv != "" {
				id = pkgLabel + "." + recv + "." + d.Name.Name
			}
			if n := seenFn[id]; n > 0 {
				seenFn[id]++
				id = fmt.Sprintf("%s#%d", id, n+1)
			} else {
				seenFn[id] = 1
			}
			fn := model.Function{
				ID: id, Module: mi.id, Package: f.pkgDir, Name: d.Name.Name, Receiver: recv,
				File: f.rel, Line: pos(d.Pos()), EndLine: pos(d.End()), CC: Cyclomatic(d.Body),
			}
			b.applyCoverage(&fn)
			b.funcs = append(b.funcs, fn)
		}
	}
}

// applyCoverage fills coverage and CRAP when a coverprofile was supplied. A
// function whose file is absent from the profile counts as uncovered; a
// function in a profiled file with no statement blocks (empty body) counts as
// fully covered, as there is nothing to cover.
func (b *builder) applyCoverage(fn *model.Function) {
	if b.profile == nil {
		return
	}
	key := b.lay.modulePath + "/" + strings.TrimPrefix(fn.File, b.cfg.Go.Dir+"/")
	stmts, covered, ok := b.profile.FuncCoverage(key, fn.Line, fn.EndLine)
	cov := 0.0
	switch {
	case !ok:
	case stmts == 0:
		cov = 1
		fn.Statements, fn.Covered = &stmts, &covered
	default:
		cov = float64(covered) / float64(stmts)
		fn.Statements, fn.Covered = &stmts, &covered
	}
	cov, crap := coverageScores(fn.CC, cov)
	fn.Coverage, fn.CRAP = &cov, &crap
}

// coverageScores rounds coverage and CRAP for the model. CRAP is computed from
// the exact coverage: rounding first would shift it by up to a few points.
func coverageScores(cc int, cov float64) (float64, float64) {
	return Round(cov), Round(CRAP(cc, cov))
}

// collectAPI reads OpenAPI specs.
func (b *builder) collectAPI() {
	dir := filepath.Join(b.root, b.cfg.API.SpecsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.warn("api-specs-missing", "no OpenAPI specs found in "+b.cfg.API.SpecsDir, b.cfg.API.SpecsDir)
		return
	}
	seenOp := map[string]string{}
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if e.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		owner := strings.TrimSuffix(e.Name(), ext)
		rel := path.Join(b.cfg.API.SpecsDir, e.Name())
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			b.warn("api-spec-read", err.Error(), rel)
			continue
		}
		res, err := ParseSpec(owner, rel, data)
		if err != nil {
			b.warn("api-spec-parse", err.Error(), rel)
			continue
		}
		if b.mods[owner] == nil {
			b.warn("api-spec-without-module", fmt.Sprintf("spec declares Domain %q but no such Module exists", owner), rel)
		}
		for _, op := range res.Operations {
			if prev, dup := seenOp[op.ID]; dup {
				b.warn("api-duplicate-operation", fmt.Sprintf("operationId %q is declared in %s and %s", op.ID, prev, rel), rel)
				continue
			}
			seenOp[op.ID] = rel
			b.m.Operations = append(b.m.Operations, op)
		}
		b.m.Schemas = append(b.m.Schemas, res.Schemas...)
		b.m.Warnings = append(b.m.Warnings, res.Warnings...)
	}
}

// linkOperations links each operation to the method on the Domain's
// implementer type whose name is the operationId in Go casing. Purely
// syntactic (ADR-0003): it does not need the generated ServerInterface.
func (b *builder) linkOperations() {
	byMethod := map[string]*model.Function{}
	for i := range b.funcs {
		fn := &b.funcs[i]
		if fn.Receiver == b.cfg.API.Implementer {
			byMethod[fn.Module+"."+fn.Name] = fn
		}
	}
	for i := range b.m.Operations {
		op := &b.m.Operations[i]
		fn := byMethod[op.Owner+"."+GoName(op.ID)]
		if fn == nil {
			b.warn("api-operation-unimplemented",
				fmt.Sprintf("operation %q has no method %s.%s in Module %q", op.ID, b.cfg.API.Implementer, GoName(op.ID), op.Owner), op.Spec)
			continue
		}
		op.Implementation = &model.Implementation{Function: fn.ID, File: fn.File, Line: fn.Line}
	}
}

// collectSQL reads goose migrations and sqlc query files.
func (b *builder) collectSQL() {
	cat := NewCatalog()
	schemaDir := filepath.Join(b.root, b.cfg.SQL.SchemaDir)
	for _, name := range sqlFiles(schemaDir) {
		data, err := os.ReadFile(filepath.Join(schemaDir, name))
		if err != nil {
			b.warn("sql-read", err.Error(), path.Join(b.cfg.SQL.SchemaDir, name))
			continue
		}
		cat.ApplyMigration(path.Join(b.cfg.SQL.SchemaDir, name), string(data))
	}
	tables := cat.Tables()
	owners := map[string]map[string]bool{}
	queriesDir := filepath.Join(b.root, b.cfg.SQL.QueriesDir)
	seen := map[string]string{}
	for _, name := range sqlFiles(queriesDir) {
		rel := path.Join(b.cfg.SQL.QueriesDir, name)
		data, err := os.ReadFile(filepath.Join(queriesDir, name))
		if err != nil {
			b.warn("sql-read", err.Error(), rel)
			continue
		}
		base := strings.TrimSuffix(name, ".sql")
		module := base
		if o := b.cfg.SQL.QueryModules[base]; o != "" {
			module = o
		}
		if b.mods[module] == nil {
			b.warn("sql-query-module-unknown", fmt.Sprintf("queries in %s are attributed to %q, which is not a Module (map it with sql.query_modules)", name, module), rel)
		}
		for _, q := range ParseQueries(string(data), cat.Has) {
			if prev, dup := seen[q.Name]; dup {
				b.warn("sql-duplicate-query", fmt.Sprintf("query %q is defined in %s and %s", q.Name, prev, rel), rel)
				continue
			}
			seen[q.Name] = rel
			b.m.Queries = append(b.m.Queries, model.Query{
				ID: q.Name, Kind: q.Kind, Module: module, File: rel, Line: q.Line, Tables: q.Tables,
			})
			for _, t := range q.Tables {
				if owners[t] == nil {
					owners[t] = map[string]bool{}
				}
				owners[t][module] = true
			}
		}
	}
	for i := range tables {
		tables[i].Owners = sortedKeys(owners[tables[i].Name])
	}
	b.m.Tables = tables
	if len(tables) == 0 {
		if _, err := os.Stat(schemaDir); err != nil {
			b.warn("sql-schema-missing", "no migrations found in "+b.cfg.SQL.SchemaDir, b.cfg.SQL.SchemaDir)
		}
	}
}

func sqlFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// computeMetrics fills Dependencies and per-module Metrics (production graph
// only; test-only Modules are outside it).
func (b *builder) computeMetrics() {
	for k, d := range b.deps {
		b.m.Dependencies = append(b.m.Dependencies, model.Dependency{
			From: k.from, To: k.to, TestOnly: d.allTests, Files: sortedKeys(d.files),
		})
	}
	sort.Slice(b.m.Dependencies, func(i, j int) bool {
		a, c := b.m.Dependencies[i], b.m.Dependencies[j]
		if a.From != c.From {
			return a.From < c.From
		}
		return a.To < c.To
	})
	ca, ce := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, d := range b.m.Dependencies {
		if d.TestOnly || b.kinds[d.From] == model.KindTestOnly || b.kinds[d.To] == model.KindTestOnly {
			continue
		}
		if ce[d.From] == nil {
			ce[d.From] = map[string]bool{}
		}
		if ca[d.To] == nil {
			ca[d.To] = map[string]bool{}
		}
		ce[d.From][d.To] = true
		ca[d.To][d.From] = true
	}
	types, abstract := map[string]int{}, map[string]int{}
	for _, s := range b.m.Symbols {
		switch s.Kind {
		case "struct", "type":
			types[s.Module]++
		case "interface":
			types[s.Module]++
			abstract[s.Module]++
		}
	}
	for i := range b.m.Modules {
		mod := &b.m.Modules[i]
		if mod.Kind == model.KindTestOnly {
			continue
		}
		m := ComputeMetrics(len(ca[mod.ID]), len(ce[mod.ID]), types[mod.ID], abstract[mod.ID])
		b.rollupFunctions(mod.ID, m)
		mod.Metrics = m
	}
}

// rollupFunctions adds the worst-function figures to a module's metrics.
func (b *builder) rollupFunctions(id string, m *model.Metrics) {
	var stmts, covered int
	for i := range b.m.Functions {
		fn := &b.m.Functions[i]
		if fn.Module != id {
			continue
		}
		if fn.CC > m.WorstCC || (fn.CC == m.WorstCC && m.WorstCCFunction == "") {
			m.WorstCC, m.WorstCCFunction = fn.CC, fn.ID
		}
		if fn.CRAP != nil && (m.WorstCRAP == nil || *fn.CRAP > *m.WorstCRAP) {
			v := *fn.CRAP
			m.WorstCRAP, m.WorstCRAPFunction = &v, fn.ID
		}
		if fn.Statements != nil {
			stmts += *fn.Statements
			covered += *fn.Covered
		}
	}
	if b.profile != nil && stmts > 0 {
		c := Round(float64(covered) / float64(stmts))
		m.Coverage = &c
	}
}
