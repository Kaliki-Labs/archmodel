package extract

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Query footprint (ADR-0013): for each operation, walk its handler's code and
// find every sqlc query it reaches, and whether any runs inside a loop. It is
// purely syntactic like operation linking (ADR-0003): a call is a query call
// when the method name is a known sqlc query name (sqlc names the generated
// method after the query), so it needs neither generated code nor type
// information. It says "this is a candidate", never "this is slow": it cannot
// know how many iterations a loop runs, so a loop around a query is reported as
// "1 + N", and a probe (--probe) is what confirms it.

const maxFootprintDepth = 8

// fnDecl is a function body the footprint walker can step into.
type fnDecl struct {
	decl *ast.FuncDecl
	file *srcFile
	recv string // receiver type name, "" for a function
	rcv  string // receiver identifier ("s" in func (s *Service) ...)
}

type fnIndex struct {
	byFileLine map[string]*fnDecl   // "rel:line" of the func keyword
	byMethod   map[string]*fnDecl   // pkgDir|recv|name
	byName     map[string][]*fnDecl // module|name
	fields     map[string]string    // pkgDir|Type|field -> field type name ("" if not a plain named type)
}

func buildFnIndex(files []*srcFile) *fnIndex {
	ix := &fnIndex{byFileLine: map[string]*fnDecl{}, byMethod: map[string]*fnDecl{}, byName: map[string][]*fnDecl{}, fields: map[string]string{}}
	for _, f := range files {
		if f.test || f.generated || f.genPkg {
			continue
		}
		for _, d := range f.ast.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, sp := range gd.Specs {
					ts := sp.(*ast.TypeSpec)
					if st, ok := ts.Type.(*ast.StructType); ok {
						for _, fl := range st.Fields.List {
							for _, n := range fl.Names {
								ix.fields[f.pkgDir+"|"+ts.Name.Name+"|"+n.Name] = namedType(fl.Type)
							}
						}
					}
				}
				continue
			}
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			e := &fnDecl{decl: fd, file: f, recv: recvName(fd)}
			if fd.Recv != nil && len(fd.Recv.List) > 0 && len(fd.Recv.List[0].Names) > 0 {
				e.rcv = fd.Recv.List[0].Names[0].Name
			}
			ix.byFileLine[f.rel+":"+strconv.Itoa(f.fset.Position(fd.Pos()).Line)] = e
			ix.byMethod[f.pkgDir+"|"+e.recv+"|"+fd.Name.Name] = e
			k := f.module + "|" + fd.Name.Name
			ix.byName[k] = append(ix.byName[k], e)
		}
	}
	return ix
}

// namedType returns T for T and *T, else "".
func namedType(e ast.Expr) string {
	if st, ok := e.(*ast.StarExpr); ok {
		e = st.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

type loopCtx struct {
	inLoop bool
	times  int
}

type footprintWalker struct {
	ix      *fnIndex
	queries map[string]bool
	out     []model.OperationQuery
	stack   map[*ast.FuncDecl]bool
	via     []string
}

// collectFootprints fills Operation.SQL for every operation with a linked handler.
func (b *builder) collectFootprints(files []*srcFile) {
	if len(b.m.Queries) == 0 {
		return
	}
	ix := buildFnIndex(files)
	ids := map[string]bool{}
	for _, q := range b.m.Queries {
		ids[q.ID] = true
	}
	for i := range b.m.Operations {
		op := &b.m.Operations[i]
		if op.Implementation == nil {
			continue
		}
		fd := ix.byFileLine[op.Implementation.File+":"+strconv.Itoa(op.Implementation.Line)]
		if fd == nil {
			continue
		}
		w := &footprintWalker{ix: ix, queries: ids, stack: map[*ast.FuncDecl]bool{}}
		w.walkFn(fd, loopCtx{times: 1})
		op.SQL = b.summarizeFootprint(op, w.out)
	}
}

func (w *footprintWalker) walkFn(fd *fnDecl, ctx loopCtx) {
	if w.stack[fd.decl] || len(w.stack) >= maxFootprintDepth {
		return
	}
	w.stack[fd.decl] = true
	w.via = append(w.via, fd.decl.Name.Name)
	w.walkNode(fd.decl.Body, fd, ctx)
	w.via = w.via[:len(w.via)-1]
	delete(w.stack, fd.decl)
}

// literalBound returns n for "for i := 0; i < n; i++" and "range n" with a small
// integer literal n, else 0.
func literalBound(n ast.Node) int {
	lit := func(e ast.Expr) int {
		if bl, ok := e.(*ast.BasicLit); ok && bl.Kind == token.INT {
			if v, err := strconv.Atoi(bl.Value); err == nil && v > 0 && v <= 10 {
				return v
			}
		}
		return 0
	}
	switch s := n.(type) {
	case *ast.ForStmt:
		if be, ok := s.Cond.(*ast.BinaryExpr); ok && (be.Op == token.LSS || be.Op == token.LEQ) {
			v := lit(be.Y)
			if v > 0 && be.Op == token.LEQ {
				v++
			}
			return v
		}
	case *ast.RangeStmt:
		return lit(s.X)
	}
	return 0
}

func (w *footprintWalker) walkNode(root ast.Node, fd *fnDecl, ctx loopCtx) {
	ast.Inspect(root, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			inner := ctx
			if v := literalBound(s); v > 0 {
				inner.times *= v
			} else {
				inner.inLoop = true
			}
			switch l := s.(type) {
			case *ast.ForStmt:
				if l.Init != nil {
					w.walkNode(l.Init, fd, ctx)
				}
				if l.Cond != nil {
					w.walkNode(l.Cond, fd, ctx)
				}
				w.walkNode(l.Body, fd, inner)
			case *ast.RangeStmt:
				w.walkNode(l.X, fd, ctx)
				w.walkNode(l.Body, fd, inner)
			}
			return false
		case *ast.CallExpr:
			w.call(s, fd, ctx)
		}
		return true
	})
}

