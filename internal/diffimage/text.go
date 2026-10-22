package diffimage

import (
	"fmt"
	"strconv"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var (
	parsedOnce    sync.Once
	regular, bold *opentype.Font
)

func fonts() (*opentype.Font, *opentype.Font) {
	parsedOnce.Do(func() {
		var err error
		if regular, err = opentype.Parse(goregular.TTF); err != nil {
			panic(err)
		}
		if bold, err = opentype.Parse(gobold.TTF); err != nil {
			panic(err)
		}
	})
	return regular, bold
}

type faceKey struct {
	bold bool
	size float64
}

var (
	faceMu sync.Mutex
	faces  = map[faceKey]font.Face{}
)

// face returns the Go font face at the given pixel size (72 dpi, no hinting,
// so widths scale linearly with the size).
func face(boldFace bool, size float64) font.Face {
	faceMu.Lock()
	defer faceMu.Unlock()
	k := faceKey{boldFace, size}
	if f, ok := faces[k]; ok {
		return f
	}
	r, b := fonts()
	src := r
	if boldFace {
		src = b
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		panic(err)
	}
	faces[k] = f
	return f
}

// measure returns the width of s in logical units at the given size.
func measure(boldFace bool, size float64, s string) float64 {
	// Measure at 8x for sub-pixel accuracy, then scale back.
	const k = 8
	d := font.Drawer{Face: face(boldFace, size*k)}
	return float64(d.MeasureString(s)) / 64 / k
}

func fx(f float64) fixed.Int26_6 { return fixed.Int26_6(f * 64) }

func hexRGB(h string) (r, g, b uint8) {
	v, err := strconv.ParseUint(h[1:], 16, 32)
	if err != nil || len(h) != 7 {
		panic(fmt.Sprintf("diffimage: bad color %q", h))
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

func rgbHex(r, g, b uint8) string { return fmt.Sprintf("#%02x%02x%02x", r, g, b) }
