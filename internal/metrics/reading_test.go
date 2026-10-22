package metrics

import (
	"strings"
	"testing"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

func m(ca, ce int, i, a, d float64, types int) *model.Metrics {
	return &model.Metrics{Ca: ca, Ce: ce, Instability: i, InstabilityDefined: ca+ce > 0,
		Abstractness: a, Distance: d, ExportedTypes: types}
}

func TestReading(t *testing.T) {
	cases := []struct {
		name string
		kind string
		m    *model.Metrics
		want []string
	}{
		{"leaf", model.KindInfrastructure, m(1, 9, 0.9, 0, 0.1, 2),
			[]string{"I=0.9, A=0: unstable and concrete", "main sequence", "fine for a leaf"}},
		{"root", model.KindCompositionRoot, m(0, 5, 1, 0, 0, 0),
			[]string{"Composition root", "nothing depends on it"}},
		{"pain", model.KindInfrastructure, m(8, 0, 0, 0, 1, 3),
			[]string{"near (0,0)", "zone of pain", "stable and concrete", "hard to change"}},
		{"foundation pain is explained", model.KindFoundation, m(8, 0, 0, 0, 1, 3),
			[]string{"zone of pain", "Expected for a Foundation"}},
		{"uselessness", model.KindInfrastructure, m(0, 1, 1, 1, 1, 2),
			[]string{"near (1,1)", "zone of uselessness"}},
		{"stable abstract", model.KindInfrastructure, m(5, 0, 0, 1, 0, 2),
			[]string{"stable and abstract", "main sequence"}},
		{"undefined", model.KindInfrastructure, m(0, 0, 0, 0, 1, 1),
			[]string{"undefined", "unused"}},
		{"off sequence pain side", model.KindInfrastructure, m(1, 1, 0.5, 0, 0.5, 2),
			[]string{"D=0.5", "leaning towards the zone of pain"}},
		{"no exported types", model.KindInfrastructure, m(0, 1, 1, 0, 0, 0),
			[]string{"no exported types"}},
	}
	for _, c := range cases {
		got := Reading(c.kind, c.m)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q lacks %q", c.name, got, w)
			}
		}
	}
	if got := Reading(model.KindInfrastructure, m(0, 0, 0, 0, 1, 1)); strings.Contains(got, "zone of pain") {
		t.Errorf("undefined instability must not be called the zone of pain: %q", got)
	}
}
