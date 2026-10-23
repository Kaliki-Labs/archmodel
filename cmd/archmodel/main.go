// Command archmodel extracts and queries a project's Architecture model.
//
// Usage: archmodel <command> [flags]. Run "archmodel help" for the commands.
package main

import (
	"fmt"
	"io"
	"os"
	"sort"
)

// command is one subcommand. Adding a command means adding an entry to
// commands (see build.go).
type command struct {
	summary string
	run     func(args []string, stdout, stderr io.Writer) error
}

var commands = map[string]command{}

func register(name string, c command) { commands[name] = c }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(stderr)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	c, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "archmodel: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
	if err := c.run(args[1:], stdout, stderr); err != nil {
		switch err {
		case errUsage:
			return 2
		case errFailed:
			return 1
		}
		fmt.Fprintf(stderr, "archmodel %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: archmodel <command> [flags]\n\ncommands:")
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-8s %s\n", n, commands[n].summary)
	}
}
