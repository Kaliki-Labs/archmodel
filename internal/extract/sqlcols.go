package extract

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Output columns say what a query returns, so a diff can show that a query now
// returns one more column (the usual cost of SELECT *). sqlc already rewrites
// * into an explicit list in the Go it generates, so when that code is present
// it is the source of truth; otherwise * is expanded from the migrations.
const (
	colsGenerated = "generated" // from sqlc's generated Go
	colsSchema    = "schema"    // * expanded from the migrations
	colsText      = "text"      // listed in the query itself
)

var (
	reGenConst  = regexp.MustCompile("(?s)const \\w+ = `-- name: (\\w+) :\\w+\\n(.*?)`")
	reSelAlias  = regexp.MustCompile(`(?i)\s+as\s+("?\w+"?)\s*$`)
	reFromJoins = regexp.MustCompile(`(?i)\b(?:from|join)\s+(?:only\s+)?([\w."]+)(?:\s+(?:as\s+)?(\w+))?`)
)

// textHash fingerprints a statement ignoring whitespace and case, so a pure
// reformat is not a change.
func textHash(body string) string {
	n := strings.ToLower(strings.Join(strings.Fields(body), " "))
	sum := sha256.Sum256([]byte(n))
	return hex.EncodeToString(sum[:])[:12]
}

// generatedSQL reads sqlc's generated query constants (name -> statement
// text), or nil when the generated code is not on disk.
func generatedSQL(goDir string) map[string]string {
	out := map[string]string{}
	filepath.WalkDir(goDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == "node_modules" || n == ".git" || n == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".sql.go") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, m := range reGenConst.FindAllStringSubmatch(string(data), -1) {
			out[m[1]] = stripSQLComments(m[2])
		}
		return nil
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func stripSQLComments(s string) string {
	var b strings.Builder
	for _, ln := range strings.Split(s, "\n") {
		if i := strings.Index(ln, "--"); i >= 0 {
			ln = ln[:i]
		}
		b.WriteString(ln + "\n")
	}
	return b.String()
}

// splitTopCommas splits s at commas outside parentheses and quotes.
func splitTopCommas(s string) []string {
	var out []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
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
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// outputColumns lists what a statement returns: its SELECT list, or RETURNING
// for writes. A * is expanded from tables (the migrations); from reports where
// the list came from. Statements that return nothing give nil.
func outputColumns(body string, tables map[string]model.Table) (cols []string, from string) {
	text := strings.TrimSpace(reWS.ReplaceAllString(body, " "))
	if text == "" {
		return nil, ""
	}
	_, main := splitCTEs(text)
	st := splitClauses(main)
	list := ""
	switch st.verb {
	case "select":
		list = st.head
	case "insert", "update", "delete":
		list = st.clauses["returning"]
	}
	if list == "" {
		return nil, ""
	}
	// table aliases in the statement's FROM / JOIN / INSERT INTO / UPDATE.
	alias := map[string]string{}
	first := ""
	scan := main
	if st.verb == "insert" {
		scan = reInsertInto.ReplaceAllString(main, "from $1")
	}
	if st.verb == "update" {
		scan = reUpdateTarget.ReplaceAllString(main, "from $1")
	}
	for _, m := range reFromJoins.FindAllStringSubmatch(scan, -1) {
		t := ident(m[1])
		if first == "" {
			first = t
		}
		alias[t] = t
		if a := strings.ToLower(m[2]); a != "" && !nonAliasWord[a] {
			alias[a] = t
		}
	}
	from = colsText
	for _, item := range splitTopCommas(list) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" || strings.HasSuffix(item, ".*") {
			t := first
			if q := strings.TrimSuffix(item, ".*"); q != item {
				t = alias[strings.ToLower(ident(q))]
			}
			if tb, ok := tables[t]; ok {
				for _, c := range tb.Columns {
					cols = append(cols, c.Name)
				}
				from = colsSchema
			} else {
				cols = append(cols, item)
			}
			continue
		}
		if m := reSelAlias.FindStringSubmatch(item); m != nil {
			cols = append(cols, ident(m[1]))
			continue
		}
		name := item
		if i := strings.LastIndex(name, "."); i >= 0 && !strings.ContainsAny(name, "( ") {
			name = name[i+1:]
		}
		if len(name) > 40 {
			name = name[:40]
		}
		cols = append(cols, ident(name))
	}
	return cols, from
}

var (
	reInsertInto   = regexp.MustCompile(`(?i)^insert\s+into\s+([\w."]+)`)
	reUpdateTarget = regexp.MustCompile(`(?i)^update\s+(?:only\s+)?([\w."]+)`)
)
