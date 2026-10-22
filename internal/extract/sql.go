package extract

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Catalog accumulates tables by applying goose migrations in order. The
// parser is deliberately tolerant: statements it does not understand are
// skipped, never fatal.
type Catalog struct {
	tables map[string]*tableDef
}

type tableDef struct {
	name       string
	columns    []model.Column
	migrations []string
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog { return &Catalog{tables: map[string]*tableDef{}} }

// Tables returns the tables, sorted by name. Owners are left empty.
func (c *Catalog) Tables() []model.Table {
	out := make([]model.Table, 0, len(c.tables))
	for _, t := range c.tables {
		cols := append([]model.Column{}, t.columns...)
		out = append(out, model.Table{Name: t.name, Columns: cols, Migrations: append([]string{}, t.migrations...)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Has reports whether a table exists in the catalog.
func (c *Catalog) Has(name string) bool { _, ok := c.tables[name]; return ok }

// ApplyMigration applies the "Up" section of one goose migration. file is the
// path recorded in Table.Migrations.
func (c *Catalog) ApplyMigration(file, content string) {
	for _, stmt := range splitStatements(upSection(content)) {
		c.applyStatement(file, stmt)
	}
}

// upSection returns the part of a goose migration that runs on "up": text
// after "-- +goose Up" until "-- +goose Down". Files with no markers are
// treated as all-up.
func upSection(content string) string {
	var b strings.Builder
	down := false
	for _, line := range strings.Split(content, "\n") {
		t := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(t, "-- +goose") {
			rest := strings.TrimSpace(strings.TrimPrefix(t, "-- +goose"))
			switch {
			case strings.HasPrefix(rest, "up"):
				down = false
			case strings.HasPrefix(rest, "down"):
				down = true
			}
			continue
		}
		if !down {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// splitStatements splits SQL at top-level semicolons, dropping comments.
// Single quotes, double quotes, dollar quotes and block comments are respected.
func splitStatements(sql string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(sql); {
		ch := sql[i]
		switch {
		case ch == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case ch == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = len(sql)
			} else {
				i += end + 4
			}
			cur.WriteByte(' ')
		case ch == '\'' || ch == '"':
			j := i + 1
			for j < len(sql) {
				if sql[j] == ch {
					if j+1 < len(sql) && sql[j+1] == ch { // doubled quote
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j >= len(sql) {
				j = len(sql) - 1
			}
			cur.WriteString(sql[i : j+1])
			i = j + 1
		case ch == '$':
			if m := dollarTag.FindString(sql[i:]); m != "" {
				end := strings.Index(sql[i+len(m):], m)
				if end < 0 {
					cur.WriteString(sql[i:])
					i = len(sql)
				} else {
					cur.WriteString(sql[i : i+len(m)+end+len(m)])
					i += len(m) + end + len(m)
				}
			} else {
				cur.WriteByte(ch)
				i++
			}
		case ch == ';':
			flush()
			i++
		default:
			cur.WriteByte(ch)
			i++
		}
	}
	flush()
	return out
}

var dollarTag = regexp.MustCompile(`^\$[A-Za-z_]*\$`)

var (
	reCreateTable = regexp.MustCompile(`(?is)^create\s+(?:(?:global|local)\s+)?(?:temp(?:orary)?\s+|unlogged\s+)?table\s+(?:if\s+not\s+exists\s+)?([\w."]+)\s*\(`)
	reAlterTable  = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?(?:only\s+)?([\w."]+)\s+(.*)$`)
	reDropTable   = regexp.MustCompile(`(?is)^drop\s+table\s+(?:if\s+exists\s+)?(.*?)(?:\s+(?:cascade|restrict))?$`)

	reAddColumn   = regexp.MustCompile(`(?is)^add\s+(?:column\s+)?(?:if\s+not\s+exists\s+)?(.+)$`)
	reDropColumn  = regexp.MustCompile(`(?is)^drop\s+(?:column\s+)?(?:if\s+exists\s+)?([\w."]+)`)
	reRenameCol   = regexp.MustCompile(`(?is)^rename\s+(?:column\s+)?([\w."]+)\s+to\s+([\w."]+)$`)
	reRenameTable = regexp.MustCompile(`(?is)^rename\s+to\s+([\w."]+)$`)
	reAlterColumn = regexp.MustCompile(`(?is)^alter\s+(?:column\s+)?([\w."]+)\s+(.*)$`)
	reSetType     = regexp.MustCompile(`(?is)^(?:set\s+data\s+)?type\s+(.+?)(?:\s+using\s+.*)?$`)
	reNotNull     = regexp.MustCompile(`(?i)\bnot\s+null\b`)
	rePrimaryKey  = regexp.MustCompile(`(?i)\bprimary\s+key\b`)
	reReferences  = regexp.MustCompile(`(?i)\breferences\s+([\w."]+)`)
	reFKCols      = regexp.MustCompile(`(?is)foreign\s+key\s*\(([^)]*)\)\s*references\s+([\w."]+)`)
	rePKCols      = regexp.MustCompile(`(?is)primary\s+key\s*\(([^)]*)\)`)
)

// ident normalizes an identifier: drops the schema prefix and quotes, and
// lower-cases unquoted names.
func ident(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) && len(s) >= 2 {
		return s[1 : len(s)-1]
	}
	return strings.ToLower(s)
}

func (c *Catalog) touch(t *tableDef, file string) {
	for _, m := range t.migrations {
		if m == file {
			return
		}
	}
	t.migrations = append(t.migrations, file)
}

func (c *Catalog) applyStatement(file, stmt string) {
	if m := reCreateTable.FindStringSubmatchIndex(stmt); m != nil {
		name := ident(stmt[m[2]:m[3]])
		body, ok := balanced(stmt[m[1]-1:])
		if !ok {
			return
		}
		t := &tableDef{name: name}
		for _, item := range splitTop(body, ',') {
			applyTableItem(t, strings.TrimSpace(item))
		}
		c.touch(t, file)
		c.tables[name] = t
		return
	}
	if m := reAlterTable.FindStringSubmatch(stmt); m != nil {
		name := ident(m[1])
		t := c.tables[name]
		if t == nil {
			return
		}
		for _, action := range splitTop(m[2], ',') {
			c.applyAlter(t, strings.TrimSpace(action))
		}
		c.touch(t, file)
		return
	}
	if m := reDropTable.FindStringSubmatch(stmt); m != nil {
		for _, n := range strings.Split(m[1], ",") {
			delete(c.tables, ident(n))
		}
	}
}

func (c *Catalog) applyAlter(t *tableDef, action string) {
	if m := reRenameTable.FindStringSubmatch(action); m != nil {
		delete(c.tables, t.name)
		t.name = ident(m[1])
		c.tables[t.name] = t
		return
	}
	if m := reRenameCol.FindStringSubmatch(action); m != nil {
		if col := t.col(ident(m[1])); col != nil {
			col.Name = ident(m[2])
		}
		return
	}
	if m := reAddColumn.FindStringSubmatch(action); m != nil {
		applyTableItem(t, strings.TrimSpace(m[1]))
		return
	}
	if m := reDropColumn.FindStringSubmatch(action); m != nil && !strings.HasPrefix(strings.ToLower(action), "drop constraint") {
		name := ident(m[1])
		for i := range t.columns {
			if t.columns[i].Name == name {
				t.columns = append(t.columns[:i], t.columns[i+1:]...)
				break
			}
		}
		return
	}
	if m := reAlterColumn.FindStringSubmatch(action); m != nil {
		col := t.col(ident(m[1]))
		if col == nil {
			return
		}
		rest := strings.TrimSpace(m[2])
		low := strings.ToLower(rest)
		switch {
		case strings.HasPrefix(low, "set not null"):
			col.Nullable = false
		case strings.HasPrefix(low, "drop not null"):
			col.Nullable = true
		default:
			if tm := reSetType.FindStringSubmatch(rest); tm != nil {
				col.Type = normType(tm[1])
			}
		}
	}
}

func (t *tableDef) col(name string) *model.Column {
	for i := range t.columns {
		if t.columns[i].Name == name {
			return &t.columns[i]
		}
	}
	return nil
}

func looksLikeConstraint(item string) bool {
	w := strings.ToLower(firstWord(item))
	switch w {
	case "constraint", "primary", "unique", "foreign", "check", "exclude", "like":
		return true
	}
	return false
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\r\n("); i >= 0 {
		return s[:i]
	}
	return s
}

// applyTableItem handles one comma-separated element of a CREATE TABLE body
// (or an ALTER ... ADD): a column definition or a table constraint.
func applyTableItem(t *tableDef, item string) {
	if item == "" {
		return
	}
	if looksLikeConstraint(item) {
		if m := rePKCols.FindStringSubmatch(item); m != nil {
			for _, n := range strings.Split(m[1], ",") {
				if col := t.col(ident(n)); col != nil {
					col.PrimaryKey = true
					col.Nullable = false
				}
			}
		}
		if m := reFKCols.FindStringSubmatch(item); m != nil {
			cols := strings.Split(m[1], ",")
			if len(cols) == 1 {
				if col := t.col(ident(cols[0])); col != nil {
					col.References = ident(m[2])
				}
			}
		}
		return
	}
	name, rest := splitFirstToken(item)
	if name == "" {
		return
	}
	col := model.Column{Name: ident(name), Nullable: true}
	var typeToks []string
	for _, tok := range splitTop(rest, ' ') {
		if tok = strings.TrimSpace(tok); tok == "" {
			continue
		}
		if isColumnKeyword(tok) {
			break
		}
		typeToks = append(typeToks, tok)
	}
	col.Type = normType(strings.Join(typeToks, " "))
	if reNotNull.MatchString(rest) {
		col.Nullable = false
	}
	if rePrimaryKey.MatchString(rest) {
		col.PrimaryKey = true
		col.Nullable = false
	}
	if m := reReferences.FindStringSubmatch(rest); m != nil {
		col.References = ident(m[1])
	}
	if old := t.col(col.Name); old != nil {
		*old = col
		return
	}
	t.columns = append(t.columns, col)
}

func isColumnKeyword(tok string) bool {
	switch strings.ToLower(tok) {
	case "not", "null", "default", "primary", "unique", "references", "check", "generated", "collate", "constraint":
		return true
	}
	return false
}

func normType(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// splitFirstToken splits "name rest..." where name may be double-quoted.
func splitFirstToken(s string) (string, string) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) {
		if end := strings.Index(s[1:], `"`); end >= 0 {
			return s[:end+2], strings.TrimSpace(s[end+2:])
		}
	}
	if i := strings.IndexAny(s, " \t\r\n"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i:])
	}
	return s, ""
}

// balanced returns the text inside the parenthesis group s starts with.
func balanced(s string) (string, bool) {
	depth := 0
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr != 0 {
			if ch == inStr {
				inStr = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			inStr = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], true
			}
		}
	}
	return "", false
}

// splitTop splits s on sep at parenthesis depth 0, outside quotes. A space
// separator splits on any whitespace.
func splitTop(s string, sep byte) []string {
	var out []string
	depth := 0
	inStr := byte(0)
	start := 0
	isSep := func(ch byte) bool {
		if sep == ' ' {
			return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
		}
		return ch == sep
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr != 0 {
			if ch == inStr {
				inStr = 0
			}
			continue
		}
		switch {
		case ch == '\'' || ch == '"':
			inStr = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case depth == 0 && isSep(ch):
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// ParsedQuery is one sqlc query found in a query file.
type ParsedQuery struct {
	Name   string
	Kind   string // one, many, exec, execrows, ...
	Line   int
	Tables []string // tables touched, restricted to known tables, sorted
}

var (
	reQueryName = regexp.MustCompile(`^--\s*name:\s*(\w+)\s+:(\w+)`)
	reTableRef  = regexp.MustCompile(`(?i)\b(?:from|join|into|update)\s+(?:only\s+)?([\w."]+)`)
)

// ParseQueries parses a sqlc query file. known limits the tables reported
// (this also screens out CTE names and keywords the tolerant regex catches).
func ParseQueries(content string, known func(string) bool) []ParsedQuery {
	lines := strings.Split(content, "\n")
	var out []ParsedQuery
	var cur *ParsedQuery
	var body []string
	finish := func() {
		if cur == nil {
			return
		}
		seen := map[string]bool{}
		text := strings.Join(splitStatementsKeepAll(strings.Join(body, "\n")), " ")
		for _, m := range reTableRef.FindAllStringSubmatch(text, -1) {
			n := ident(m[1])
			if known(n) && !seen[n] {
				seen[n] = true
				cur.Tables = append(cur.Tables, n)
			}
		}
		sort.Strings(cur.Tables)
		out = append(out, *cur)
		cur, body = nil, nil
	}
	for i, line := range lines {
		if m := reQueryName.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			finish()
			cur = &ParsedQuery{Name: m[1], Kind: m[2], Line: i + 1, Tables: []string{}}
			continue
		}
		if cur != nil {
			body = append(body, line)
		}
	}
	finish()
	return out
}

// splitStatementsKeepAll strips comments and returns the statements.
func splitStatementsKeepAll(s string) []string { return splitStatements(s) }
