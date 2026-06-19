// Package board reconstructs the sequence of board positions from a parsed PGN
// game. The pgn package decodes each move's destination and annotations from
// SAN but not the originating square; this package replays the moves on a real
// 8x8 board, resolving each move's source square (including disambiguation,
// pins, castling, en passant and promotion) so the game can be animated.
package board

import (
	"fmt"

	"chess-to-music/internal/pgn"
)

// Square is a board coordinate with File 0='a'..7='h' and Rank 0='1'..7='8'.
type Square struct {
	File int
	Rank int
}

// Piece is the occupant of a square. Kind is 0 when the square is empty.
type Piece struct {
	Kind  pgn.Piece
	Color pgn.Color
}

// Empty reports whether the square has no piece.
func (p Piece) Empty() bool { return p.Kind == 0 }

// Position is a full snapshot of the board: cells[rank][file].
type Position [8][8]Piece

// Ply is a single resolved half-move with everything needed to animate it.
type Ply struct {
	SAN       string
	Color     pgn.Color
	Piece     pgn.Piece
	From      To
	Capture   bool
	CaptureAt Square // square the captured piece sat on (en passant differs from To)
	HasCastle bool
	RookFrom  Square
	RookTo    Square
	Promotion pgn.Piece
}

// To embeds the from/to squares of the moving piece.
type To struct {
	From Square
	To   Square
}

// Board is a mutable chess position used to replay a game.
type Board struct {
	cells  Position
	epFile int // en passant target file, -1 when none
	epRank int // en passant target rank, -1 when none
}

// New returns a board in the standard starting position.
func New() *Board {
	b := &Board{epFile: -1, epRank: -1}
	back := []pgn.Piece{pgn.Rook, pgn.Knight, pgn.Bishop, pgn.Queen, pgn.King, pgn.Bishop, pgn.Knight, pgn.Rook}
	for f := 0; f < 8; f++ {
		b.cells[0][f] = Piece{Kind: back[f], Color: pgn.White}
		b.cells[1][f] = Piece{Kind: pgn.Pawn, Color: pgn.White}
		b.cells[6][f] = Piece{Kind: pgn.Pawn, Color: pgn.Black}
		b.cells[7][f] = Piece{Kind: back[f], Color: pgn.Black}
	}
	return b
}

// Position returns a copy of the current board snapshot.
func (b *Board) Position() Position { return b.cells }

func (b *Board) at(s Square) Piece { return b.cells[s.Rank][s.File] }

func inBounds(f, r int) bool { return f >= 0 && f < 8 && r >= 0 && r < 8 }

// Replay reconstructs every position of the game. It returns the initial
// position followed by the position after each move, plus the resolved plies.
func Replay(g *pgn.Game) (positions []Position, plies []Ply, err error) {
	b := New()
	positions = append(positions, b.Position())
	for i, mv := range g.Moves {
		ply, e := b.apply(mv)
		if e != nil {
			return positions, plies, fmt.Errorf("move %d (%s): %w", i+1, mv.SAN, e)
		}
		plies = append(plies, ply)
		positions = append(positions, b.Position())
	}
	return positions, plies, nil
}

// apply resolves and performs a single move, mutating the board.
func (b *Board) apply(mv pgn.Move) (Ply, error) {
	if mv.Castle != pgn.NoCastle {
		return b.applyCastle(mv)
	}

	dst := Square{File: mv.File, Rank: mv.Rank}
	from, err := b.findSource(mv, dst)
	if err != nil {
		return Ply{}, err
	}

	ply := Ply{
		SAN:       mv.SAN,
		Color:     mv.Color,
		Piece:     mv.Piece,
		From:      To{From: from, To: dst},
		Promotion: mv.Promotion,
	}

	// Determine capture (including en passant where the target square is empty).
	captureAt := dst
	if mv.Piece == pgn.Pawn && from.File != dst.File && b.at(dst).Empty() {
		// En passant: captured pawn sits beside the destination, on the
		// moving pawn's starting rank.
		captureAt = Square{File: dst.File, Rank: from.Rank}
	}
	if !b.at(captureAt).Empty() && b.at(captureAt).Color != mv.Color {
		ply.Capture = true
		ply.CaptureAt = captureAt
	}

	// Perform the move.
	moving := b.at(from)
	if mv.Promotion != 0 {
		moving.Kind = mv.Promotion
	}
	b.cells[from.Rank][from.File] = Piece{}
	if ply.Capture {
		b.cells[captureAt.Rank][captureAt.File] = Piece{}
	}
	b.cells[dst.Rank][dst.File] = moving

	// Track en passant target for the next move.
	b.epFile, b.epRank = -1, -1
	if mv.Piece == pgn.Pawn && abs(dst.Rank-from.Rank) == 2 {
		b.epFile = dst.File
		b.epRank = (dst.Rank + from.Rank) / 2
	}
	return ply, nil
}

