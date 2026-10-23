package extract

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/Kaliki-Labs/archmodel/internal/config"
)

// srcFile is one parsed Go source file that belongs to a configured Module.
type srcFile struct {
	rel       string // path relative to the project root
	test      bool   // _test.go
	generated bool   // carries a "Code generated ... DO NOT EDIT." header
	ast       *ast.File
	fset      *token.FileSet
	module    string // Module ID
	pkgDir    string // package directory relative to the project root
	root      bool   // belongs to the Module's root package
	genPkg    bool   // lives in a package mapped through go.generated
	modDir    string // Module directory relative to the project root
}

// location says where a directory (relative to go.dir) sits in the Module layout.
type location struct {
	module string
	modDir string // Module directory relative to the project root
	pkgDir string // package directory relative to the project root
	root   bool   // the Module's own root package
	genPkg bool   // matched a go.generated mapping
}

// layout resolves directories and import paths to Modules using the config.
type layout struct {
	cfg        *config.Config
	modulePath string // Go module path from go.mod
}

func (l *layout) locate(relDir string) (location, bool) {
	relDir = path.Clean(filepath.ToSlash(relDir))
	for _, g := range l.cfg.Go.Generated {
		gd := path.Clean(g.Dir)
		rest, ok := strings.CutPrefix(relDir, gd+"/")
		if !ok || rest == "" {
			continue
		}
		seg, _, _ := strings.Cut(rest, "/")
		id := strings.ReplaceAll(g.Module, "{name}", seg)
		return location{module: id, modDir: l.moduleDir(id), pkgDir: path.Join(l.cfg.Go.Dir, relDir), genPkg: true}, true
	}
	for _, r := range l.cfg.Go.Roots {
		rd := path.Clean(r.Dir)
		rest, ok := strings.CutPrefix(relDir, rd+"/")
		if !ok || rest == "" {
			continue
		}
		seg, sub, _ := strings.Cut(rest, "/")
		return location{
			module: r.IDPrefix + seg,
			modDir: path.Join(l.cfg.Go.Dir, rd, seg),
			pkgDir: path.Join(l.cfg.Go.Dir, relDir),
			root:   sub == "",
		}, true
	}
	return location{}, false
}

// moduleDir is the directory of a Module ID: the root with the longest
// matching id_prefix.
func (l *layout) moduleDir(id string) string {
	best := -1
	dir := ""
	for _, r := range l.cfg.Go.Roots {
		if strings.HasPrefix(id, r.IDPrefix) && len(r.IDPrefix) > best {
			best = len(r.IDPrefix)
			dir = path.Join(l.cfg.Go.Dir, r.Dir, strings.TrimPrefix(id, r.IDPrefix))
		}
	}
	return dir
}

// locateImport resolves an import path inside this Go module.
func (l *layout) locateImport(imp string) (location, bool) {
	rest, ok := strings.CutPrefix(imp, l.modulePath+"/")
	if !ok {
		return location{}, false
	}
	return l.locate(rest)
}

var reModule = regexp.MustCompile(`(?m)^\s*module\s+("?)([^\s"]+)("?)`)

func readModulePath(goDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(goDir, "go.mod"))
	if err != nil {
		return "", err
	}
	m := reModule.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("%s/go.mod: no module directive", goDir)
	}
	return string(m[2]), nil
}

var reGenerated = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

func isGenerated(f *ast.File) bool {
	for _, cg := range f.Comments {
		if cg.Pos() > f.Package {
			break
		}
		for _, c := range cg.List {
			if reGenerated.MatchString(c.Text) {
				return true
			}
		}
	}
	return false
}

// loadFiles runs go/packages over the Go module (Tests: true, so _test.go
// files and external test packages are included) and returns the parsed
// source files that belong to configured Modules. Load problems become
// warnings (ADR-0003): missing go.sum entries, missing generated packages and
// the like never abort the build.
func loadFiles(root string, l *layout) ([]*srcFile, []string, error) {
	goDir, err := filepath.Abs(filepath.Join(root, l.cfg.Go.Dir))
	if err != nil {
		return nil, nil, err
	}
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedSyntax,
		Dir:   goDir,
		Fset:  fset,
		Tests: true,
		// Never touch the network or write go.mod/go.sum in the consumer's tree.
		Env: append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local"),
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, nil, fmt.Errorf("loading Go packages in %s: %w", goDir, err)
	}
	dirs := []string{goDir}
	if real, err := filepath.EvalSymlinks(goDir); err == nil && real != goDir {
		dirs = append(dirs, real)
	}
	relToGo := func(abs string) (string, bool) {
		for _, d := range dirs {
			if r, err := filepath.Rel(d, abs); err == nil && !strings.HasPrefix(r, "..") {
				return filepath.ToSlash(r), true
			}
		}
		return "", false
	}

	var problems []string
	seenProblem := map[string]bool{}
	seen := map[string]bool{}
	var files []*srcFile
	for _, p := range pkgs {
		for _, e := range p.Errors {
			msg := e.Msg
			if e.Pos != "" {
				msg = e.Pos + ": " + msg
			}
			for _, d := range dirs {
				msg = strings.ReplaceAll(msg, d+string(filepath.Separator), "")
				msg = strings.ReplaceAll(msg, d, ".")
			}
			if !seenProblem[msg] {
				seenProblem[msg] = true
				problems = append(problems, msg)
			}
		}
		names := p.CompiledGoFiles
		if len(names) != len(p.Syntax) {
			names = p.GoFiles
		}
		if len(names) != len(p.Syntax) {
			continue
		}
		for i, abs := range names {
			rel, ok := relToGo(abs)
			if !ok || seen[rel] {
				continue
			}
			loc, ok := l.locate(path.Dir(rel))
			if !ok {
				continue
			}
			seen[rel] = true
			f := p.Syntax[i]
			files = append(files, &srcFile{
				rel:       path.Join(l.cfg.Go.Dir, rel),
				test:      strings.HasSuffix(rel, "_test.go"),
				generated: isGenerated(f),
				ast:       f,
				fset:      fset,
				module:    loc.module,
				pkgDir:    loc.pkgDir,
				root:      loc.root,
				genPkg:    loc.genPkg,
				modDir:    loc.modDir,
			})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	sort.Strings(problems)
	return files, problems, nil
}

// importsOf lists the import paths of a file.
func importsOf(f *ast.File) []string {
	var out []string
	for _, s := range f.Imports {
		if p, err := strconv.Unquote(s.Path.Value); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// recvName returns the receiver type name of a method ("" for functions).
func recvName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

// Cyclomatic computes McCabe cyclomatic complexity of a function body: 1 plus
// one per if, for, range, non-default case, non-default select case, && and ||.
// Function literals inside the body count toward the enclosing function.
func Cyclomatic(body *ast.BlockStmt) int {
	cc := 1
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			cc++
		case *ast.CaseClause:
			if n.List != nil {
				cc++
			}
		case *ast.CommClause:
			if n.Comm != nil {
				cc++
			}
		case *ast.BinaryExpr:
			if n.Op == token.LAND || n.Op == token.LOR {
				cc++
			}
		}
		return true
	})
	return cc
}

// typeKind classifies a type spec for the public surface.
func typeKind(ts *ast.TypeSpec) string {
	if ts.Assign.IsValid() {
		return "type"
	}
	switch ts.Type.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	}
	return "type"
}
