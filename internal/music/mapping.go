// Package music turns parsed chess moves into musical material and writes it
// out in standard notation formats (ABC notation and Standard MIDI Files).
package music

import "chess-to-music/internal/pgn"

// Instrument is the timbre/voice a note is played with. Each chess piece maps
// to a distinct instrument so you can hear which kind of piece moved.
type Instrument int

const (
	InstPiano  Instrument = iota // pawns
	InstHorn                     // knights
	InstOrgan                    // bishops
	InstTuba                     // rooks
	InstViolin                   // queens
	InstChoir                    // kings
	InstrumentCount
)

// instrumentNames are the stable identifiers used by the CLI/API and the web
// UI to refer to instruments. The order matches the Instrument constants.
var instrumentNames = [InstrumentCount]string{
	InstPiano:  "piano",
	InstHorn:   "horn",
	InstOrgan:  "organ",
	InstTuba:   "tuba",
	InstViolin: "violin",
	InstChoir:  "choir",
}

// String returns the stable identifier for an instrument.
func (i Instrument) String() string {
	if i < 0 || i >= InstrumentCount {
		return "unknown"
	}
	return instrumentNames[i]
}

// InstrumentNames returns the list of available instrument identifiers in
// canonical order, for populating UI dropdowns and validating input.
func InstrumentNames() []string {
	names := make([]string, InstrumentCount)
	copy(names, instrumentNames[:])
	return names
}

// ParseInstrument resolves an instrument identifier (e.g. "violin") to its
// Instrument value, reporting whether it was recognised.
func ParseInstrument(name string) (Instrument, bool) {
	for i, n := range instrumentNames {
		if n == name {
			return Instrument(i), true
		}
	}
	return 0, false
}

// Effect is a bitmask of special-move flourishes layered on top of a note.
type Effect uint8

const (
	EffectCapture Effect = 1 << iota // a piece was taken: percussive hit
	EffectCheck                      // check: bright bell/triangle ping
	EffectMate                       // checkmate: big cymbal crash
	EffectCastle                     // castling: shaker swell under the triad
)

// Note is one musical event produced from a chess move. A note normally has a
// single pitch, but castling produces a chord (multiple simultaneous pitches).
type Note struct {
	Pitches    []int // MIDI note numbers (60 = middle C)
	Duration   int   // length in eighth-note units (the ABC/MIDI unit length)
	Velocity   int   // MIDI velocity 1..127 (loudness/accent)
	Color      pgn.Color
	Instrument Instrument // which voice plays this note (derived from the piece)
	Effects    Effect     // special-move flourishes to layer on top
	SAN        string     // source move, kept for ABC comments
}

// Config controls how chess moves are mapped to pitches and rhythm.
type Config struct {
	// BaseOctave is the octave of the lowest mapped square for White
	// (scientific pitch notation, where octave 4 contains middle C).
	BaseOctave int
	// Tempo is the playback tempo in quarter-note beats per minute.
	Tempo int
	// Instruments optionally overrides which instrument each piece plays. Any
	// piece missing from the map falls back to the default assignment.
	Instruments map[pgn.Piece]Instrument
}

// DefaultConfig returns a musically sensible default mapping.
func DefaultConfig() Config {
	return Config{BaseOctave: 4, Tempo: 120}
}

// instrumentFor returns the instrument a piece should play under cfg, honouring
// any per-piece override and otherwise using the default assignment.
func (cfg Config) instrumentFor(p pgn.Piece) Instrument {
	if cfg.Instruments != nil {
		if inst, ok := cfg.Instruments[p]; ok {
			return inst
		}
	}
	return instrumentByPiece[p]
}

// InstrumentForPiece is the exported form of instrumentFor, used by callers
// (such as the web API) to report the instrument assigned to a piece.
func (cfg Config) InstrumentForPiece(p pgn.Piece) Instrument {
	return cfg.instrumentFor(p)
}

