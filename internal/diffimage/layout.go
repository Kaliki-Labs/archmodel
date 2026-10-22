// Package diffimage draws the Architecture diff's changed subgraph as SVG and
// PNG in pure Go (ADR-0005). One layered layout feeds both writers, and the
// colors come from internal/style, the palette the Explorer's CSS uses too.
package diffimage

import (
	"math"
	"sort"

	"github.com/Kaliki-Labs/archmodel/internal/diff"
	"github.com/Kaliki-Labs/archmodel/internal/style"
)

// Geometry, in logical units. The PNG draws at Scale times this, so the widest
// image is (MaxRowWidth + 2*Margin) * Scale = 1200 px.
const (
	MaxRowWidth = 560.0
	Margin      = 20.0
	Scale       = 2

	minCanvasWidth = 420.0
	nodeMinWidth   = 96.0
	nodePadX       = 14.0
	nodeBaseHeight = 38.0
	noteLineHeight = 14.0
	nodeGap        = 28.0
	rowGap         = 62.0
	titleHeight    = 34.0
	radius         = 8.0

	labelSize  = 13.0
	noteSize   = 10.5
	titleSize  = 14.0
	legendSize = 11.0
)

// Pt is a point.
type Pt struct{ X, Y float64 }

// LNode is a placed Module.
type LNode struct {
	ID         string
	Notes      []string
	Status     string
	X, Y, W, H float64
	Fill       string // fill color
	Text       string // label color
	Outline    string // "" for none
	OutlineW   float64
	Dashed     bool
	Strike     bool
	Badge      string // "+", "-", "~" or ""
	BadgeColor string
}

// LEdge is a routed dependency.
type LEdge struct {
	Pts   []Pt // flattened path, source to target
	Arrow [3]Pt
	Color string
	Width float64
	Dash  []float64
}

// LegendItem is one legend entry.
type LegendItem struct {
	Label string
	Box   bool // a node swatch (else a line swatch)
	Fill  string
	Color string // outline or line color
	Width float64
	Dash  []float64
	X, Y  float64
}

// Layout is a laid-out subgraph, ready for either writer.
type Layout struct {
	W, H      float64
	Title     string
	Nodes     []LNode
	Edges     []LEdge
	Legend    []LegendItem
	LegendTop float64
	Footer    string
	FooterY   float64
}

const (
	inkColor    = "#1c2330"
	mutedFill   = "#e6e9ee"
	legendRule  = "#dde1e7"
	mutedStroke = "#aab0ba"
)

var (
	dashRemoved = []float64{8, 5}
	dashTest    = []float64{4, 4}
)

