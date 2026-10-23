package diffimage

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const badgeR = 9.0

type painter struct {
	img *image.RGBA
	s   float64
}

func rgba(h string) color.RGBA {
	r, g, b := hexRGB(h)
	return color.RGBA{r, g, b, 255}
}

// fill rasterizes the polygons (all with the same winding) in one color.
func (p *painter) fill(col string, polys ...[]Pt) {
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, poly := range polys {
		for _, q := range poly {
			minX, maxX = math.Min(minX, q.X*p.s), math.Max(maxX, q.X*p.s)
			minY, maxY = math.Min(minY, q.Y*p.s), math.Max(maxY, q.Y*p.s)
		}
	}
	r := image.Rect(int(math.Floor(minX))-1, int(math.Floor(minY))-1, int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1).
		Intersect(p.img.Bounds())
	if r.Empty() {
		return
	}
	z := vector.NewRasterizer(r.Dx(), r.Dy())
	for _, poly := range polys {
		for i, q := range poly {
			x, y := float32(q.X*p.s-float64(r.Min.X)), float32(q.Y*p.s-float64(r.Min.Y))
			if i == 0 {
				z.MoveTo(x, y)
			} else {
				z.LineTo(x, y)
			}
		}
		z.ClosePath()
	}
	z.Draw(p.img, r, image.NewUniform(rgba(col)), image.Point{})
}

// stroke draws a polyline of the given width, optionally dashed. Segment
// quads and round joins share one winding so overlaps do not cancel.
func (p *painter) stroke(col string, pts []Pt, w float64, dash []float64, closed bool) {
	if closed {
		pts = append(append([]Pt(nil), pts...), pts[0])
	}
	for _, run := range dashRuns(pts, dash) {
		hw := w / 2
		var polys [][]Pt
		for i := 0; i+1 < len(run); i++ {
			a, b := run[i], run[i+1]
			dx, dy := b.X-a.X, b.Y-a.Y
			d := math.Hypot(dx, dy)
			if d == 0 {
				continue
			}
			nx, ny := -dy/d*hw, dx/d*hw
			polys = append(polys, []Pt{{a.X + nx, a.Y + ny}, {b.X + nx, b.Y + ny}, {b.X - nx, b.Y - ny}, {a.X - nx, a.Y - ny}})
		}
		for _, q := range run {
			polys = append(polys, circle(q, hw, false))
		}
		if len(polys) > 0 {
			p.fill(col, polys...)
		}
	}
}

// circle returns a polygon; ccw=false matches the winding of stroke quads.
func circle(c Pt, r float64, ccw bool) []Pt {
	const n = 14
	out := make([]Pt, n)
	for i := range out {
		a := 2 * math.Pi * float64(i) / n
		if !ccw {
			a = -a
		}
		out[i] = Pt{c.X + r*math.Cos(a), c.Y + r*math.Sin(a)}
	}
	return out
}

// dashRuns cuts a polyline into dash runs; with no pattern it is one run.
func dashRuns(pts []Pt, dash []float64) [][]Pt {
	if len(dash) < 2 {
		return [][]Pt{pts}
	}
	var runs [][]Pt
	var cur []Pt
	on, left := true, dash[0]
	cur = append(cur, pts[0])
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		seg := math.Hypot(b.X-a.X, b.Y-a.Y)
		pos := 0.0
		for seg-pos > left {
			pos += left
			t := pos / seg
			q := Pt{a.X + (b.X-a.X)*t, a.Y + (b.Y-a.Y)*t}
			if on {
				cur = append(cur, q)
				runs = append(runs, cur)
				cur = nil
			} else {
				cur = []Pt{q}
			}
			on = !on
			if on {
				left = dash[0]
			} else {
				left = dash[1]
			}
		}
		left -= seg - pos
		if on {
			cur = append(cur, b)
		}
	}
	if on && len(cur) > 1 {
		runs = append(runs, cur)
	}
	return runs
}

