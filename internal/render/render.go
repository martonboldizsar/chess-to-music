// Package render draws chess positions and animation frames as raster images.
// Pieces are drawn from the Unicode chess glyphs of an embedded font, and two
// board themes are provided that mimic the look of the Lichess and Chess.com
// boards.
package render

import (
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"chess-to-music/internal/board"
	"chess-to-music/internal/pgn"
)

//go:embed assets/DejaVuSans.ttf
var fontFS embed.FS

// Theme describes the colours used to draw a board and its pieces.
type Theme struct {
	Name         string
	Label        string
	Light        color.RGBA // light square
	Dark         color.RGBA // dark square
	Highlight    color.RGBA // last-move square tint
	WhiteFill    color.RGBA
	WhiteOutline color.RGBA
	BlackFill    color.RGBA
	BlackOutline color.RGBA
	Coord        color.RGBA // coordinate labels
}

var themes = map[string]Theme{
	"lichess": {
		Name:         "lichess",
		Label:        "Lichess",
		Light:        color.RGBA{0xf0, 0xd9, 0xb5, 0xff},
		Dark:         color.RGBA{0xb5, 0x88, 0x63, 0xff},
		Highlight:    color.RGBA{0xcd, 0xd2, 0x6a, 0xa0},
		WhiteFill:    color.RGBA{0xf7, 0xf7, 0xf2, 0xff},
		WhiteOutline: color.RGBA{0x33, 0x2a, 0x22, 0xff},
		BlackFill:    color.RGBA{0x2b, 0x27, 0x22, 0xff},
		BlackOutline: color.RGBA{0x0a, 0x08, 0x06, 0xff},
		Coord:        color.RGBA{0x00, 0x00, 0x00, 0x66},
	},
	"chesscom": {
		Name:         "chesscom",
		Label:        "Chess.com",
		Light:        color.RGBA{0xee, 0xee, 0xd2, 0xff},
		Dark:         color.RGBA{0x76, 0x96, 0x56, 0xff},
		Highlight:    color.RGBA{0xf6, 0xf6, 0x69, 0x9c},
		WhiteFill:    color.RGBA{0xfa, 0xfa, 0xfa, 0xff},
		WhiteOutline: color.RGBA{0x40, 0x40, 0x40, 0xff},
		BlackFill:    color.RGBA{0x33, 0x33, 0x33, 0xff},
		BlackOutline: color.RGBA{0x08, 0x08, 0x08, 0xff},
		Coord:        color.RGBA{0x00, 0x00, 0x00, 0x55},
	},
}

// ThemeNames returns the available theme identifiers.
func ThemeNames() []string { return []string{"lichess", "chesscom"} }

// ThemeByName resolves a theme identifier, reporting whether it was found.
func ThemeByName(name string) (Theme, bool) {
	t, ok := themes[name]
	return t, ok
}

// glyphFor returns the filled Unicode chess glyph for a piece kind; the same
// glyph is recoloured for white and black pieces.
func glyphFor(p pgn.Piece) rune {
	switch p {
	case pgn.King:
		return '\u265A'
	case pgn.Queen:
		return '\u265B'
	case pgn.Rook:
		return '\u265C'
	case pgn.Bishop:
		return '\u265D'
	case pgn.Knight:
		return '\u265E'
	case pgn.Pawn:
		return '\u265F'
	}
	return '?'
}

// Mover is a piece drawn at a fractional board coordinate during an animation.
type Mover struct {
	Kind  pgn.Piece
	Color pgn.Color
	File  float64 // 0='a' .. 7='h'
	Rank  float64 // 0='1' .. 7='8'
}

// Renderer draws frames of a fixed pixel size using a cached font face.
type Renderer struct {
	theme     Theme
	size      int // board side length in pixels (multiple of 8)
	sq        int // square side length
	pieceFace font.Face
	coordFace font.Face
}

// NewRenderer builds a renderer for the given theme and pixel size. The size is
// rounded down to a multiple of eight so squares are integral.
func NewRenderer(theme Theme, size int) (*Renderer, error) {
	sq := size / 8
	if sq < 8 {
		sq = 8
	}
	size = sq * 8

	ttf, err := fontFS.ReadFile("assets/DejaVuSans.ttf")
	if err != nil {
		return nil, fmt.Errorf("loading font: %w", err)
	}
	fnt, err := opentype.Parse(ttf)
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}
	pieceFace, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size: float64(sq) * 0.86, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("piece face: %w", err)
	}
	coordFace, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size: float64(sq) * 0.2, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("coord face: %w", err)
	}
	return &Renderer{theme: theme, size: size, sq: sq, pieceFace: pieceFace, coordFace: coordFace}, nil
}

// Size returns the rendered board's pixel side length.
func (r *Renderer) Size() int { return r.size }

// Position renders a static board snapshot, optionally tinting highlight
// squares (e.g. the last move's from/to squares).
func (r *Renderer) Position(pos board.Position, highlight ...board.Square) *image.RGBA {
	img := r.drawBoard(highlight)
	for rank := 0; rank < 8; rank++ {
		for file := 0; file < 8; file++ {
			p := pos[rank][file]
			if p.Empty() {
				continue
			}
			r.drawPiece(img, p.Kind, p.Color, float64(file), float64(rank))
		}
	}
	return img
}