// Compute lays out the subgraph. Rows are Levels (outermost at the top,
// Test-only last); within a row Modules are ordered by the barycenter of their
// neighbours in the adjacent rows, then wrapped to MaxRowWidth.
func Compute(s diff.Subgraph) *Layout {
	l := &Layout{Title: "Architecture diff: changed subgraph"}

	levelRank := map[string]int{}
	for i, k := range s.Levels {
		levelRank[k] = i
	}
	rankOf := func(kind string) int {
		if r, ok := levelRank[kind]; ok {
			return r
		}
		return len(s.Levels)
	}

	// Group into ranks (only the non-empty ones).
	byRank := map[int][]int{} // rank -> node indexes
	for i, n := range s.Nodes {
		byRank[rankOf(n.Kind)] = append(byRank[rankOf(n.Kind)], i)
	}
	var ranks []int
	for r := range byRank {
		ranks = append(ranks, r)
	}
	sort.Ints(ranks)
	idx := map[string]int{}
	for i, n := range s.Nodes {
		idx[n.ID] = i
	}
	rankIdx := map[int]int{} // rank -> position in ranks
	for i, r := range ranks {
		rankIdx[r] = i
	}

	// Barycenter ordering, a few sweeps down and up.
	nbr := make([][]int, len(s.Nodes))
	for _, e := range s.Edges {
		a, aok := idx[e.From]
		b, bok := idx[e.To]
		if !aok || !bok {
			continue
		}
		nbr[a] = append(nbr[a], b)
		nbr[b] = append(nbr[b], a)
	}
	pos := make([]float64, len(s.Nodes))
	setPos := func(r int) {
		for i, n := range byRank[r] {
			pos[n] = (float64(i) + 0.5) / float64(len(byRank[r]))
		}
	}
	for _, r := range ranks {
		setPos(r)
	}
	sweep := func(r, other int) {
		list := byRank[r]
		bary := map[int]float64{}
		for _, n := range list {
			sum, cnt := 0.0, 0
			for _, m := range nbr[n] {
				if rankOf(s.Nodes[m].Kind) == other {
					sum += pos[m]
					cnt++
				}
			}
			if cnt > 0 {
				bary[n] = sum / float64(cnt)
			} else {
				bary[n] = pos[n]
			}
		}
		sort.SliceStable(list, func(a, b int) bool { return bary[list[a]] < bary[list[b]] })
		setPos(r)
	}
	for it := 0; it < 4; it++ {
		for i := 1; i < len(ranks); i++ {
			sweep(ranks[i], ranks[i-1])
		}
		for i := len(ranks) - 2; i >= 0; i-- {
			sweep(ranks[i], ranks[i+1])
		}
	}

	// Size nodes.
	type sized struct {
		i    int
		w, h float64
	}
	sizes := make([]sized, len(s.Nodes))
	for i, n := range s.Nodes {
		w := measure(true, labelSize, n.ID)
		for _, note := range n.Notes {
			if nw := measure(false, noteSize, note); nw > w {
				w = nw
			}
		}
		w += 2 * nodePadX
		if w < nodeMinWidth {
			w = nodeMinWidth
		}
		if w > MaxRowWidth {
			w = MaxRowWidth
		}
		sizes[i] = sized{i, math.Ceil(w), nodeBaseHeight + noteLineHeight*float64(len(n.Notes))}
	}

	// Wrap ranks into rows and place them.
	type row struct{ nodes []int }
	var rows []row
	for _, r := range ranks {
		var cur row
		width := 0.0
		for _, n := range byRank[r] {
			w := sizes[n].w
			if len(cur.nodes) > 0 && width+nodeGap+w > MaxRowWidth {
				rows = append(rows, cur)
				cur, width = row{}, 0
			}
			if len(cur.nodes) > 0 {
				width += nodeGap
			}
			cur.nodes = append(cur.nodes, n)
			width += w
		}
		rows = append(rows, cur)
	}
	rowWidth := func(r row) float64 {
		w := 0.0
		for i, n := range r.nodes {
			if i > 0 {
				w += nodeGap
			}
			w += sizes[n].w
		}
		return w
	}
	contentW := minCanvasWidth - 2*Margin
	for _, r := range rows {
		if w := rowWidth(r); w > contentW {
			contentW = w
		}
	}
	l.W = contentW + 2*Margin

	l.Nodes = make([]LNode, len(s.Nodes))
	y := Margin + titleHeight
	for _, r := range rows {
		h := 0.0
		for _, n := range r.nodes {
			if sizes[n].h > h {
				h = sizes[n].h
			}
		}
		x := Margin + (contentW-rowWidth(r))/2
		for _, n := range r.nodes {
			nd := s.Nodes[n]
			ln := LNode{ID: nd.ID, Notes: nd.Notes, Status: nd.Status, X: x, Y: y + (h-sizes[n].h)/2, W: sizes[n].w, H: sizes[n].h}
			styleNode(&ln, nd)
			l.Nodes[n] = ln
			x += sizes[n].w + nodeGap
		}
		y += h + rowGap
	}
	y -= rowGap

	// Route edges. Edges between rows leave the bottom (or top, for an edge
	// pointing up) of a Module through ports spread along the side in the
	// order of the other end, so they fan out instead of piling up.
	type routed struct {
		a, b int
		span float64
	}
	var rs []routed
	var kept []diff.Edge
	for _, e := range s.Edges {
		a, aok := idx[e.From]
		b, bok := idx[e.To]
		if !aok || !bok || a == b {
			continue
		}
		span := math.Abs(float64(rankIdx[rankOf(s.Nodes[a].Kind)]-rankIdx[rankOf(s.Nodes[b].Kind)])) +
			math.Abs(pos[a]-pos[b])*float64(len(byRank[rankOf(s.Nodes[a].Kind)]))
		rs = append(rs, routed{a, b, span})
		kept = append(kept, e)
	}
	type portKey struct {
		node   int
		bottom bool
	}
	type portUse struct {
		edge   int
		source bool
		otherX float64
	}
	ports := map[portKey][]portUse{}
	cx := func(n int) float64 { return l.Nodes[n].X + l.Nodes[n].W/2 }
	sameRow := func(r routed) bool {
		return math.Abs(l.Nodes[r.a].Y+l.Nodes[r.a].H/2-l.Nodes[r.b].Y-l.Nodes[r.b].H/2) < 1
	}
	for i, r := range rs {
		if sameRow(r) {
			continue
		}
		down := l.Nodes[r.b].Y > l.Nodes[r.a].Y // source above target
		ports[portKey{r.a, down}] = append(ports[portKey{r.a, down}], portUse{i, true, cx(r.b)})
		ports[portKey{r.b, !down}] = append(ports[portKey{r.b, !down}], portUse{i, false, cx(r.a)})
	}
	portX := map[[2]int]float64{} // edge, 0 = source end, 1 = target end
	for k, uses := range ports {
		sort.SliceStable(uses, func(i, j int) bool {
			if uses[i].otherX != uses[j].otherX {
				return uses[i].otherX < uses[j].otherX
			}
			return uses[i].edge < uses[j].edge
		})
		n := l.Nodes[k.node]
		for i, u := range uses {
			end := 1
			if u.source {
				end = 0
			}
			portX[[2]int{u.edge, end}] = n.X + n.W*float64(i+1)/float64(len(uses)+1)
		}
	}
	for i, r := range rs {
		var le LEdge
		if sameRow(r) {
			le = arcRoute(l.Nodes[r.a], l.Nodes[r.b], r.span)
		} else {
			le = curveRoute(l.Nodes[r.a], l.Nodes[r.b], portX[[2]int{i, 0}], portX[[2]int{i, 1}])
		}
		styleEdge(&le, kept[i])
		l.Edges = append(l.Edges, le)
	}

	// Legend and footer.
	y += Margin
	l.LegendTop = y
	l.Legend, y = legend(s, y+10, l.W)
	if s.Omitted > 0 {
		l.Footer = "+" + itoa(s.Omitted) + " more changed modules not shown"
		l.FooterY = y + 6
		y += 22
	}
	l.H = y + Margin - 6
	return l
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func styleNode(ln *LNode, n diff.Node) {
	fill := style.Level(n.Kind)
	if fill == "" {
		fill = style.Get("c-subpackage")
	}
	ln.Fill, ln.Text = fill, "#ffffff"
	switch n.Status {
	case diff.StatusAdded:
		ln.Outline, ln.OutlineW, ln.Badge, ln.BadgeColor = style.Added(), 3.5, "+", style.Added()
	case diff.StatusRemoved:
		ln.Fill = blend(fill, "#ffffff", 0.45)
		ln.Text = inkColor
		ln.Outline, ln.OutlineW, ln.Dashed, ln.Strike = style.Removed(), 3, true, true
		ln.Badge, ln.BadgeColor = "-", style.Removed()
	case diff.StatusChanged:
		ln.Outline, ln.OutlineW, ln.Badge, ln.BadgeColor = style.Changed(), 3.5, "~", style.Changed()
	default: // context
		ln.Fill = blend(fill, "#ffffff", 0.7)
		ln.Text = inkColor
	}
}

func styleEdge(le *LEdge, e diff.Edge) {
	le.Color, le.Width = style.Get("edge"), 1.6
	if e.TestOnly {
		le.Dash = dashTest
	}
	switch e.Status {
	case diff.StatusAdded:
		le.Color, le.Width = style.Added(), 2.6
	case diff.StatusRemoved:
		le.Color, le.Width, le.Dash = style.Removed(), 2.2, dashRemoved
	case diff.StatusChanged:
		le.Color, le.Width = style.Changed(), 2.4
	}
	if e.Violation {
		le.Color, le.Width = style.Violation(), 3.2
	}
}

// arcRoute joins two nodes in the same row with an arc over the top.
func arcRoute(a, b LNode, span float64) LEdge {
	p0, p2 := Pt{a.X + a.W/2, a.Y}, Pt{b.X + b.W/2, b.Y}
	ctl := Pt{(p0.X + p2.X) / 2, a.Y - 22 - 7*math.Min(span, 4)}
	var pts []Pt
	for i := 0; i <= 20; i++ {
		t := float64(i) / 20
		pts = append(pts, Pt{
			(1-t)*(1-t)*p0.X + 2*(1-t)*t*ctl.X + t*t*p2.X,
			(1-t)*(1-t)*p0.Y + 2*(1-t)*t*ctl.Y + t*t*p2.Y,
		})
	}
	return finish(pts)
}

// curveRoute joins nodes in different rows with an S-shaped cubic between
// ports on the facing sides, with vertical tangents at both ends.
func curveRoute(a, b LNode, sx, tx float64) LEdge {
	var sy, ty float64
	if b.Y > a.Y {
		sy, ty = a.Y+a.H, b.Y
	} else {
		sy, ty = a.Y, b.Y+b.H
	}
	dy := (ty - sy) / 2
	var pts []Pt
	for i := 0; i <= 24; i++ {
		t := float64(i) / 24
		u := 1 - t
		pts = append(pts, Pt{
			u*u*u*sx + 3*u*u*t*sx + 3*u*t*t*tx + t*t*t*tx,
			u*u*u*sy + 3*u*u*t*(sy+dy) + 3*u*t*t*(ty-dy) + t*t*t*ty,
		})
	}
	return finish(pts)
}

// finish adds the arrowhead and stops the line at its base so a thick line
// does not poke out of the tip.
func finish(pts []Pt) LEdge {
	e := LEdge{Pts: pts}
	e.Arrow = arrow(pts[len(pts)-2], pts[len(pts)-1])
	e.Pts[len(e.Pts)-1] = Pt{(e.Arrow[1].X + e.Arrow[2].X) / 2, (e.Arrow[1].Y + e.Arrow[2].Y) / 2}
	return e
}

// arrow returns the arrowhead triangle (tip, base left, base right) for a
// segment ending at to.
func arrow(from, to Pt) [3]Pt {
	const length, half = 10.0, 4.8
	dx, dy := to.X-from.X, to.Y-from.Y
	d := math.Hypot(dx, dy)
	if d == 0 {
		dx, dy, d = 0, 1, 1
	}
	ux, uy := dx/d, dy/d
	bx, by := to.X-ux*length, to.Y-uy*length
	return [3]Pt{to, {bx - uy*half, by + ux*half}, {bx + uy*half, by - ux*half}}
}

// legend places the legend entries that apply to this subgraph, wrapping at
// width w, and returns them with the y below the last line.
func legend(s diff.Subgraph, y, w float64) ([]LegendItem, float64) {
	var items []LegendItem
	seenKind := map[string]bool{}
	statuses := map[string]bool{}
	for _, n := range s.Nodes {
		if !seenKind[n.Kind] {
			seenKind[n.Kind] = true
		}
		statuses[n.Status] = true
	}
	order := append(append([]string(nil), s.Levels...), "test_only")
	for _, k := range order {
		if seenKind[k] {
			delete(seenKind, k)
			items = append(items, LegendItem{Label: diff.KindLabel(k), Box: true, Fill: style.Level(k), Color: style.Level(k)})
		}
	}
	if statuses[diff.StatusAdded] {
		items = append(items, LegendItem{Label: "added", Box: true, Fill: "#ffffff", Color: style.Added(), Width: 3})
	}
	if statuses[diff.StatusRemoved] {
		items = append(items, LegendItem{Label: "removed", Box: true, Fill: "#ffffff", Color: style.Removed(), Width: 2.5, Dash: dashRemoved})
	}
	if statuses[diff.StatusChanged] {
		items = append(items, LegendItem{Label: "changed", Box: true, Fill: "#ffffff", Color: style.Changed(), Width: 3})
	}
	if statuses[diff.StatusContext] {
		items = append(items, LegendItem{Label: "unchanged context", Box: true, Fill: mutedFill, Color: mutedStroke, Width: 1})
	}
	var hasAdded, hasRemoved, hasViol, hasTest bool
	for _, e := range s.Edges {
		hasAdded = hasAdded || e.Status == diff.StatusAdded
		hasRemoved = hasRemoved || e.Status == diff.StatusRemoved
		hasViol = hasViol || e.Violation
		hasTest = hasTest || e.TestOnly
	}
	if hasAdded {
		items = append(items, LegendItem{Label: "new dependency", Color: style.Added(), Width: 2.6})
	}
	if hasRemoved {
		items = append(items, LegendItem{Label: "removed dependency", Color: style.Removed(), Width: 2.2, Dash: dashRemoved})
	}
	if hasViol {
		items = append(items, LegendItem{Label: "new violation", Color: style.Violation(), Width: 3.2})
	}
	if hasTest {
		items = append(items, LegendItem{Label: "test-only", Color: style.Get("edge"), Width: 1.6, Dash: dashTest})
	}
	x := Margin
	const swatch, gap, lineH = 22.0, 18.0, 20.0
	for i := range items {
		iw := swatch + 6 + measure(false, legendSize, items[i].Label)
		if x > Margin && x+iw > w-Margin {
			x = Margin
			y += lineH
		}
		items[i].X, items[i].Y = x, y
		x += iw + gap
	}
	return items, y + lineH
}

// blend mixes hex color a over b with weight t of a.
func blend(a, b string, t float64) string {
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	mix := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*t + float64(y)*(1-t))) }
	return rgbHex(mix(ar, br), mix(ag, bg), mix(ab, bb))
}
