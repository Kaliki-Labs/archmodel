package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Kaliki-Labs/archmodel/internal/query"
)

func init() {
	register("query", command{summary: "answer questions about the model as JSON (who-depends, module, operation, ...)", run: runQuery})
}

// queryKinds maps each query to its argument name ("" = none) and handler.
var queryKinds = []struct {
	name, arg, help string
	run             func(ix *query.Index, arg string) (any, error)
}{
	{"who-depends", "module", "modules that import <module>", func(ix *query.Index, a string) (any, error) { return ix.WhoDepends(a) }},
	{"depends-on", "module", "modules <module> imports", func(ix *query.Index, a string) (any, error) { return ix.DependsOn(a) }},
	{"module", "name", "the Module card", func(ix *query.Index, a string) (any, error) { return ix.Module(a) }},
	{"operation", "operationId", "an API operation with its schemas", func(ix *query.Index, a string) (any, error) { return ix.Operation(a) }},
	{"table", "name", "a table with the queries and modules touching it", func(ix *query.Index, a string) (any, error) { return ix.Table(a) }},
	{"rules", "", "every rule with its Rationale, and the metric Rationales", func(ix *query.Index, _ string) (any, error) { return ix.Rules(), nil }},
	{"modules", "", "list every Module", func(ix *query.Index, _ string) (any, error) { return ix.Modules(), nil }},
	{"operations", "", "list every operation", func(ix *query.Index, _ string) (any, error) { return ix.Operations(), nil }},
}

func queryUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: archmodel query <kind> [argument] [flags]\n\nkinds:")
	for _, k := range queryKinds {
		name := k.name
		if k.arg != "" {
			name += " <" + k.arg + ">"
		}
		fmt.Fprintf(w, "  %-26s %s\n", name, k.help)
	}
	fmt.Fprintln(w, "\nflags: --root, --config, --coverprofile (build fresh), or --model <path> (read a model.json)")
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func runQuery(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { queryUsage(stderr) }
	g := addGuardFlags(fs)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	if len(pos) == 0 {
		queryUsage(stderr)
		return errUsage
	}
	for _, k := range queryKinds {
		if k.name != pos[0] {
			continue
		}
		want := 1
		if k.arg != "" {
			want = 2
		}
		if len(pos) != want {
			if k.arg != "" {
				fmt.Fprintf(stderr, "archmodel query %s: needs one argument, <%s>\n", k.name, k.arg)
			} else {
				fmt.Fprintf(stderr, "archmodel query %s: takes no argument\n", k.name)
			}
			return errUsage
		}
		m, err := g.load()
		if err != nil {
			return err
		}
		arg := ""
		if want == 2 {
			arg = pos[1]
		}
		ans, err := k.run(query.New(m), arg)
		var nf *query.NotFoundError
		if errors.As(err, &nf) {
			if perr := printJSON(stdout, nf); perr != nil {
				return perr
			}
			return errFailed
		}
		if err != nil {
			return err
		}
		return printJSON(stdout, ans)
	}
	fmt.Fprintf(stderr, "archmodel query: unknown kind %q\n\n", pos[0])
	queryUsage(stderr)
	return errUsage
}
