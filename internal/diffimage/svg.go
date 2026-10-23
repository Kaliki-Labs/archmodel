package diffimage

import (
	"bytes"
	"fmt"
	"html"
	"strings"
)

const svgFont = "'Helvetica Neue', Helvetica, Arial, sans-serif"

func num(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}

func dashAttr(d []float64) string {
	if len(d) == 0 {
		return ""
	}
	return fmt.Sprintf(` stroke-dasharray="%s %s"`, num(d[0]), num(d[1]))
}

func esc(s string) string { return html.EscapeString(s) }

// SVG renders the layout as a standalone SVG document. The output is
// deterministic text.
func (l *Layout) SVG() []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%s" height="%s" viewBox="0 0 %s %s" font-family="%s">`+"\n",
		num(l.W), num(l.H), num(l.W), num(l.H), svgFont)
	fmt.Fprintf(&b, `<rect width="%s" height="%s" fill="#ffffff"/>`+"\n", num(l.W), num(l.H))
	fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="%s" font-weight="bold" fill="%s">%s</text>`+"\n",
		num(Margin), num(Margin+14), num(titleSize), inkColor, esc(l.Title))

	for _, e := range l.Edges {
		var pts []string
		for _, p := range e.Pts {
			pts = append(pts, num(p.X)+","+num(p.Y))
		}
		fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="%s" stroke-linejoin="round"%s/>`+"\n",
			strings.Join(pts, " "), e.Color, num(e.Width), dashAttr(e.Dash))
		fmt.Fprintf(&b, `<polygon points="%s,%s %s,%s %s,%s" fill="%s"/>`+"\n",
			num(e.Arrow[0].X), num(e.Arrow[0].Y), num(e.Arrow[1].X), num(e.Arrow[1].Y), num(e.Arrow[2].X), num(e.Arrow[2].Y), e.Color)
	}

	for _, n := range l.Nodes {
		stroke := ""
		if n.Outline != "" {
			stroke = fmt.Sprintf(` stroke="%s" stroke-width="%s"`, n.Outline, num(n.OutlineW))
			if n.Dashed {
				stroke += dashAttr([]float64{7, 4})
			}
		} else {
			stroke = fmt.Sprintf(` stroke="%s" stroke-width="1"`, mutedStroke)
		}
		fmt.Fprintf(&b, `<rect x="%s" y="%s" width="%s" height="%s" rx="%s" fill="%s"%s/>`+"\n",
			num(n.X), num(n.Y), num(n.W), num(n.H), num(radius), n.Fill, stroke)
		cx := n.X + n.W/2
		labelY := n.Y + 8 + labelSize
		if len(n.Notes) == 0 {
			labelY = n.Y + n.H/2 + labelSize*0.35
		}
		fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="%s" font-weight="bold" text-anchor="middle" fill="%s">%s</text>`+"\n",
			num(cx), num(labelY), num(labelSize), n.Text, esc(n.ID))
		if n.Strike {
			w := measure(true, labelSize, n.ID)
			fmt.Fprintf(&b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="1.6"/>`+"\n",
				num(cx-w/2), num(labelY-labelSize*0.32), num(cx+w/2), num(labelY-labelSize*0.32), n.Text)
		}
		for i, note := range n.Notes {
			fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="%s" text-anchor="middle" fill="%s">%s</text>`+"\n",
				num(cx), num(labelY+noteLineHeight*float64(i+1)-1), num(noteSize), n.Text, esc(note))
		}
		if n.Badge != "" {
			bx, by := n.X+n.W-2, n.Y+2
			fmt.Fprintf(&b, `<circle cx="%s" cy="%s" r="%s" fill="%s"/>`+"\n", num(bx), num(by), num(badgeR), n.BadgeColor)
			fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="13" font-weight="bold" text-anchor="middle" fill="#ffffff">%s</text>`+"\n",
				num(bx), num(by+4.5), n.Badge)
		}
	}

	if len(l.Legend) > 0 {
		fmt.Fprintf(&b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s"/>`+"\n",
			num(Margin), num(l.LegendTop), num(l.W-Margin), num(l.LegendTop), legendRule)
	}
	for _, it := range l.Legend {
		cy := it.Y + 6
		if it.Box {
			stroke := ""
			if it.Width > 0 {
				stroke = fmt.Sprintf(` stroke="%s" stroke-width="%s"%s`, it.Color, num(it.Width), dashAttr(it.Dash))
			}
			fmt.Fprintf(&b, `<rect x="%s" y="%s" width="22" height="12" rx="3" fill="%s"%s/>`+"\n", num(it.X), num(cy-6), it.Fill, stroke)
		} else {
			fmt.Fprintf(&b, `<line x1="%s" y1="%s" x2="%s" y2="%s" stroke="%s" stroke-width="%s"%s/>`+"\n",
				num(it.X), num(cy), num(it.X+22), num(cy), it.Color, num(it.Width), dashAttr(it.Dash))
		}
		fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="%s" fill="%s">%s</text>`+"\n",
			num(it.X+28), num(cy+4), num(legendSize), inkColor, esc(it.Label))
	}
	if l.Footer != "" {
		fmt.Fprintf(&b, `<text x="%s" y="%s" font-size="%s" fill="%s">%s</text>`+"\n",
			num(Margin), num(l.FooterY+10), num(legendSize), inkColor, esc(l.Footer))
	}
	b.WriteString("</svg>\n")
	return b.Bytes()
}
