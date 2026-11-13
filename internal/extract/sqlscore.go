package extract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// ScoreQuery rates one sqlc query statically: it reads the statement text and
// the indexes of the tables it touches, and never talks to a database. It
// flags patterns that usually cost a sequential scan, an unbounded result or
// a full sort. A table's row count is unknown, so findings are "this will not
// scale" signals rather than measured slowness.
func ScoreQuery(q ParsedQuery, tables map[string]model.Table) model.QueryScore {
	findings := analyze(q, tables)
	score := 100
	for _, f := range findings {
		score -= f.Penalty
	}
	if score < 0 {
		score = 0
	}
	grade := "D"
	switch {
	case score >= 90:
		grade = "A"
	case score >= 75:
		grade = "B"
	case score >= 55:
		grade = "C"
	}
	if findings == nil {
		findings = []model.Finding{}
	}
	return model.QueryScore{Value: score, Grade: grade, Findings: findings}
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
	"select-star":        "SELECT * (or RETURNING *) couples the query to the whole table: new columns widen every result, and a covering index can never apply.",
	"or-filter":          "OR across different columns often defeats a single index and forces a scan or a bitmap merge.",
}

var (
	reWS         = regexp.MustCompile(`\s+`)
	reSelectStar = regexp.MustCompile(`(?i)\bselect\s+(?:distinct\s+)?(?:\w+\.)?\*|\breturning\s+\*`)
	reLimit      = regexp.MustCompile(`(?i)\blimit\b`)
	reOffset     = regexp.MustCompile(`(?i)\boffset\b`)
	reLikeWild   = regexp.MustCompile(`(?i)\bi?like\s+(?:'%|\(?\s*'%'\s*\|\|)`)
	reFuncCol    = regexp.MustCompile(`(?i)\b(lower|upper|date|date_trunc|trim|cast)\s*\(\s*(?:'\w+'\s*,\s*)?(\w+)`)
	reWhere      = regexp.MustCompile(`(?is)\bwhere\b(.*?)(?:\bgroup\s+by\b|\border\s+by\b|\blimit\b|\boffset\b|\breturning\b|\bfor\s+update\b|$)`)
	reOrderBy    = regexp.MustCompile(`(?is)\border\s+by\b(.*?)(?:\blimit\b|\boffset\b|\bfor\s+update\b|$)`)
	reConjunct   = regexp.MustCompile(`(?is)^\(?\s*(?:\w+\.)?("?\w+"?)\s*(=|<>|!=|<=|>=|<|>|\bin\b|\bis\b|\bbetween\b|\blike\b|\bilike\b|\bnot\b)`)
)

