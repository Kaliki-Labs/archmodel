package extract

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// ScoreQuery rates one sqlc query statically: it reads the statement text and
// the indexes of the tables it touches, and never talks to a database. It
// flags patterns that usually cost a sequential scan, an unbounded result or
// a full sort. A table's row count is unknown, so findings are "this will not
// scale" signals rather than measured slowness.
//
// The analysis is a clause scanner, not a SQL parser. It reads the top-level
// clauses of each statement (and of each CTE body), judges filters and sorts
// on the statement's primary table only, and stays silent when it cannot tell
// (views, CASE filters, joins it does not model). A wrong finding costs more
// trust than a missed one.
func ScoreQuery(q ParsedQuery, tables map[string]model.Table) model.QueryScore {
	findings := analyze(q, tables)
	score := 100
	for _, f := range findings {
		score -= f.Penalty
	}
	if score < 0 {
		score = 0
	}
	if findings == nil {
		findings = []model.Finding{}
	}
	return model.QueryScore{Value: score, Grade: gradeFor(score), Findings: findings}
}

func gradeFor(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 55:
		return "C"
	}
	return "D"
}

// scoreRationale holds the Rationale for each finding. Like rule Rationales,
// the same text is shown wherever the finding appears.
var scoreRationale = map[string]string{
	"unbounded-write":    "An UPDATE or DELETE with no WHERE rewrites or removes every row. It is almost never intended and cannot be undone.",
	"unindexed-filter":   "With no index leading on a filtered column the database reads every row to find matches. That is invisible on a small table and slow on a large one.",
	"unindexed-sort":     "ORDER BY on a column no index can deliver in order makes the database sort the whole result first, even when you only want a few rows.",
	"unbounded-list":     "A :many query with no LIMIT returns every matching row, so response size and memory grow with the table.",
	"offset-pagination":  "OFFSET still reads and discards every skipped row, so each page is slower than the last. Keyset pagination (WHERE col > $last) stays fast.",
	"leading-wildcard":   "A LIKE pattern that starts with % cannot use a btree index, so it scans every row.",
	"function-on-column": "Wrapping a column in a function in WHERE hides it from a plain index on that column, so the index is skipped.",
	"select-star":        "SELECT * couples the query to the whole table: new columns widen every result, and a covering index can never apply.",
	"or-filter":          "OR across different columns often defeats a single index and forces a scan or a bitmap merge.",
}

var (
	reWS         = regexp.MustCompile(`\s+`)
	reStarItem   = regexp.MustCompile(`(?:^|,)\s*(?:\w+\.)?\*\s*(?:,|$)`)
	reAggregate  = regexp.MustCompile(`(?i)\b(count|sum|avg|min|max|bool_or|bool_and)\s*\(`)
	reLikeWild   = regexp.MustCompile(`(?i)\bi?like\s+(?:'%|\(?\s*'%'\s*\|\|)`)
	reFuncCol    = regexp.MustCompile(`(?i)\b(lower|upper|date|date_trunc|trim|cast)\s*\(\s*(?:'\w+'\s*,\s*)?(?:(\w+)\.)?(\w+)`)
	reQualifier  = regexp.MustCompile(`\b\w+\.([a-z_])`)
	reConjunct   = regexp.MustCompile(`(?is)^\(?\s*(?:(\w+)\.)?("?\w+"?)\s*(?:::\w+(?:\[\])?)?\s*(=|<>|!=|<=|>=|<|>|\bin\b|\bis\b|\bbetween\b|\blike\b|\bilike\b|\bnot\b)`)
	reFromTable  = regexp.MustCompile(`(?is)^\s*(?:only\s+)?([\w."]+)(?:\s+(?:as\s+)?(\w+))?`)
	reUpdTarget  = regexp.MustCompile(`(?is)^update\s+(?:only\s+)?([\w."]+)(?:\s+(?:as\s+)?(\w+))?`)
	reColRHS     = regexp.MustCompile(`^\s*(\w+)\.\w+\s*$`)
	reIsNotNull  = regexp.MustCompile(`^(\w+) is not null$`)
	reCTEHead    = regexp.MustCompile(`(?is)^\s*([\w"]+)\s*(?:\([^)]*\))?\s*as\s+(?:not\s+)?(?:materialized\s+)?\(`)
	nonAliasWord = map[string]bool{"where": true, "join": true, "inner": true, "left": true, "right": true, "full": true, "cross": true,
		"on": true, "natural": true, "lateral": true, "group": true, "order": true, "limit": true, "offset": true, "set": true, "using": true,
		"returning": true, "for": true, "union": true, "having": true, "window": true}
)