// applyCastle moves the king and rook for a castling move.
func (b *Board) applyCastle(mv pgn.Move) (Ply, error) {
	rank := 0
	if mv.Color == pgn.Black {
		rank = 7
	}
	var kingFrom, kingTo, rookFrom, rookTo Square
	kingFrom = Square{File: 4, Rank: rank}
	if mv.Castle == pgn.KingSide {
		kingTo = Square{File: 6, Rank: rank}
		rookFrom = Square{File: 7, Rank: rank}
		rookTo = Square{File: 5, Rank: rank}
	} else {
		kingTo = Square{File: 2, Rank: rank}
		rookFrom = Square{File: 0, Rank: rank}
		rookTo = Square{File: 3, Rank: rank}
	}

	king := b.at(kingFrom)
	rook := b.at(rookFrom)
	b.cells[kingFrom.Rank][kingFrom.File] = Piece{}
	b.cells[rookFrom.Rank][rookFrom.File] = Piece{}
	b.cells[kingTo.Rank][kingTo.File] = king
	b.cells[rookTo.Rank][rookTo.File] = rook
	b.epFile, b.epRank = -1, -1

	return Ply{
		SAN:       mv.SAN,
		Color:     mv.Color,
		Piece:     pgn.King,
		From:      To{From: kingFrom, To: kingTo},
		HasCastle: true,
		RookFrom:  rookFrom,
		RookTo:    rookTo,
	}, nil
}

// findSource locates the square the moving piece came from.
func (b *Board) findSource(mv pgn.Move, dst Square) (Square, error) {
	var candidates []Square
	for r := 0; r < 8; r++ {
		for f := 0; f < 8; f++ {
			p := b.cells[r][f]
			if p.Empty() || p.Color != mv.Color || p.Kind != mv.Piece {
				continue
			}
			from := Square{File: f, Rank: r}
			if mv.FromFile >= 0 && from.File != mv.FromFile {
				continue
			}
			if mv.FromRank >= 0 && from.Rank != mv.FromRank {
				continue
			}
			if b.canReach(mv, from, dst) {
				candidates = append(candidates, from)
			}
		}
	}

	switch len(candidates) {
	case 0:
		return Square{}, fmt.Errorf("no %s found that can move to %c%d", pieceName(mv.Piece), 'a'+dst.File, dst.Rank+1)
	case 1:
		return candidates[0], nil
	}

	// Several pseudo-legal candidates: keep only those that don't leave the
	// mover's king in check (resolves pinned-piece ambiguity).
	var legal []Square
	for _, from := range candidates {
		if b.legalAfter(mv, from, dst) {
			legal = append(legal, from)
		}
	}
	if len(legal) == 1 {
		return legal[0], nil
	}
	if len(legal) == 0 {
		// Fall back to the first candidate rather than failing the whole game.
		return candidates[0], nil
	}
	return legal[0], nil
}

// canReach reports whether a piece on `from` can move to `dst` on the current
// board, respecting movement patterns and blocking (but not king safety).
func (b *Board) canReach(mv pgn.Move, from, dst Square) bool {
	df := dst.File - from.File
	dr := dst.Rank - from.Rank
	switch mv.Piece {
	case pgn.Pawn:
		dir := 1
		startRank := 1
		if mv.Color == pgn.Black {
			dir = -1
			startRank = 6
		}
		if mv.Capture {
			// Diagonal capture (covers en passant onto an empty square).
			return abs(df) == 1 && dr == dir
		}
		// Straight push: must stay on the same file.
		if df != 0 {
			return false
		}
		if dr == dir && b.at(dst).Empty() {
			return true
		}
		if dr == 2*dir && from.Rank == startRank && b.at(dst).Empty() {
			mid := Square{File: from.File, Rank: from.Rank + dir}
			return b.at(mid).Empty()
		}
		return false
	case pgn.Knight:
		return (abs(df) == 1 && abs(dr) == 2) || (abs(df) == 2 && abs(dr) == 1)
	case pgn.Bishop:
		return abs(df) == abs(dr) && df != 0 && b.clearPath(from, dst)
	case pgn.Rook:
		return (df == 0 || dr == 0) && (df != 0 || dr != 0) && b.clearPath(from, dst)
	case pgn.Queen:
		straight := df == 0 || dr == 0
		diagonal := abs(df) == abs(dr)
		return (straight || diagonal) && (df != 0 || dr != 0) && b.clearPath(from, dst)
	case pgn.King:
		return abs(df) <= 1 && abs(dr) <= 1 && (df != 0 || dr != 0)
	}
	return false
}

