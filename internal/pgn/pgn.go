// Package pgn parses chess games written in Portable Game Notation (PGN),
// the standard export format used by chess.com, Lichess and most chess software.
//
// The parser is intentionally lightweight: it extracts the information that the
// music mapping needs (which piece moved, to which square, and any annotations
// such as captures, checks, mates, castling and promotions) directly from the
// Standard Algebraic Notation (SAN) of each move. It does not attempt to verify
// move legality, so it works on any well-formed PGN without a full chess engine.
package pgn

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// Color identifies which player made a move.
type Color int

const (
	White Color = iota
	Black
)

func (c Color) String() string {
	if c == White {
		return "White"
	}
	return "Black"
}

// Piece is the moving piece, using the standard English SAN letters.
type Piece byte

const (
	Pawn   Piece = 'P'
	Knight Piece = 'N'
	Bishop Piece = 'B'
	Rook   Piece = 'R'
	Queen  Piece = 'Q'
	King   Piece = 'K'
)

// Castle describes whether a move is a castling move and, if so, which side.
type Castle int

const (
	NoCastle Castle = iota
	KingSide
	QueenSide
)

// Move is a single half-move (ply) decoded from SAN.
type Move struct {
	SAN       string // the original SAN token, e.g. "Qxc6+"
	Color     Color
	Piece     Piece
	File      int // destination file, 0 = 'a' .. 7 = 'h' (-1 when not applicable)
	Rank      int // destination rank, 0 = '1' .. 7 = '8' (-1 when not applicable)
	Capture   bool
	Check     bool
	Mate      bool
	Castle    Castle
	Promotion Piece // 0 when the move is not a promotion
}

// Game is a parsed PGN game: its header tags plus the sequence of moves.
type Game struct {
	Tags  map[string]string
	Moves []Move
}

// Title returns a human-friendly title for the game derived from its tags,
// falling back to a generic label when the relevant tags are missing.
func (g *Game) Title() string {
	white := g.Tags["White"]
	black := g.Tags["Black"]
	if white != "" || black != "" {
		if white == "" {
			white = "?"
		}
		if black == "" {
			black = "?"
		}
		return fmt.Sprintf("%s vs %s", white, black)
	}
	if ev := g.Tags["Event"]; ev != "" {
		return ev
	}
	return "Chess Game"
}

var tagLine = regexp.MustCompile(`^\s*\[\s*(\w+)\s+"(.*)"\s*\]\s*$`)

// sanToken matches a single SAN move (without surrounding annotations such as
// move numbers, NAGs or comments, which are stripped before tokenisation).
var sanToken = regexp.MustCompile(
	`^(O-O-O|O-O|0-0-0|0-0)([+#]?)$|` + // castling
		`^([KQRBN]?)([a-h]?)([1-8]?)(x?)([a-h])([1-8])(=[QRBN])?([+#]?)$`)

// ParseFirst reads PGN from r and returns the first game it contains.
func ParseFirst(r io.Reader) (*Game, error) {
	games, err := ParseAll(r)
	if err != nil {
		return nil, err
	}
	if len(games) == 0 {
		return nil, fmt.Errorf("no games found in input")
	}
	return games[0], nil
}

// ParseAll reads PGN from r and returns every game it contains.
func ParseAll(r io.Reader) ([]*Game, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var games []*Game
	var tags map[string]string
	var movetext strings.Builder
	inMoves := false

	flush := func() error {
		if tags == nil && movetext.Len() == 0 {
			return nil
		}
		moves, err := parseMovetext(movetext.String())
		if err != nil {
			return err
		}
		if tags == nil {
			tags = map[string]string{}
		}
		games = append(games, &Game{Tags: tags, Moves: moves})
		tags = nil
		movetext.Reset()
		inMoves = false
		return nil
	}

	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		if m := tagLine.FindStringSubmatch(line); m != nil {
			// A new tag section after movetext signals a new game.
			if inMoves {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			if tags == nil {
				tags = map[string]string{}
			}
			tags[m[1]] = m[2]
			continue
		}

		if trimmed == "" {
			continue
		}

		inMoves = true
		movetext.WriteByte(' ')
		movetext.WriteString(trimmed)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return games, nil
}

var (
	commentBrace = regexp.MustCompile(`\{[^}]*\}`)
	nag          = regexp.MustCompile(`\$\d+`)
	moveNumber   = regexp.MustCompile(`\d+\.(\.\.)?`)
)

// parseMovetext converts a PGN movetext section into a slice of decoded moves.
func parseMovetext(text string) ([]Move, error) {
	// Remove brace comments first.
	text = commentBrace.ReplaceAllString(text, " ")
	// Remove line comments (semicolon to end of line already split out).
	text = strings.ReplaceAll(text, ";", " ")
	// Remove recursive annotation variations (parentheses), handling nesting.
	text = stripVariations(text)
	// Remove NAGs and move numbers.
	text = nag.ReplaceAllString(text, " ")
	text = moveNumber.ReplaceAllString(text, " ")

	fields := strings.Fields(text)
	var moves []Move
	color := White
	for _, f := range fields {
		switch f {
		case "1-0", "0-1", "1/2-1/2", "*":
			continue // game result terminators
		}
		// Strip trailing annotation glyphs like ! and ? (but keep + and #).
		f = strings.TrimRight(f, "!?")
		if f == "" {
			continue
		}
		mv, ok := parseSAN(f, color)
		if !ok {
			// Skip anything we cannot interpret rather than failing the
			// whole game; this keeps parsing resilient to exotic notation.
			continue
		}
		moves = append(moves, mv)
		if color == White {
			color = Black
		} else {
			color = White
		}
	}
	return moves, nil
}

// stripVariations removes balanced parenthesised sub-variations.
func stripVariations(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// parseSAN decodes a single SAN token into a Move.
func parseSAN(tok string, color Color) (Move, bool) {
	m := sanToken.FindStringSubmatch(tok)
	if m == nil {
		return Move{}, false
	}

	mv := Move{SAN: tok, Color: color, File: -1, Rank: -1}

	// Castling branch.
	if m[1] != "" {
		switch m[1] {
		case "O-O", "0-0":
			mv.Castle = KingSide
		case "O-O-O", "0-0-0":
			mv.Castle = QueenSide
		}
		mv.Piece = King
		applyCheck(&mv, m[2])
		return mv, true
	}

	// Normal move branch.
	pieceStr := m[3]
	if pieceStr == "" {
		mv.Piece = Pawn
	} else {
		mv.Piece = Piece(pieceStr[0])
	}
	mv.Capture = m[6] == "x"
	mv.File = int(m[7][0] - 'a')
	mv.Rank = int(m[8][0] - '1')
	if promo := m[9]; promo != "" {
		mv.Promotion = Piece(promo[1])
	}
	applyCheck(&mv, m[10])
	return mv, true
}

func applyCheck(mv *Move, suffix string) {
	switch suffix {
	case "+":
		mv.Check = true
	case "#":
		mv.Mate = true
	}
}