// Frame renders an animation frame: the static base position (with the moving
// pieces removed) plus the movers drawn at their fractional coordinates.
func (r *Renderer) Frame(base board.Position, highlight []board.Square, movers []Mover) *image.RGBA {
	img := r.drawBoard(highlight)
	for rank := 0; rank < 8; rank++ {
		for file := 0; file < 8; file++ {
			p := base[rank][file]
			if p.Empty() {
				continue
			}
			r.drawPiece(img, p.Kind, p.Color, float64(file), float64(rank))
		}
	}
	for _, m := range movers {
		r.drawPiece(img, m.Kind, m.Color, m.File, m.Rank)
	}
	return img
}

// drawBoard paints the 64 squares, last-move highlights and coordinate labels.
func (r *Renderer) drawBoard(highlight []board.Square) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, r.size, r.size))
	for rank := 0; rank < 8; rank++ {
		for file := 0; file < 8; file++ {
			col := r.theme.Light
			if (file+rank)%2 == 0 {
				col = r.theme.Dark // a1 (file 0, rank 0) is dark
			}
			x0, y0 := r.px(float64(file), float64(rank))
			rect := image.Rect(x0, y0, x0+r.sq, y0+r.sq)
			draw.Draw(img, rect, &image.Uniform{col}, image.Point{}, draw.Src)
		}
	}
	for _, s := range highlight {
		if s.File < 0 || s.Rank < 0 {
			continue
		}
		x0, y0 := r.px(float64(s.File), float64(s.Rank))
		rect := image.Rect(x0, y0, x0+r.sq, y0+r.sq)
		draw.Draw(img, rect, &image.Uniform{premultiply(r.theme.Highlight)}, image.Point{}, draw.Over)
	}
	r.drawCoordinates(img)
	return img
}

// drawCoordinates labels files along the bottom rank and ranks up the left edge.
func (r *Renderer) drawCoordinates(img *image.RGBA) {
	pad := r.sq / 12
	if pad < 2 {
		pad = 2
	}
	src := &image.Uniform{r.theme.Coord}
	for file := 0; file < 8; file++ {
		x0, _ := r.px(float64(file), 0)
		d := &font.Drawer{Dst: img, Src: src, Face: r.coordFace}
		label := string(rune('a' + file))
		w := d.MeasureString(label)
		d.Dot = fixed.Point26_6{
			X: fixed.I(x0+r.sq-pad) - w,
			Y: fixed.I(r.size - pad),
		}
		d.DrawString(label)
	}
	for rank := 0; rank < 8; rank++ {
		_, y0 := r.px(0, float64(rank))
		d := &font.Drawer{Dst: img, Src: src, Face: r.coordFace}
		m := r.coordFace.Metrics()
		d.Dot = fixed.Point26_6{
			X: fixed.I(pad),
			Y: fixed.I(y0+pad) + m.Ascent,
		}
		d.DrawString(string(rune('1' + rank)))
	}
}

// drawPiece draws a single piece (with an outline) centred on the given
// fractional board coordinate.
func (r *Renderer) drawPiece(img *image.RGBA, kind pgn.Piece, c pgn.Color, file, rank float64) {
	glyph := string(glyphFor(kind))
	fill, outline := r.theme.WhiteFill, r.theme.WhiteOutline
	if c == pgn.Black {
		fill, outline = r.theme.BlackFill, r.theme.BlackOutline
	}

	x0, y0 := r.px(file, rank)
	cx := float64(x0) + float64(r.sq)/2
	cy := float64(y0) + float64(r.sq)/2

	bounds, _ := font.BoundString(r.pieceFace, glyph)
	gw := bounds.Max.X - bounds.Min.X
	gh := bounds.Max.Y - bounds.Min.Y
	dotX := fixed.Int26_6(cx*64) - bounds.Min.X - gw/2
	dotY := fixed.Int26_6(cy*64) - bounds.Min.Y - gh/2

	// Outline: draw the glyph offset in the four cardinal directions. Using
	// four passes (instead of eight) roughly halves the per-piece draw cost
	// while still giving the glyph a clear edge against the board.
	off := r.sq / 28
	if off < 1 {
		off = 1
	}
	ofx := fixed.I(off)
	outlineSrc := &image.Uniform{outline}
	for _, d := range [][2]fixed.Int26_6{
		{-ofx, 0}, {ofx, 0}, {0, -ofx}, {0, ofx},
	} {
		od := &font.Drawer{Dst: img, Src: outlineSrc, Face: r.pieceFace}
		od.Dot = fixed.Point26_6{X: dotX + d[0], Y: dotY + d[1]}
		od.DrawString(glyph)
	}

	fd := &font.Drawer{Dst: img, Src: &image.Uniform{fill}, Face: r.pieceFace}
	fd.Dot = fixed.Point26_6{X: dotX, Y: dotY}
	fd.DrawString(glyph)
}

// px converts a fractional board coordinate to top-left pixel coordinates,
// orienting the board with White at the bottom (rank 0 at the bottom edge).
func (r *Renderer) px(file, rank float64) (int, int) {
	x := int(file * float64(r.sq))
	y := int((7 - rank) * float64(r.sq))
	return x, y
}

// premultiply converts a straight-alpha RGBA into the alpha-premultiplied form
// expected by Go's image/draw, so translucent highlights blend correctly.
func premultiply(c color.RGBA) color.RGBA {
	a := uint32(c.A)
	return color.RGBA{
		R: uint8(uint32(c.R) * a / 255),
		G: uint8(uint32(c.G) * a / 255),
		B: uint8(uint32(c.B) * a / 255),
		A: c.A,
	}
}
