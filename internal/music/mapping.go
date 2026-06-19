// Package music turns parsed chess moves into musical material and writes it
// out in standard notation formats (ABC notation and Standard MIDI Files).
package music

import (
	"chess-to-music/internal/pgn"
	"hash/fnv"
	"strings"
)

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
	// Scale is the set of pitches the melody is quantized to, so the whole
	// game stays in a single key and sounds song-like rather than wandering.
	Scale ScaleType
	// Key is the tonic pitch class (0 = C .. 11 = B). A negative value means
	// "derive a key from the game" so each game gets its own recognisable key.
	Key int
	// Beat is the length of one beat in eighth-note units (2 = a quarter-note
	// pulse). Every move is quantized to whole beats so the piece keeps a
	// steady, foot-tapping meter instead of drifting rhythmically.
	Beat int
	// Meter is the number of beats per bar (4 = common time). It groups moves
	// into bars and marks each bar's downbeat with a small accent.
	Meter int
	// Instruments optionally overrides which instrument each piece plays. Any
	// piece missing from the map falls back to the default assignment.
	Instruments map[pgn.Piece]Instrument
}

// KeyAuto is the sentinel Config.Key value meaning "derive the key from the
// game" instead of using a fixed tonic.
const KeyAuto = -1

// DefaultConfig returns a musically sensible default mapping.
func DefaultConfig() Config {
	return Config{BaseOctave: 4, Tempo: 120, Scale: ScaleMajorPentatonic, Key: KeyAuto, Beat: 2, Meter: 4}
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

// ScaleType selects the set of pitches the melody is quantized to. Keeping
// every note inside one scale is what makes the result sound like a tune in a
// key rather than a random walk.
type ScaleType int

const (
	ScaleMajorPentatonic ScaleType = iota // foolproof default: never sounds wrong
	ScaleMinorPentatonic
	ScaleMajor
	ScaleMinor
	ScaleDorian
	ScaleTypeCount
)

// scaleNames are the stable identifiers used by the CLI/API and web UI.
var scaleNames = [ScaleTypeCount]string{
	ScaleMajorPentatonic: "major-pentatonic",
	ScaleMinorPentatonic: "minor-pentatonic",
	ScaleMajor:           "major",
	ScaleMinor:           "minor",
	ScaleDorian:          "dorian",
}

// scaleSteps are the semitone offsets of each scale within one octave.
var scaleSteps = [ScaleTypeCount][]int{
	ScaleMajorPentatonic: {0, 2, 4, 7, 9},
	ScaleMinorPentatonic: {0, 3, 5, 7, 10},
	ScaleMajor:           {0, 2, 4, 5, 7, 9, 11},
	ScaleMinor:           {0, 2, 3, 5, 7, 8, 10},
	ScaleDorian:          {0, 2, 3, 5, 7, 9, 10},
}

// String returns the stable identifier for a scale.
func (s ScaleType) String() string {
	if s < 0 || s >= ScaleTypeCount {
		return "unknown"
	}
	return scaleNames[s]
}

// ScaleNames returns the available scale identifiers in canonical order.
func ScaleNames() []string {
	names := make([]string, ScaleTypeCount)
	copy(names, scaleNames[:])
	return names
}

// ParseScale resolves a scale identifier (e.g. "major-pentatonic") to its
// ScaleType value, reporting whether it was recognised.
func ParseScale(name string) (ScaleType, bool) {
	for i, n := range scaleNames {
		if n == name {
			return ScaleType(i), true
		}
	}
	return 0, false
}

// keyNames are the twelve tonic pitch classes, used to parse and display keys.
var keyNames = [12]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// KeyNames returns the selectable key identifiers for UI dropdowns: "auto"
// (derive from the game) followed by the twelve tonic note names.
func KeyNames() []string {
	names := make([]string, 0, len(keyNames)+1)
	names = append(names, "auto")
	names = append(names, keyNames[:]...)
	return names
}

// KeyName returns the note name of a tonic pitch class (0..11), or "auto" for
// the KeyAuto sentinel.
func KeyName(k int) string {
	if k == KeyAuto {
		return "auto"
	}
	if k < 0 || k > 11 {
		return "unknown"
	}
	return keyNames[k]
}

// ParseKey resolves a key name ("C", "F#", "auto", …) to a tonic pitch class,
// returning KeyAuto for "auto". Reports whether the name was recognised.
func ParseKey(name string) (int, bool) {
	if strings.EqualFold(name, "auto") {
		return KeyAuto, true
	}
	for i, n := range keyNames {
		if strings.EqualFold(n, name) {
			return i, true
		}
	}
	return 0, false
}

// deriveKey picks a deterministic tonic pitch class from the game's identity
// (players, event, date and opening moves), so the same game always yields the
// same key while different games are spread across the twelve keys.
func deriveKey(game *pgn.Game) int {
	h := fnv.New64a()
	write := func(s string) { h.Write([]byte(strings.ToLower(strings.TrimSpace(s)))) }
	write(game.Tags["White"])
	write(game.Tags["Black"])
	write(game.Tags["Event"])
	write(game.Tags["Date"])
	for i, mv := range game.Moves {
		if i >= 12 {
			break
		}
		write(mv.SAN)
	}
	return int(h.Sum64() % 12)
}

// resolve fills in any "auto" fields of the config from the game and clamps the
// scale to a valid value, returning the concrete config actually used.
func (cfg Config) resolve(game *pgn.Game) Config {
	if cfg.Scale < 0 || cfg.Scale >= ScaleTypeCount {
		cfg.Scale = ScaleMajorPentatonic
	}
	if cfg.Key < 0 || cfg.Key > 11 {
		cfg.Key = deriveKey(game)
	}
	if cfg.Beat <= 0 {
		cfg.Beat = 2
	}
	if cfg.Meter <= 0 {
		cfg.Meter = 4
	}
	return cfg
}

// pitchFor returns the in-scale pitch offset (relative to the tonic) for a
// board square. The file selects a scale degree — degrees past the end of the
// scale wrap into higher octaves, so the eight files trace a rising in-key
// line — and the rank lifts the note by whole octaves up the board.
func (cfg Config) pitchFor(file, rank int) int {
	steps := scaleSteps[cfg.Scale]
	n := len(steps)
	octave := file / n
	return steps[file%n] + 12*octave + 12*(rank/2)
}

// durationByPiece gives each piece a characteristic note length in *beats*
// (whole beats only, so every move lands squarely on the pulse). Lighter
// pieces get one beat; the queen rings out for two, the way a long note would
// in a tune.
var durationByPiece = map[pgn.Piece]int{
	pgn.Pawn:   1, // one beat
	pgn.Knight: 1, // one beat
	pgn.Bishop: 1, // one beat
	pgn.Rook:   1, // one beat
	pgn.Queen:  2, // two beats (a long, singing note)
	pgn.King:   1, // one beat
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
// Moves are laid out on a steady beat grid, and the note that opens each bar is
// accented so the meter is easy to feel.
func Build(game *pgn.Game, cfg Config) Score {
	cfg = cfg.resolve(game)
	s := Score{Title: game.Title(), Tempo: cfg.Tempo, Config: cfg}
	barEighths := cfg.Beat * cfg.Meter
	pos := 0 // running position in eighth units
	for _, mv := range game.Moves {
		n := noteFor(mv, cfg)
		if barEighths > 0 && pos%barEighths == 0 {
			n.Velocity = accentDownbeat(n.Velocity)
		}
		s.Notes = append(s.Notes, n)
		pos += n.Duration
	}
	return s
}

// accentDownbeat nudges a note louder so the first beat of each bar stands out,
// giving the ear a clear pulse. It never pushes past the MIDI maximum.
func accentDownbeat(v int) int {
	v += 12
	if v > 127 {
		return 127
	}
	return v
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
	// audibly distinct: White on the tonic, Black a fifth higher (a perfect
	// fifth is present in every supported scale, so both stay in key).
	base := 12*(cfg.BaseOctave+1) + cfg.Key // MIDI octave: C in octave o = 12*(o+1)
	if mv.Color == pgn.Black {
		base += 7
	}

	if mv.Castle != pgn.NoCastle {
		// Castling is a structural, ceremonial move: render it as a triad built
		// from the scale's 1st, 3rd and 5th degrees, so it matches the key
		// (a major triad in major scales, a minor triad in minor ones).
		steps := scaleSteps[cfg.Scale]
		root := base
		if mv.Castle == pgn.QueenSide {
			root += 7 // a different colour (up a fifth) for the two castling sides
		}
		third := steps[2%len(steps)]
		fifth := steps[4%len(steps)]
		n.Pitches = []int{root, root + third, root + fifth}
		n.Duration = 2 * cfg.Beat // a two-beat fanfare
		return n
	}

	pitch := base + cfg.pitchFor(mv.File, mv.Rank)

	// A promotion lifts the pawn into its new piece's voice by an octave.
	if mv.Promotion != 0 {
		pitch += 12
	}

	n.Pitches = []int{pitch}
	beats := 1
	if d, ok := durationByPiece[mv.Piece]; ok {
		beats = d
	}
	n.Duration = beats * cfg.Beat
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