func analyze(q ParsedQuery, tables map[string]model.Table) []model.Finding {
	var out []model.Finding
	add := func(code, sev string, penalty int, msg, fix string) {
		out = append(out, model.Finding{Code: code, Severity: sev, Penalty: penalty, Message: msg, Rationale: scoreRationale[code], Fix: fix})
	}
	text := strings.TrimSpace(reWS.ReplaceAllString(q.Body, " "))
	if text == "" {
		return nil
	}
	verb := strings.ToLower(firstWord(text))
	isSelect := verb == "select" || verb == "with"
	isWrite := verb == "update" || verb == "delete"

	if reSelectStar.MatchString(text) {
		add("select-star", "info", 5, "selects or returns every column (*)", "list only the columns the caller needs")
	}

	where := ""
	if m := reWhere.FindStringSubmatch(text); m != nil {
		where = strings.TrimSpace(m[1])
	}
	if isWrite && where == "" {
		add("unbounded-write", "high", 40, fmt.Sprintf("%s without WHERE touches every row", strings.ToUpper(verb)), "add a WHERE that targets rows by primary key or another selective column")
	}
	if isSelect && q.Kind == "many" && !reLimit.MatchString(text) {
		add("unbounded-list", "warn", 20, "returns every matching row (no LIMIT)", "add LIMIT $n, ideally with keyset pagination")
	}
	if reOffset.MatchString(text) {
		add("offset-pagination", "warn", 10, "paginates with OFFSET", "page by a keyset: WHERE (sort_col) > $last ORDER BY sort_col LIMIT $n")
	}
	if reLikeWild.MatchString(text) {
		add("leading-wildcard", "warn", 15, "LIKE pattern starts with a wildcard", "match a prefix instead, or add a trigram (pg_trgm) GIN index")
	}

	// The filtered table is the first one the statement touches; joins are
	// scored on that table only (a deliberate POC simplification).
	var tbl *model.Table
	if len(q.Tables) > 0 {
		if t, ok := tables[q.Tables[0]]; ok {
			tbl = &t
		}
	}
	if tbl == nil {
		return out
	}
	filterCols := map[string]bool{}
	var hasOr bool
	var preds []string
	for _, c := range splitConjuncts(where) {
		if strings.Contains(strings.ToLower(c), " or ") {
			hasOr = true
			continue
		}
		preds = append(preds, normPredicate(c))
		if m := reConjunct.FindStringSubmatch(c); m != nil {
			filterCols[ident(m[1])] = true
		}
	}
	if hasOr {
		add("or-filter", "info", 5, "filter combines columns with OR", "split into a UNION of indexed lookups if this is hot")
	}
	if m := reFuncCol.FindStringSubmatch(where); m != nil {
		expr := strings.ToLower(m[1]) + "(" + strings.ToLower(m[2]) + ")"
		if !hasExprIndex(tbl, expr) {
			add("function-on-column", "warn", 25, fmt.Sprintf("filters on %s, which a plain index cannot serve", expr), fmt.Sprintf("add an expression index on %s, or compare the raw column", expr))
		}
	}
	if len(filterCols) > 0 && !filterServed(tbl, filterCols, preds) {
		cols := sortedKeys(filterCols)
		add("unindexed-filter", "warn", 30,
			fmt.Sprintf("filters %s.%s, but no index leads on it", tbl.Name, strings.Join(cols, ", ")),
			fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s)", tbl.Name, strings.Join(cols, "_"), tbl.Name, strings.Join(cols, ", ")))
	}
	if isSelect {
		if m := reOrderBy.FindStringSubmatch(text); m != nil {
			first := strings.Fields(strings.TrimSpace(strings.SplitN(m[1], ",", 2)[0]))
			if len(first) > 0 {
				col := ident(first[0])
				if !sortServed(tbl, col, filterCols, preds) {
					add("unindexed-sort", "warn", 15,
						fmt.Sprintf("sorts %s by %s with no index to read it in order", tbl.Name, col),
						fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s (%s)", tbl.Name, col, tbl.Name, col))
				}
			}
		}
	}
	return out
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
	inStr := false
	for i := 0; i < len(where); i++ {
		switch where[i] {
		case '\'':
			inStr = !inStr
		case '(':
			if !inStr {
				depth++
			}
		case ')':
			if !inStr {
				depth--
			}
		}
		if inStr || depth != 0 || !strings.HasPrefix(low[i:], " and ") {
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

// usable reports whether a partial index's predicate is implied by the query
// (here: the query states it verbatim as a conjunct).
func usable(ix model.Index, preds []string) bool {
	if ix.Where == "" {
		return true
	}
	for _, p := range preds {
		if p == ix.Where {
			return true
		}
	}
	return false
}

// filterServed: some usable index leads on a filtered column, or is a partial
// index whose predicate the query states (its rows are the filtered rows).
func filterServed(t *model.Table, cols map[string]bool, preds []string) bool {
	for _, ix := range t.Indexes {
		if !usable(ix, preds) {
			continue
		}
		if ix.Where != "" || (len(ix.Columns) > 0 && cols[ix.Columns[0]]) {
			return true
		}
	}
	return false
}

// sortServed: a usable index delivers rows ordered by col, after any leading
// columns pinned by equality filters.
func sortServed(t *model.Table, col string, eq map[string]bool, preds []string) bool {
	for _, ix := range t.Indexes {
		if !usable(ix, preds) {
			continue
		}
		for _, c := range ix.Columns {
			if c == col {
				return true
			}
			if !eq[c] {
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