// clearPath reports whether all squares strictly between from and dst are empty.
func (b *Board) clearPath(from, dst Square) bool {
	df := sign(dst.File - from.File)
	dr := sign(dst.Rank - from.Rank)
	f, r := from.File+df, from.Rank+dr
	for f != dst.File || r != dst.Rank {
		if !inBounds(f, r) {
			return false
		}
		if !b.cells[r][f].Empty() {
			return false
		}
		f += df
		r += dr
	}
	return true
}

// legalAfter checks that performing the move does not leave the mover's king
// attacked (used only to disambiguate between pseudo-legal candidates).
func (b *Board) legalAfter(mv pgn.Move, from, dst Square) bool {
	clone := *b
	moving := clone.at(from)
	if mv.Promotion != 0 {
		moving.Kind = mv.Promotion
	}
	captureAt := dst
	if mv.Piece == pgn.Pawn && from.File != dst.File && clone.at(dst).Empty() {
		captureAt = Square{File: dst.File, Rank: from.Rank}
	}
	clone.cells[from.Rank][from.File] = Piece{}
	clone.cells[captureAt.Rank][captureAt.File] = Piece{}
	clone.cells[dst.Rank][dst.File] = moving
	king := clone.findKing(mv.Color)
	if king.File < 0 {
		return true
	}
	return !clone.attacked(king, opposite(mv.Color))
}

// findKing returns the square of the given colour's king, or {-1,-1}.
func (b *Board) findKing(c pgn.Color) Square {
	for r := 0; r < 8; r++ {
		for f := 0; f < 8; f++ {
			p := b.cells[r][f]
			if p.Kind == pgn.King && p.Color == c {
				return Square{File: f, Rank: r}
			}
		}
	}
	return Square{File: -1, Rank: -1}
}

// attacked reports whether square s is attacked by any piece of colour by.
func (b *Board) attacked(s Square, by pgn.Color) bool {
	// Pawn attacks.
	dir := 1
	if by == pgn.Black {
		dir = -1
	}
	for _, df := range []int{-1, 1} {
		f, r := s.File+df, s.Rank-dir
		if inBounds(f, r) {
			p := b.cells[r][f]
			if p.Kind == pgn.Pawn && p.Color == by {
				return true
			}
		}
	}
	// Knight attacks.
	knightOffsets := [8][2]int{{1, 2}, {2, 1}, {-1, 2}, {-2, 1}, {1, -2}, {2, -1}, {-1, -2}, {-2, -1}}
	for _, o := range knightOffsets {
		f, r := s.File+o[0], s.Rank+o[1]
		if inBounds(f, r) {
			p := b.cells[r][f]
			if p.Kind == pgn.Knight && p.Color == by {
				return true
			}
		}
	}
	// King adjacency.
	for dr := -1; dr <= 1; dr++ {
		for df := -1; df <= 1; df++ {
			if df == 0 && dr == 0 {
				continue
			}
			f, r := s.File+df, s.Rank+dr
			if inBounds(f, r) {
				p := b.cells[r][f]
				if p.Kind == pgn.King && p.Color == by {
					return true
				}
			}
		}
	}
	// Sliding attacks: rook/queen orthogonally, bishop/queen diagonally.
	orth := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	diag := [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	if b.raySlider(s, by, orth, pgn.Rook) {
		return true
	}
	if b.raySlider(s, by, diag, pgn.Bishop) {
		return true
	}
	return false
}

// raySlider walks each direction looking for an attacking slider (the given
// straight/diagonal piece, or a queen) of colour by.
func (b *Board) raySlider(s Square, by pgn.Color, dirs [4][2]int, kind pgn.Piece) bool {
	for _, d := range dirs {
		f, r := s.File+d[0], s.Rank+d[1]
		for inBounds(f, r) {
			p := b.cells[r][f]
			if !p.Empty() {
				if p.Color == by && (p.Kind == kind || p.Kind == pgn.Queen) {
					return true
				}
				break
			}
			f += d[0]
			r += d[1]
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func sign(x int) int {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	default:
		return 0
	}
}

func opposite(c pgn.Color) pgn.Color {
	if c == pgn.White {
		return pgn.Black
	}
	return pgn.White
}

func pieceName(p pgn.Piece) string {
	switch p {
	case pgn.Pawn:
		return "pawn"
	case pgn.Knight:
		return "knight"
	case pgn.Bishop:
		return "bishop"
	case pgn.Rook:
		return "rook"
	case pgn.Queen:
		return "queen"
	case pgn.King:
		return "king"
	}
	return "piece"
}
