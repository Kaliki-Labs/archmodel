// Package explorer assembles the Explorer: one self-contained HTML file made
// of the vendored graph libraries, the Explorer's own CSS and JS, the shared
// palette and the Architecture model embedded as JSON (ADR-0004).
package explorer

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kaliki-Labs/archmodel/internal/model"
	"github.com/Kaliki-Labs/archmodel/internal/style"
)

//go:embed assets/*
var assets embed.FS

// Options are the page-level settings that are not part of the model.
type Options struct {
	// SourceURLTemplate has {path}, {line}, {ref} placeholders; empty shows
	// path:line as text.
	SourceURLTemplate string
	// Ref fills {ref}.
	Ref string
	// Live makes the page listen for reload and build-error events on /events
	// (archmodel serve).
	Live bool
	// Diff is the optional diff object for diff mode (PR 4); the page reads it
	// from the embedded "archmodel-diff" block. Nil means no diff.
	Diff any
}

type pageConfig struct {
	Source struct {
		URLTemplate string `json:"url_template"`
		Ref         string `json:"ref"`
	} `json:"source"`
	Live bool `json:"live"`
}

func asset(name string) string {
	b, err := assets.ReadFile("assets/" + name)
	if err != nil {
		panic(fmt.Sprintf("explorer: missing embedded asset %s: %v", name, err))
	}
	return string(b)
}

// scriptJSON encodes v as JSON that is safe inside a <script> element: "<",
// ">" and "&" are escaped (so "</script>" cannot appear), as are U+2028/9.
func scriptJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Render returns the Explorer HTML for the model.
func Render(m *model.Model, opts Options) ([]byte, error) {
	m.Normalize()
	modelJSON, err := scriptJSON(m)
	if err != nil {
		return nil, err
	}
	var cfg pageConfig
	cfg.Source.URLTemplate = opts.SourceURLTemplate
	cfg.Source.Ref = opts.Ref
	cfg.Live = opts.Live
	cfgJSON, err := scriptJSON(cfg)
	if err != nil {
		return nil, err
	}
	diffBlock := ""
	if opts.Diff != nil {
		d, err := scriptJSON(opts.Diff)
		if err != nil {
			return nil, err
		}
		diffBlock = `<script type="application/json" id="archmodel-diff">` + d + `</script>`
	}
	// A Replacer scans the template once, so inserted content (libraries,
	// model) is never re-scanned for placeholders.
	r := strings.NewReplacer(
		"/*@PALETTE@*/", style.CSSVars(),
		"/*@CSS@*/", asset("explorer.css"),
		"<!--@DIFF@-->", diffBlock,
		"@CONFIG_JSON@", cfgJSON,
		"@MODEL_JSON@", modelJSON,
		"/*@LIBS@*/", asset("cytoscape.min.js")+"\n;"+asset("dagre.min.js")+"\n;"+asset("cytoscape-dagre.js"),
		"/*@JS@*/", asset("explorer.js"),
	)
	return []byte(r.Replace(asset("explorer.html"))), nil
}
