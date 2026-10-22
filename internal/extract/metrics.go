package extract

import (
	"math"
	"strconv"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

func round3(f float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(f, 'f', 3, 64), 64)
	return v
}

// ComputeMetrics derives the package metrics of one Module from its
// production-graph degrees (distinct Modules, not imports) and its exported
// types. Instability I = Ce/(Ca+Ce), defined as 0 (and recorded as undefined)
// when Ca+Ce = 0. Abstractness A = interfaces/exported types, 0 with no
// exported types. Distance D = |A+I-1|.
func ComputeMetrics(ca, ce, exportedTypes, abstractTypes int) *model.Metrics {
	m := &model.Metrics{Ca: ca, Ce: ce, ExportedTypes: exportedTypes, AbstractTypes: abstractTypes}
	var i, a float64
	if ca+ce > 0 {
		m.InstabilityDefined = true
		i = float64(ce) / float64(ca+ce)
	}
	if exportedTypes > 0 {
		a = float64(abstractTypes) / float64(exportedTypes)
	}
	m.Instability = round3(i)
	m.Abstractness = round3(a)
	m.Distance = round3(math.Abs(a + i - 1))
	return m
}
