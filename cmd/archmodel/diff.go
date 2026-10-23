package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/diff"
	"github.com/Kaliki-Labs/archmodel/internal/diffimage"
)

func init() {
	register("diff", command{summary: "Architecture diff of two models: Markdown, JSON, SVG/PNG", run: runDiff})
}

// relativeImage is the PNG's path relative to the Markdown file, or just its
// file name when the Markdown goes to stdout.
func relativeImage(mdPath, png string) string {
	if mdPath != "-" {
		if rel, err := filepath.Rel(filepath.Dir(mdPath), png); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(png)
}

// imageBase strips a .svg or .png extension, so --image out/diff, out/diff.svg
// and out/diff.png all write out/diff.svg and out/diff.png.
func imageBase(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".svg", ".png":
		return strings.TrimSuffix(path, filepath.Ext(path))
	}
	return path
}

func writeFile(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

func runDiff(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	basePath := fs.String("base", "", "base model.json (required)")
	headPath := fs.String("head", "", "head model.json (required)")
	mdPath := fs.String("markdown", "-", `Markdown PR comment: file, or "-" for stdout`)
	jsonPath := fs.String("json", "", `diff JSON: file, or "-" for stdout`)
	imagePath := fs.String("image", "", "changed-subgraph image: writes <path>.svg and <path>.png (a .svg or .png extension is dropped first); nothing is written when nothing changed")
	imageURL := fs.String("image-url", "", "URL of the image in the Markdown (default: the PNG's path relative to the --markdown file, so `gh pr comment --attach` can upload it)")
	mermaid := fs.Bool("mermaid", true, "add the Mermaid version of the subgraph, folded in <details>")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errUsage
	}
	if fs.NArg() > 0 || *basePath == "" || *headPath == "" {
		fmt.Fprintln(stderr, "usage: archmodel diff --base <model.json> --head <model.json> [--markdown <path|->] [--json <path|->] [--image <path>] [--image-url <url>] [--mermaid=false]")
		return errUsage
	}
	mdExplicit := false
	fs.Visit(func(f *flag.Flag) { mdExplicit = mdExplicit || f.Name == "markdown" })
	if mdExplicit && *mdPath == "-" && *jsonPath == "-" {
		fmt.Fprintln(stderr, `archmodel diff: --markdown and --json cannot both write to stdout; give one a file`)
		return errUsage
	}
	base, err := readModel(*basePath)
	if err != nil {
		return err
	}
	head, err := readModel(*headPath)
	if err != nil {
		return err
	}
	d := diff.Compute(base, head)

	// The Markdown names the image only when one is being written.
	opts := diff.MarkdownOptions{Mermaid: *mermaid}
	draw := *imagePath != "" && d.Changed && d.Subgraph.Drawable()
	if draw {
		opts.ImageURL = *imageURL
		if opts.ImageURL == "" {
			opts.ImageURL = relativeImage(*mdPath, imageBase(*imagePath)+".png")
		}
	}
	md := d.Markdown(opts)

	// Markdown goes to stdout by default, unless "--json -" took stdout.
	if mdExplicit || *jsonPath != "-" {
		if *mdPath == "-" {
			if _, err := io.WriteString(stdout, md); err != nil {
				return err
			}
		} else if err := writeFile(*mdPath, []byte(md)); err != nil {
			return err
		}
	}
	if *jsonPath != "" {
		data, err := d.JSON()
		if err != nil {
			return err
		}
		if *jsonPath == "-" {
			_, err = stdout.Write(data)
		} else {
			err = writeFile(*jsonPath, data)
		}
		if err != nil {
			return err
		}
	}
	if draw {
		l := diffimage.Compute(d.Subgraph)
		pngData, err := l.PNG()
		if err != nil {
			return err
		}
		b := imageBase(*imagePath)
		if err := writeFile(b+".svg", l.SVG()); err != nil {
			return err
		}
		if err := writeFile(b+".png", pngData); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "wrote %s.svg and %s.png (%d modules, %d dependencies)\n", b, b, len(d.Subgraph.Nodes), len(d.Subgraph.Edges))
	}
	return nil
}