// clause keywords, longest first so "group by" is not read as "group".
var clauseKeywords = []string{"group by", "order by", "for update", "returning", "having", "offset", "values", "window", "where", "limit", "union", "from", "set"}

// stmt is one statement split into its top-level clauses.
type stmt struct {
	verb    string
	head    string            // text between the verb and the first clause (the select list)
	clauses map[string]string // keyword -> text, first occurrence only
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// splitClauses scans text once, tracking quotes and parenthesis depth, and
// cuts it at each clause keyword found at depth 0.
func splitClauses(text string) stmt {
	text = strings.TrimSpace(reWS.ReplaceAllString(text, " "))
	low := strings.ToLower(text)
	st := stmt{verb: strings.ToLower(firstWord(text)), clauses: map[string]string{}}
	type cut struct {
		kw         string
		start, end int
	}
	var cuts []cut
	depth := 0
	var quote byte
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			continue
		case '(':
			depth++
			continue
		case ')':
			depth--
			continue
		}
		if depth != 0 || (i > 0 && isWordByte(text[i-1])) {
			continue
		}
		for _, kw := range clauseKeywords {
			if !strings.HasPrefix(low[i:], kw) {
				continue
			}
			if e := i + len(kw); e < len(text) && isWordByte(text[e]) {
				continue
			}
			// "for update" only counts as a clause; a bare "for" never reaches here.
			cuts = append(cuts, cut{kw: kw, start: i + len(kw)})
			i += len(kw) - 1
			break
		}
	}
	end := len(text)
	first := end
	if len(cuts) > 0 {
		first = cuts[0].start - len(cuts[0].kw)
	}
	head := text[:first]
	if len(head) > len(st.verb) {
		head = head[len(st.verb):]
	} else {
		head = ""
	}
	st.head = strings.TrimSpace(head)
	for i := range cuts {
		e := end
		if i+1 < len(cuts) {
			e = cuts[i+1].start - len(cuts[i+1].kw)
		}
		cuts[i].end = e
		if _, dup := st.clauses[cuts[i].kw]; !dup {
			st.clauses[cuts[i].kw] = strings.TrimSpace(text[cuts[i].start:e])
		}
	}
	return st
}

// balancedAt returns the text inside the parenthesis group that starts at
// s[open] and the index just past its closing parenthesis.
func balancedAt(s string, open int) (string, int, bool) {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[open+1 : i], i + 1, true
			}
		}
	}
	return "", 0, false
}

// splitCTEs separates "WITH a AS (...), b AS (...) main" into its bodies and
// the main statement. Text without a WITH comes back as the main statement.
func splitCTEs(text string) (bodies []string, main string) {
	text = strings.TrimSpace(text)
	low := strings.ToLower(text)
	if !strings.HasPrefix(low, "with ") {
		return nil, text
	}
	rest := strings.TrimSpace(text[5:])
	if strings.HasPrefix(strings.ToLower(rest), "recursive ") {
		rest = strings.TrimSpace(rest[10:])
	}
	for {
		m := reCTEHead.FindStringIndex(rest)
		if m == nil {
			return bodies, rest
		}
		body, next, ok := balancedAt(rest, m[1]-1)
		if !ok {
			return bodies, rest
		}
		bodies = append(bodies, body)
		rest = strings.TrimSpace(rest[next:])
		if strings.HasPrefix(rest, ",") {
			rest = strings.TrimSpace(rest[1:])
			continue
		}
		return bodies, rest
	}
}