func (w *footprintWalker) call(c *ast.CallExpr, fd *fnDecl, ctx loopCtx) {
	var name string
	var viaRecv, bare bool
	switch f := c.Fun.(type) {
	case *ast.SelectorExpr:
		name = f.Sel.Name
		if id, ok := f.X.(*ast.Ident); ok && fd.rcv != "" && id.Name == fd.rcv {
			viaRecv = true
		}
	case *ast.Ident:
		name, bare = f.Name, true
	default:
		return
	}
	// A call on the receiver itself (s.helper) is the handler's own helper even
	// if it shares a name with a query; any other X.Name that is a sqlc query
	// name is a query call.
	own := w.ix.byMethod[fd.file.pkgDir+"|"+fd.recv+"|"+name]
	// s.store.X where the store type has its own method X (a wrapper that may
	// run several queries, or a transaction) is walked into, even when X is
	// also a query name. Generated code is never walked: it is the query.
	if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
		if _, nested := sel.X.(*ast.SelectorExpr); nested {
			if m := w.fieldMethod(sel, name, fd); m != nil && !m.file.generated && w.queries[name] {
				// Only a wrapper that itself reaches queries replaces the call;
				// a bare stub of the query is the query.
				n := len(w.out)
				w.walkFn(m, ctx)
				if len(w.out) > n {
					return
				}
			}
		}
	}
	if !bare && !(viaRecv && own != nil) && w.queries[name] {
		pos := fd.file.fset.Position(c.Pos())
		w.out = append(w.out, model.OperationQuery{
			Query: name, InLoop: ctx.inLoop, Times: ctx.times,
			Via: append([]string(nil), w.via...), File: fd.file.rel, Line: pos.Line,
		})
		return
	}
	var target *fnDecl
	switch {
	case viaRecv:
		target = own
	case bare:
		target = w.ix.byMethod[fd.file.pkgDir+"||"+name]
	default:
		target = w.resolveSelector(c.Fun.(*ast.SelectorExpr), name, fd)
	}
	if target != nil {
		w.walkFn(target, ctx)
	}
}

var footprintRationale = map[string]string{
	"n-plus-one":       "A query inside a loop runs once per item, so one request costs 1 + N round trips and gets slower as the data grows. This is the classic N+1.",
	"too-many-queries": "Every query is a database round trip. A handler that runs many of them per request pays for each, and tends to hide a missing JOIN or batch query.",
}