// Major scale semitone offsets for the eight files a..h, so each file of the
// board maps to a pleasant diatonic step (C D E F G A B C in the home octave).
var fileScale = [8]int{0, 2, 4, 5, 7, 9, 11, 12}

// durationByPiece gives each piece a characteristic note length (in eighths),
// so heavier pieces ring out longer than pawns.
var durationByPiece = map[pgn.Piece]int{
	pgn.Pawn:   1, // eighth note
	pgn.Knight: 2, // quarter note
	pgn.Bishop: 2, // quarter note
	pgn.Rook:   3, // dotted quarter
	pgn.Queen:  4, // half note
	pgn.King:   2, // quarter note
}

// instrumentByPiece gives each kind of piece its own voice.
var instrumentByPiece = map[pgn.Piece]Instrument{
	pgn.Pawn:   InstPiano,  // foot soldiers: plain piano
	pgn.Knight: InstHorn,   // cavalry: a horn
	pgn.Bishop: InstOrgan,  // the church: an organ
	pgn.Rook:   InstTuba,   // the heavy tower: a tuba
	pgn.Queen:  InstViolin, // the lead voice: a violin
	pgn.King:   InstChoir,  // the monarch: a choir
}

// Score is the full musical rendering of a game.
type Score struct {
	Title  string
	Tempo  int
	Notes  []Note
	Config Config
}

// Build converts a parsed game into a Score using the supplied configuration.
func Build(game *pgn.Game, cfg Config) Score {
	s := Score{Title: game.Title(), Tempo: cfg.Tempo, Config: cfg}
	for _, mv := range game.Moves {
		s.Notes = append(s.Notes, noteFor(mv, cfg))
	}
	return s
}

// noteFor maps a single move to a Note.
func noteFor(mv pgn.Move, cfg Config) Note {
	n := Note{
		Color:      mv.Color,
		SAN:        mv.SAN,
		Velocity:   velocityFor(mv),
		Instrument: cfg.instrumentFor(mv.Piece),
		Effects:    effectsFor(mv),
	}

	// White and black are placed in different registers so the two players are
	// audibly distinct: White in the base octave, Black a fifth higher.
	base := 12 * (cfg.BaseOctave + 1) // MIDI octave: C in octave o = 12*(o+1)
	if mv.Color == pgn.Black {
		base += 7
	}

	if mv.Castle != pgn.NoCastle {
		// Castling is a structural, ceremonial move: render it as a triad.
		root := base
		if mv.Castle == pgn.QueenSide {
			root += 5 // a different colour for the two castling sides
		}
		n.Pitches = []int{root, root + 4, root + 7} // major triad
		n.Duration = 4
		return n
	}

	pitch := base + fileScale[mv.File] + 12*(mv.Rank/2)

	// A promotion lifts the pawn into its new piece's voice by an octave.
	if mv.Promotion != 0 {
		pitch += 12
	}

	n.Pitches = []int{pitch}
	if d, ok := durationByPiece[mv.Piece]; ok {
		n.Duration = d
	} else {
		n.Duration = 2
	}
	return n
}

// velocityFor maps move annotations to loudness: captures are accented, checks
// louder still, and checkmate is the loudest, most emphatic event.
func velocityFor(mv pgn.Move) int {
	switch {
	case mv.Mate:
		return 127
	case mv.Check:
		return 112
	case mv.Capture:
		return 100
	default:
		return 76
	}
}

// effectsFor collects the special-move flourishes that apply to a move. A move
// can trigger several at once (e.g. a capture that gives checkmate).
func effectsFor(mv pgn.Move) Effect {
	var e Effect
	if mv.Capture {
		e |= EffectCapture
	}
	if mv.Check {
		e |= EffectCheck
	}
	if mv.Mate {
		e |= EffectMate
	}
	if mv.Castle != pgn.NoCastle {
		e |= EffectCastle
	}
	return e
}
