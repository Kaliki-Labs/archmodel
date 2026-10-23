package extract

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// CoverBlock is one line of a Go coverprofile.
type CoverBlock struct {
	StartLine, EndLine int
	Statements         int
	Count              int
}

// Profile maps a file path as written in the profile (import path style,
// e.g. "example.com/app/backend/internal/auth/service.go") to its blocks.
type Profile map[string][]CoverBlock

// ParseCoverProfile reads a Go coverprofile (mode set, count or atomic).
// Blocks listed more than once (merged profiles) are combined by summing counts.
func ParseCoverProfile(r io.Reader) (Profile, error) {
	prof := Profile{}
	type key struct {
		file                 string
		sl, sc, el, ec, stmt int
	}
	index := map[key]int{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		// file:startLine.startCol,endLine.endCol numStmt count
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			return nil, fmt.Errorf("coverprofile line %d: malformed", n)
		}
		file, rest := line[:colon], line[colon+1:]
		var sl, scol, el, ecol, stmts, count int
		if _, err := fmt.Sscanf(rest, "%d.%d,%d.%d %d %d", &sl, &scol, &el, &ecol, &stmts, &count); err != nil {
			return nil, fmt.Errorf("coverprofile line %d: %w", n, err)
		}
		k := key{file, sl, scol, el, ecol, stmts}
		if i, ok := index[k]; ok {
			prof[file][i].Count += count
			continue
		}
		index[k] = len(prof[file])
		prof[file] = append(prof[file], CoverBlock{StartLine: sl, EndLine: el, Statements: stmts, Count: count})
	}
	return prof, sc.Err()
}

// FuncCoverage returns the statements and covered statements of the blocks
// that lie inside lines [start, end] of file. ok is false when the profile
// has no entry for the file at all.
func (p Profile) FuncCoverage(file string, start, end int) (stmts, covered int, ok bool) {
	blocks, ok := p[file]
	if !ok {
		return 0, 0, false
	}
	for _, b := range blocks {
		if b.StartLine >= start && b.EndLine <= end {
			stmts += b.Statements
			if b.Count > 0 {
				covered += b.Statements
			}
		}
	}
	return stmts, covered, true
}

// CRAP is the Change Risk Anti-Patterns score: CC^2 * (1-cov)^3 + CC, with
// cov in [0,1].
func CRAP(cc int, cov float64) float64 {
	c := float64(cc)
	return c*c*math.Pow(1-cov, 3) + c
}

// Round rounds to 2 decimals so the JSON stays stable and readable.
func Round(f float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', 2, 64), 64)
	return v
}