func (b *builder) summarizeFootprint(op *model.Operation, qs []model.OperationQuery) *model.OperationSQL {
	s := &model.OperationSQL{Queries: qs, Findings: []model.Finding{}}
	var perItem []model.OperationQuery
	for _, q := range qs {
		if q.InLoop {
			s.PerItem += q.Times
			perItem = append(perItem, q)
		} else {
			s.Fixed += q.Times
		}
	}
	switch {
	case s.PerItem == 0:
		s.Estimate = strconv.Itoa(s.Fixed)
	case s.PerItem == 1:
		s.Estimate = fmt.Sprintf("%d + N", s.Fixed)
	default:
		s.Estimate = fmt.Sprintf("%d + %dN", s.Fixed, s.PerItem)
	}
	if len(perItem) > 0 {
		var names []string
		seen := map[string]bool{}
		for _, q := range perItem {
			if !seen[q.Query] {
				seen[q.Query] = true
				names = append(names, q.Query)
			}
		}
		list := strings.Join(names, ", ")
		if len(names) > 4 {
			list = strings.Join(names[:4], ", ") + fmt.Sprintf(" and %d more", len(names)-4)
		}
		first := perItem[0]
		s.Findings = append(s.Findings, model.Finding{
			Code: "n-plus-one", Severity: "warn",
			Message:   fmt.Sprintf("%s run inside a loop (first at %s:%d), once per item: %s queries per request", list, first.File, first.Line, s.Estimate),
			Rationale: footprintRationale["n-plus-one"],
			Fix:       "fetch the rows in one query (WHERE id = ANY($1), or a JOIN) and group them in Go; confirm with a probe",
		})
	}
	if max := b.cfg.SQL.MaxQueriesPerOperation; max > 0 && s.Fixed > max {
		s.Findings = append(s.Findings, model.Finding{
			Code: "too-many-queries", Severity: "info",
			Message:   fmt.Sprintf("the handler can run up to %d queries per request, counting every branch (limit %d)", s.Fixed, max),
			Rationale: footprintRationale["too-many-queries"],
			Fix:       "merge related reads into one query with a JOIN, or move independent work off the request path",
		})
	}
	return s
}

// resolveSelector finds the function behind x.Name(...). With x = s.field it
// uses the receiver struct's field type; otherwise it accepts a name that is
// unique in the Module once the caller's own type is excluded (so
// s.store.DeleteSection is not mistaken for the handler's own DeleteSection).
// fieldMethod resolves s.field.Name(...) through the receiver struct's field
// type; nil when the receiver, field or method is not found in the package.
func (w *footprintWalker) fieldMethod(sel *ast.SelectorExpr, name string, fd *fnDecl) *fnDecl {
	inner, ok := sel.X.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	id, ok := inner.X.(*ast.Ident)
	if !ok || fd.rcv == "" || id.Name != fd.rcv {
		return nil
	}
	t := w.ix.fields[fd.file.pkgDir+"|"+fd.recv+"|"+inner.Sel.Name]
	if t == "" {
		return nil
	}
	return w.ix.byMethod[fd.file.pkgDir+"|"+t+"|"+name]
}

func (w *footprintWalker) resolveSelector(sel *ast.SelectorExpr, name string, fd *fnDecl) *fnDecl {
	if inner, ok := sel.X.(*ast.SelectorExpr); ok {
		if id, ok := inner.X.(*ast.Ident); ok && fd.rcv != "" && id.Name == fd.rcv {
			if t := w.ix.fields[fd.file.pkgDir+"|"+fd.recv+"|"+inner.Sel.Name]; t != "" {
				if m := w.ix.byMethod[fd.file.pkgDir+"|"+t+"|"+name]; m != nil {
					return m
				}
			}
		}
	}
	// pkg.Name where pkg is an import is outside this Module's own functions;
	// never match it to a same-named function here (uuid.New, errors.New).
	if id, ok := sel.X.(*ast.Ident); ok && isImportName(fd.file.ast, id.Name) {
		return nil
	}
	var cands []*fnDecl
	for _, c := range w.ix.byName[fd.file.module+"|"+name] {
		if c.recv != fd.recv || fd.recv == "" {
			cands = append(cands, c)
		}
	}
	if len(cands) == 1 {
		return cands[0]
	}
	return nil
}

// isImportName reports whether name is the local name of an import in f.
func isImportName(f *ast.File, name string) bool {
	for _, im := range f.Imports {
		if im.Name != nil {
			if im.Name.Name == name {
				return true
			}
			continue
		}
		path := strings.Trim(im.Path.Value, `"`)
		base := path[strings.LastIndex(path, "/")+1:]
		if base == name || (strings.HasPrefix(base, "v") && len(base) <= 3 && strings.Contains(path, "/"+name+"/")) {
			return true
		}
	}
	return false
}