// roundRect returns the outline of a rounded rectangle.
func roundRect(x, y, w, h, r float64) []Pt {
	const steps = 6
	var out []Pt
	corner := func(cx, cy, from float64) {
		for i := 0; i <= steps; i++ {
			a := from + math.Pi/2*float64(i)/steps
			out = append(out, Pt{cx + r*math.Cos(a), cy + r*math.Sin(a)})
		}
	}
	corner(x+w-r, y+r, -math.Pi/2)
	corner(x+w-r, y+h-r, 0)
	corner(x+r, y+h-r, math.Pi/2)
	corner(x+r, y+r, math.Pi)
	return out
}

func (p *painter) text(s string, boldFace bool, size float64, col string, x, baseline float64, center bool) {
	f := face(boldFace, size*p.s)
	d := font.Drawer{Dst: p.img, Src: image.NewUniform(rgba(col)), Face: f}
	w := d.MeasureString(s)
	px := fx(x * p.s)
	if center {
		px -= w / 2
	}
	d.Dot = fixed.Point26_6{X: px, Y: fx(baseline * p.s)}
	d.DrawString(s)
}

// PNG renders the layout at Scale times its logical size.
func (l *Layout) PNG() ([]byte, error) {
	s := float64(Scale)
	img := image.NewRGBA(image.Rect(0, 0, int(math.Ceil(l.W*s)), int(math.Ceil(l.H*s))))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	p := &painter{img: img, s: s}

	p.text(l.Title, true, titleSize, inkColor, Margin, Margin+14, false)

	for _, e := range l.Edges {
		p.stroke(e.Color, e.Pts, e.Width, e.Dash, false)
		p.fill(e.Color, []Pt{e.Arrow[0], e.Arrow[1], e.Arrow[2]})
	}

	for _, n := range l.Nodes {
		outline := roundRect(n.X, n.Y, n.W, n.H, radius)
		p.fill(n.Fill, outline)
		if n.Outline != "" {
			dash := []float64(nil)
			if n.Dashed {
				dash = []float64{7, 4}
			}
			p.stroke(n.Outline, outline, n.OutlineW, dash, true)
		} else {
			p.stroke(mutedStroke, outline, 1, nil, true)
		}
		cx := n.X + n.W/2
		labelY := n.Y + 8 + labelSize
		if len(n.Notes) == 0 {
			labelY = n.Y + n.H/2 + labelSize*0.35
		}
		p.text(n.ID, true, labelSize, n.Text, cx, labelY, true)
		if n.Strike {
			w := measure(true, labelSize, n.ID)
			y := labelY - labelSize*0.32
			p.stroke(n.Text, []Pt{{cx - w/2, y}, {cx + w/2, y}}, 1.6, nil, false)
		}
		for i, note := range n.Notes {
			p.text(note, false, noteSize, n.Text, cx, labelY+noteLineHeight*float64(i+1)-1, true)
		}
		if n.Badge != "" {
			bx, by := n.X+n.W-2, n.Y+2
			p.fill(n.BadgeColor, circle(Pt{bx, by}, badgeR, true))
			p.text(n.Badge, true, 13, "#ffffff", bx, by+4.5, true)
		}
	}

	if len(l.Legend) > 0 {
		p.stroke(legendRule, []Pt{{Margin, l.LegendTop}, {l.W - Margin, l.LegendTop}}, 1, nil, false)
	}
	for _, it := range l.Legend {
		cy := it.Y + 6
		if it.Box {
			box := roundRect(it.X, cy-6, 22, 12, 3)
			p.fill(it.Fill, box)
			if it.Width > 0 {
				p.stroke(it.Color, box, it.Width, it.Dash, true)
			}
		} else {
			p.stroke(it.Color, []Pt{{it.X, cy}, {it.X + 22, cy}}, it.Width, it.Dash, false)
		}
		p.text(it.Label, false, legendSize, inkColor, it.X+28, cy+4, false)
	}
	if l.Footer != "" {
		p.text(l.Footer, false, legendSize, inkColor, Margin, l.FooterY+10, false)
	}

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
