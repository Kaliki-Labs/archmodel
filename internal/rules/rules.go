// Package rules evaluates the Guardrails rules (ADR-0008) over an
// Architecture model. The results are embedded in the model so every reader
// (check, query, Explorer, diff) sees the same violations.
package rules

import (
	"fmt"
	"sort"

	"github.com/Kaliki-Labs/archmodel/internal/config"
	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Rule IDs, in the order of the brief.
const (
	InwardOnly              = "inward_only"
	NoDomainToDomain        = "no_domain_to_domain"
	NoImportCompositionRoot = "no_import_composition_root"
	TestOnlyOutOfProd       = "test_only_out_of_prod"
	PackageDoc              = "package_doc"
)

// IDs lists every rule in policy order.
var IDs = []string{InwardOnly, NoDomainToDomain, NoImportCompositionRoot, TestOnlyOutOfProd, PackageDoc}

// Policy returns every rule with its severity, Rationale and fix hint from
// the config, in policy order.
func Policy(cfg *config.Config) []model.Rule {
	out := make([]model.Rule, 0, len(IDs))
	for _, id := range IDs {
		r := cfg.Rules[id]
		out = append(out, model.Rule{ID: id, Severity: r.Severity, Rationale: r.Rationale, Fix: r.Fix})
	}
	return out
}

// Evaluate checks the model against every rule. Violations come back in
// policy order, then by Module, so output is deterministic.
func Evaluate(m *model.Model, cfg *config.Config) []model.Violation {
	policy := map[string]model.Rule{}
	for _, r := range Policy(cfg) {
		policy[r.ID] = r
	}
	mods := map[string]model.Module{}
	for _, mod := range m.Modules {
		mods[mod.ID] = mod
	}
	out := []model.Violation{}
	add := func(rule, module, to, msg string, sites []model.Site) {
		p := policy[rule]
		subject := module
		if to != "" {
			subject = module + " -> " + to
		}
		out = append(out, model.Violation{
			Rule: rule, Severity: p.Severity, Subject: subject, Module: module, To: to,
			Message: msg, Rationale: p.Rationale, Fix: p.Fix, Sites: sites,
		})
	}

	for _, d := range m.Dependencies {
		// Rules 1-4 read the production graph: test-only edges never violate,
		// and Test-only Modules are outside it (ADR-0008).
		if d.TestOnly {
			continue
		}
		from, to := mods[d.From], mods[d.To]
		if from.Kind == model.KindTestOnly {
			continue
		}
		sites := prodSites(d)
		if from.Level != nil && to.Level != nil && *to.Level < *from.Level {
			add(InwardOnly, d.From, d.To, fmt.Sprintf("%s (%s) imports %s (%s), which is further out",
				d.From, kindName(from.Kind), d.To, kindName(to.Kind)), sites)
		}
		if from.Kind == model.KindDomain && to.Kind == model.KindDomain {
			add(NoDomainToDomain, d.From, d.To, fmt.Sprintf("Domain %s imports Domain %s", d.From, d.To), sites)
		}
		if to.Kind == model.KindCompositionRoot && from.Kind != model.KindCompositionRoot {
			add(NoImportCompositionRoot, d.From, d.To, fmt.Sprintf("%s imports the Composition root %s", d.From, d.To), sites)
		}
		if to.Kind == model.KindTestOnly {
			add(TestOnlyOutOfProd, d.From, d.To, fmt.Sprintf("production code in %s imports the Test-only module %s", d.From, d.To), sites)
		}
	}
	for _, mod := range m.Modules {
		// Missing (ADR-0003) and generated packages have no doc comment to
		// maintain.
		if mod.Present && !mod.Generated && mod.Purpose == "" {
			add(PackageDoc, mod.ID, "", fmt.Sprintf("%s has no package doc comment", mod.ID),
				[]model.Site{{File: mod.Dir}})
		}
	}

	rank := map[string]int{}
	for i, id := range IDs {
		rank[id] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Rule != b.Rule {
			return rank[a.Rule] < rank[b.Rule]
		}
		if a.Module != b.Module {
			return a.Module < b.Module
		}
		return a.To < b.To
	})
	return out
}

// prodSites are the import sites in non-test files.
func prodSites(d model.Dependency) []model.Site {
	var s []model.Site
	for _, x := range d.Sites {
		if !x.Test {
			s = append(s, x)
		}
	}
	return s
}

func kindName(k string) string {
	switch k {
	case model.KindCompositionRoot:
		return "Composition root"
	case model.KindDomain:
		return "Domain"
	case model.KindInfrastructure:
		return "Infrastructure module"
	case model.KindFoundation:
		return "Foundation"
	case model.KindTestOnly:
		return "Test-only module"
	}
	return k
}