func analyze(q ParsedQuery, tables map[string]model.Table) []model.Finding {
	text := strings.TrimSpace(reWS.ReplaceAllString(q.Body, " "))
	if text == "" {
		return nil
	}
	bodies, main := splitCTEs(text)
	var out []model.Finding
	seen := map[string]bool{}
	add := func(fs []model.Finding) {
		for _, f := range fs {
			if k := f.Code + "|" + f.Message; !seen[k] {
				seen[k] = true
				out = append(out, f)
			}
		}
	}
	for _, b := range bodies {
		add(analyzeStmt(b, "", false, tables))
	}
	add(analyzeStmt(main, q.Kind, true, tables))
	sort.SliceStable(out, func(i, j int) bool { return out[i].Penalty > out[j].Penalty })
	return out
}

// stripQualifiers lower-cases s and removes "alias." prefixes, so a partial
// index predicate written as "trashed_at IS NULL" matches "t.trashed_at IS NULL".
func stripQualifiers(s string) string {
	s = strings.ToLower(s)
	return reQualifier.ReplaceAllString(s, "$1")
}

func analyzeStmt(text, kind string, isMain bool, tables map[string]model.Table) []model.Finding {
	var out []model.Finding
	add := func(code, sev string, penalty int, msg, fix string) {
		out = append(out, model.Finding{Code: code, Severity: sev, Penalty: penalty, Message: msg, Rationale: scoreRationale[code], Fix: fix})
	}
	st := splitClauses(text)
	isSelect := st.verb == "select"
	isWrite := st.verb == "update" || st.verb == "delete"
	where := st.clauses["where"]

	if isSelect && reStarItem.MatchString(st.head) {
		add("select-star", "info", 5, "selects every column (*)", "list only the columns the caller needs")
	}
	if isWrite && where == "" {
		add("unbounded-write", "high", 40, fmt.Sprintf("%s without WHERE touches every row", strings.ToUpper(st.verb)), "add a WHERE that targets rows by primary key or another selective column")
	}
	if _, ok := st.clauses["offset"]; ok && isSelect {
		add("offset-pagination", "warn", 10, "paginates with OFFSET", "page by a keyset: WHERE (sort_col) > $last ORDER BY sort_col LIMIT $n")
	}
	if reLikeWild.MatchString(where) {
		add("leading-wildcard", "warn", 15, "LIKE pattern starts with a wildcard", "match a prefix instead, or add a trigram (pg_trgm) GIN index")
	}

	// The primary table is the statement's own FROM / UPDATE target. Views, CTE
	// names and subselects are not tables in the model, so judgement stops there.
	var name, alias string
	switch st.verb {
	case "update":
		if m := reUpdTarget.FindStringSubmatch(text); m != nil {
			name, alias = ident(m[1]), m[2]
		}
	case "select", "delete":
		if m := reFromTable.FindStringSubmatch(st.clauses["from"]); m != nil && !strings.HasPrefix(strings.TrimSpace(st.clauses["from"]), "(") {
			name, alias = ident(m[1]), m[2]
		}
	}
	if nonAliasWord[strings.ToLower(alias)] {
		alias = ""
	}
	tbl, known := tables[name]
	conj, complex := splitConjuncts(where), strings.Contains(strings.ToLower(where), " case ") || strings.HasPrefix(strings.ToLower(where), "case ")
	own := func(q string) bool { return q == "" || strings.EqualFold(q, alias) || strings.EqualFold(q, name) }

	filter, eq, cmp := map[string]bool{}, map[string]bool{}, map[string]bool{}
	hasOr := false
	if known && !complex {
		for _, c := range conj {
			if strings.Contains(strings.ToLower(c), " or ") {
				hasOr = true
				continue
			}
			m := reConjunct.FindStringSubmatch(c)
			if m == nil || !own(m[1]) {
				continue
			}
			col, op := ident(m[2]), strings.ToLower(m[3])
			if !columnExists(&tbl, col) {
				continue // an unqualified column of a joined table
			}
			// "t.a = p.b" relates two tables; it is a join condition, not a filter.
			if rhs := reColRHS.FindStringSubmatch(c[len(m[0]):]); rhs != nil && !own(rhs[1]) {
				continue
			}
			filter[col] = true
			if op == "=" {
				eq[col] = true
			}
			if op != "is" && op != "not" {
				cmp[col] = true
			}
		}
	}
	if known && !complex {
		if m := reFuncCol.FindStringSubmatch(where); m != nil && own(m[2]) {
			expr := strings.ToLower(m[1]) + "(" + strings.ToLower(m[3]) + ")"
			if !hasExprIndex(&tbl, expr) && columnExists(&tbl, ident(m[3])) {
				add("function-on-column", "warn", 25, fmt.Sprintf("filters on %s, which a plain index cannot serve", expr), fmt.Sprintf("add an expression index on %s, or compare the raw column", expr))
			}
		}
	}

	if isMain && isSelect && kind == "many" {
		_, limited := st.clauses["limit"]
		_, grouped := st.clauses["group by"]
		single := reAggregate.MatchString(st.head) && !grouped
		if !limited && !single && !(known && uniquePinned(&tbl, eq)) {
			add("unbounded-list", "warn", 20, "returns every matching row (no LIMIT)", "add LIMIT $n, ideally with keyset pagination")
		}
	}
	if !known || complex {
		return out
	}

	ctx := predCtx{norm: stripQualifiers(where + " " + st.clauses["from"]), cmp: cmp}
	filtered := len(filter) > 0
	served := filtered && filterServed(&tbl, filter, ctx)
	if hasOr && !served {
		add("or-filter", "info", 5, "filter combines columns with OR", "split into a UNION of indexed lookups if this is hot")
	}
	if filtered && !served {
		cols := sortedKeys(filter)
		fix := fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s)", tbl.Name, strings.Join(cols, "_"), tbl.Name, strings.Join(cols, ", "))
		if len(cmp) == 0 {
			// Only IS [NOT] NULL tests: low selectivity, so a plain index rarely helps.
			add("unindexed-filter", "info", 10, fmt.Sprintf("filters %s.%s on NULL tests only, with no index", tbl.Name, strings.Join(cols, ", ")),
				"a partial index (CREATE INDEX ... WHERE "+cols[0]+" IS [NOT] NULL) helps if this set is small and hot")
		} else {
			add("unindexed-filter", "warn", 30, fmt.Sprintf("filters %s.%s, but no index leads on it", tbl.Name, strings.Join(cols, ", ")), fix)
		}
	}
	if ob := st.clauses["order by"]; isSelect && ob != "" {
		item := strings.TrimSpace(strings.SplitN(ob, ",", 2)[0])
		f := strings.Fields(item)
		if len(f) > 0 && !strings.Contains(f[0], "(") {
			qual := ""
			if i := strings.LastIndex(f[0], "."); i >= 0 {
				qual = f[0][:i]
			}
			col := ident(f[0])
			if own(qual) && columnExists(&tbl, col) && !sortServed(&tbl, col, filter, ctx) {
				fix := fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s)", tbl.Name, col, tbl.Name, col)
				if served {
					// The filter already narrows the rows through an index; only that slice is sorted.
					add("unindexed-sort", "info", 5, fmt.Sprintf("sorts the filtered %s rows by %s in memory", tbl.Name, col),
						"fine while the filtered set stays small; otherwise extend the filter's index with "+col)
				} else {
					add("unindexed-sort", "warn", 15, fmt.Sprintf("sorts %s by %s with no index to read it in order", tbl.Name, col), fix)
				}
			}
		}
	}
	return out
}

