package extract

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// SpecResult is what one OpenAPI spec contributes to the model.
type SpecResult struct {
	Operations []model.Operation
	Schemas    []model.Schema
	Warnings   []model.Warning
}

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// ParseSpec reads an OpenAPI 3 document without a full OpenAPI library: only
// the parts the model needs. owner is the Domain that declares the spec;
// specPath is the spec's path relative to the project root.
func ParseSpec(owner, specPath string, data []byte) (*SpecResult, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", specPath, err)
	}
	root := &doc
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: not an OpenAPI document", specPath)
	}
	p := &specParser{owner: owner, spec: specPath, root: root, res: &SpecResult{}}
	p.schemas()
	p.operations()
	return p.res, nil
}

type specParser struct {
	owner string
	spec  string
	root  *yaml.Node
	res   *SpecResult
}

func (p *specParser) warn(code, msg string) {
	p.res.Warnings = append(p.res.Warnings, model.Warning{Code: code, Message: msg, Path: p.spec})
}

// --- yaml.Node helpers ---

func get(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func scalar(n *yaml.Node, key string) string {
	if v := get(n, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}

func dig(n *yaml.Node, keys ...string) *yaml.Node {
	for _, k := range keys {
		n = get(n, k)
	}
	return n
}

// pairs iterates a mapping node's (key, value) pairs in document order.
func pairs(n *yaml.Node, fn func(k string, key, v *yaml.Node)) {
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		fn(n.Content[i].Value, n.Content[i], n.Content[i+1])
	}
}

const refPrefix = "#/components/"

// schemaID turns "#/components/schemas/X" into "<owner>.X"; ok is false for
// any other kind of reference.
func (p *specParser) schemaID(ref string) (string, bool) {
	if name, ok := strings.CutPrefix(ref, refPrefix+"schemas/"); ok {
		return p.owner + "." + name, true
	}
	return "", false
}

// resolve follows a local $ref to components/<section>/<name> once.
func (p *specParser) resolve(n *yaml.Node, section string) *yaml.Node {
	if ref := scalar(n, "$ref"); ref != "" {
		if name, ok := strings.CutPrefix(ref, refPrefix+section+"/"); ok {
			if t := dig(p.root, "components", section, name); t != nil {
				return t
			}
		}
	}
	return n
}

// collectRefs gathers every schema $ref reachable inside n (properties, array
// items, allOf/oneOf/anyOf, additionalProperties, media types, ...).
func (p *specParser) collectRefs(n *yaml.Node, set map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "$ref" {
				if id, ok := p.schemaID(n.Content[i+1].Value); ok {
					set[id] = true
				} else if !strings.HasPrefix(n.Content[i+1].Value, refPrefix) {
					p.warn("openapi-external-ref", fmt.Sprintf("unsupported external $ref %q", n.Content[i+1].Value))
				}
				continue
			}
			p.collectRefs(n.Content[i+1], set)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			p.collectRefs(c, set)
		}
	}
}

