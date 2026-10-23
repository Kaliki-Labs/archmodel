// Package metrics turns package metrics into plain-language readings. The
// Explorer and the diff reuse these strings, so the teaching lives here and
// not in the UI.
package metrics

import (
	"fmt"
	"strconv"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Thresholds for the descriptions. They only choose words; nothing is
// enforced (metrics are displayed, not checked, in v1).
const (
	mainSequenceD = 0.2 // D at or below this is "on the main sequence"
	lowEnd        = 0.3 // I or A at or below this is "stable" / "concrete"
	highEnd       = 0.7 // I or A at or above this is "unstable" / "abstract"
)

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// Reading explains a Module's I, A and D in one or two sentences, given its
// kind (Foundation and Composition root sit at the extremes by design).
func Reading(kind string, m *model.Metrics) string {
	if !m.InstabilityDefined {
		return "Ca=0 and Ce=0: nothing imports it and it imports no other module, so " +
			"instability is undefined (shown as 0) and its position on the main sequence says nothing. " +
			"Check whether it is unused."
	}
	i, a, d := m.Instability, m.Abstractness, m.Distance
	head := fmt.Sprintf("I=%s, A=%s: ", num(i), num(a))

	var iWord string
	switch {
	case i >= highEnd:
		iWord = "unstable"
	case i <= lowEnd:
		iWord = "stable"
	default:
		iWord = "moderately stable"
	}
	aWord := "mixed concrete and abstract"
	switch {
	case m.ExportedTypes == 0:
		aWord = "concrete (no exported types, so A=0)"
	case a >= highEnd:
		aWord = "abstract"
	case a <= lowEnd:
		aWord = "concrete"
	}
	desc := iWord + " and " + aWord

	switch {
	case i <= lowEnd && a <= lowEnd:
		if kind == model.KindFoundation {
			return head + "near (0,0): zone of pain, " + desc + ", hard to change. " +
				"Expected for a Foundation module, which many modules rely on and which should change rarely; keep it small and steady."
		}
		return head + "near (0,0): zone of pain, " + desc + ", hard to change. " +
			"Many modules depend on it but it offers no interfaces to absorb change; consider extracting an interface or reducing what depends on it."
	case i >= highEnd && a >= highEnd:
		return head + "near (1,1): zone of uselessness, " + desc + ". Interfaces that almost nothing depends on; check whether they are needed."
	case d <= mainSequenceD:
		switch {
		case i >= highEnd && a <= lowEnd && kind == model.KindCompositionRoot:
			return head + desc + ", on the main sequence. Expected for a Composition root: it wires things together and nothing depends on it."
		case i >= highEnd && a <= lowEnd:
			return head + desc + ", on the main sequence, which is fine for a leaf: little depends on it, so it is cheap to change."
		case i <= lowEnd:
			return head + desc + ", on the main sequence: stable, and abstract enough that its dependents are not locked in."
		}
		return head + desc + ", on the main sequence (D=" + num(d) + "): stability and abstraction are in balance."
	case a+i < 1:
		return head + desc + ", D=" + num(d) + ": off the main sequence, leaning towards the zone of pain. " +
			"More depends on it, or it depends on less, than its few interfaces justify."
	default:
		return head + desc + ", D=" + num(d) + ": off the main sequence, leaning towards the zone of uselessness. " +
			"More abstraction than the code that depends on it needs."
	}
}