func columnExists(t *model.Table, col string) bool {
	for _, c := range t.Columns {
		if c.Name == col {
			return true
		}
	}
	return false
}

// uniquePinned reports whether equality filters pin every column of a unique
// index, so the statement returns at most one row.
func uniquePinned(t *model.Table, eq map[string]bool) bool {
	for _, ix := range t.Indexes {
		if !ix.Unique || ix.Where != "" || len(ix.Columns) == 0 {
			continue
		}
		all := true
		for _, c := range ix.Columns {
			if !eq[c] {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// splitConjuncts splits a WHERE clause at top-level AND. The AND that belongs
// to a BETWEEN stays inside its conjunct.
func splitConjuncts(where string) []string {
	if where == "" {
		return nil
	}
	var out []string
	depth, start := 0, 0
	low := strings.ToLower(where)
	var quote byte
	for i := 0; i < len(where); i++ {
		c := where[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			continue
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth != 0 || !strings.HasPrefix(low[i:], " and ") {
			continue
		}
		before := low[start:i]
		if b := strings.LastIndex(before, " between "); b >= 0 && !strings.Contains(before[b:], " and ") {
			continue
		}
		out = append(out, strings.TrimSpace(where[start:i]))
		start = i + 5
		i += 4
	}
	return append(out, strings.TrimSpace(where[start:]))
}

// predCtx is what the statement says, used to decide whether a partial index
// applies: its WHERE and JOIN text with qualifiers stripped, and the columns
// it compares (a comparison implies NOT NULL).
type predCtx struct {
	norm string
	cmp  map[string]bool
}

// usable reports whether every conjunct of the index's predicate is stated by
// the query, and returns the columns that predicate pins.
func (c predCtx) usable(ix model.Index) (bool, map[string]bool) {
	pinned := map[string]bool{}
	if ix.Where == "" {
		return true, pinned
	}
	for _, p := range splitConjuncts(ix.Where) {
		p = strings.Trim(strings.ToLower(p), "() ")
		if m := reConjunct.FindStringSubmatch(p); m != nil {
			pinned[ident(m[2])] = true
		}
		if containsWord(c.norm, p) {
			continue
		}
		if m := reIsNotNull.FindStringSubmatch(p); m != nil && c.cmp[m[1]] {
			continue
		}
		return false, nil
	}
	return true, pinned
}

// filterServed: some usable index leads on a filtered column, or is a partial
// index whose predicate already pins every filtered column.
func filterServed(t *model.Table, filter map[string]bool, ctx predCtx) bool {
	for _, ix := range t.Indexes {
		ok, pinned := ctx.usable(ix)
		if !ok || len(ix.Columns) == 0 {
			continue
		}
		if filter[ix.Columns[0]] {
			return true
		}
		if ix.Where != "" {
			all := true
			for c := range filter {
				if !pinned[c] {
					all = false
					break
				}
			}
			if all {
				return true
			}
		}
	}
	return false
}

// sortServed: a usable index delivers rows ordered by col, after any leading
// columns pinned by filters or by the index's own predicate.
func sortServed(t *model.Table, col string, filter map[string]bool, ctx predCtx) bool {
	for _, ix := range t.Indexes {
		ok, pinned := ctx.usable(ix)
		if !ok {
			continue
		}
		for _, c := range ix.Columns {
			if c == col {
				return true
			}
			if !filter[c] && !pinned[c] {
				break
			}
		}
	}
	return false
}

func hasExprIndex(t *model.Table, expr string) bool {
	for _, ix := range t.Indexes {
		for _, c := range ix.Columns {
			if strings.ReplaceAll(c, " ", "") == expr {
				return true
			}
		}
	}
	return false
}

// containsWord reports whether sub occurs in s not glued to identifier
// characters, so "user_id is null" does not match "parent_user_id is null".
func containsWord(s, sub string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], sub)
		if j < 0 {
			return false
		}
		a, z := i+j, i+j+len(sub)
		okL := a == 0 || !isIdentByte(s[a-1])
		okR := z == len(s) || !isIdentByte(s[z])
		if okL && okR {
			return true
		}
		i = a + 1
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