func (p *specParser) refsOf(n *yaml.Node) []string {
	set := map[string]bool{}
	p.collectRefs(n, set)
	return sortedKeys(set)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// typeOf renders a schema node as a short type: Item, []Item, string, object.
func (p *specParser) typeOf(n *yaml.Node) string {
	if n == nil {
		return "any"
	}
	if ref := scalar(n, "$ref"); ref != "" {
		return ref[strings.LastIndex(ref, "/")+1:]
	}
	t := scalar(n, "type")
	switch t {
	case "array":
		return "[]" + p.typeOf(get(n, "items"))
	case "":
		for _, k := range []string{"allOf", "oneOf", "anyOf"} {
			if get(n, k) != nil {
				return k
			}
		}
		if get(n, "properties") != nil {
			return "object"
		}
		return "any"
	}
	return t
}

// --- schemas ---

func (p *specParser) schemas() {
	pairs(dig(p.root, "components", "schemas"), func(name string, _, s *yaml.Node) {
		sc := model.Schema{
			ID: p.owner + "." + name, Owner: p.owner, Name: name, Spec: p.spec,
			Type: p.typeOf(s), Description: strings.TrimSpace(scalar(s, "description")),
			Refs: p.refsOf(s),
		}
		required := map[string]bool{}
		if r := get(s, "required"); r != nil {
			for _, c := range r.Content {
				required[c.Value] = true
			}
		}
		pairs(get(s, "properties"), func(pn string, _, pv *yaml.Node) {
			sc.Properties = append(sc.Properties, model.Property{
				Name: pn, Type: p.typeOf(pv), Format: scalar(pv, "format"),
				Required: required[pn], Refs: p.refsOf(pv),
			})
		})
		// A schema does not reference itself for graph purposes.
		refs := sc.Refs[:0]
		for _, r := range sc.Refs {
			if r != sc.ID {
				refs = append(refs, r)
			}
		}
		sc.Refs = refs
		p.res.Schemas = append(p.res.Schemas, sc)
	})
}

// --- operations ---

func (p *specParser) operations() {
	global := p.securityNames(get(p.root, "security"))
	pairs(get(p.root, "paths"), func(route string, _, item *yaml.Node) {
		var shared []model.Parameter
		if ps := get(item, "parameters"); ps != nil {
			for _, pn := range ps.Content {
				shared = append(shared, p.parameter(pn))
			}
		}
		for _, m := range httpMethods {
			op := get(item, m)
			if op == nil {
				continue
			}
			var keyNode *yaml.Node
			pairs(item, func(k string, key, _ *yaml.Node) {
				if k == m {
					keyNode = key
				}
			})
			id := scalar(op, "operationId")
			if id == "" {
				p.warn("openapi-no-operation-id", fmt.Sprintf("%s %s has no operationId; skipped", strings.ToUpper(m), route))
				continue
			}
			o := model.Operation{
				ID: id, Owner: p.owner, Method: strings.ToUpper(m), Path: route,
				Summary: scalar(op, "summary"), Spec: p.spec, Line: keyNode.Line,
				Security: global,
			}
			if tags := get(op, "tags"); tags != nil {
				for _, t := range tags.Content {
					o.Tags = append(o.Tags, t.Value)
				}
			}
			if sec := get(op, "security"); sec != nil {
				o.Security = p.securityNames(sec)
			}
			if rl := get(op, "x-rate-limit"); rl != nil {
				var v map[string]any
				if err := rl.Decode(&v); err == nil {
					o.RateLimit = v
				}
			}
			o.Parameters = append(o.Parameters, shared...)
			if ps := get(op, "parameters"); ps != nil {
				for _, pn := range ps.Content {
					o.Parameters = append(o.Parameters, p.parameter(pn))
				}
			}
			if rb := p.resolve(get(op, "requestBody"), "requestBodies"); rb != nil {
				o.RequestSchemas = p.refsOf(get(rb, "content"))
			}
			pairs(get(op, "responses"), func(status string, _, rn *yaml.Node) {
				rn = p.resolve(rn, "responses")
				o.Responses = append(o.Responses, model.Response{
					Status: status, Description: scalar(rn, "description"),
					Schemas: p.refsOf(get(rn, "content")),
				})
			})
			p.res.Operations = append(p.res.Operations, o)
		}
	})
}

func (p *specParser) parameter(n *yaml.Node) model.Parameter {
	n = p.resolve(n, "parameters")
	return model.Parameter{
		Name: scalar(n, "name"), In: scalar(n, "in"),
		Required: scalar(n, "required") == "true",
		Type:     p.typeOf(get(n, "schema")),
	}
}

// securityNames lists the scheme names in a security requirement array.
func (p *specParser) securityNames(n *yaml.Node) []string {
	set := map[string]bool{}
	if n != nil {
		for _, req := range n.Content {
			pairs(req, func(name string, _, _ *yaml.Node) { set[name] = true })
		}
	}
	return sortedKeys(set)
}

// GoName converts an operationId to the Go method name oapi-codegen derives:
// the first letter of each word upper-cased, words split on non-alphanumerics
// (listItems -> ListItems, get_item -> GetItem).
func GoName(operationID string) string {
	var b strings.Builder
	up := true
	for _, r := range operationID {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9':
			s := string(r)
			if up {
				s = strings.ToUpper(s)
			}
			b.WriteString(s)
			up = false
		default:
			up = true
		}
	}
	return b.String()
}
