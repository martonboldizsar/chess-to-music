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
	// Intro enables a short opening motif played before the game: a recognisable
	// musical "hook" that is the same for every game in the same opening, so a
	// game can be recalled by its tune.
	Intro bool
	// Harmony enables a bass line and sustained chord pad underneath the melody,
	// giving the piece a harmonic foundation so it sounds like a song rather
	// than a single unaccompanied line.
	Harmony bool
	// Instruments optionally overrides which instrument each piece plays. Any
	// piece missing from the map falls back to the default assignment.
	Instruments map[pgn.Piece]Instrument
}

// KeyAuto is the sentinel Config.Key value meaning "derive the key from the
// game" instead of using a fixed tonic.
const KeyAuto = -1

// DefaultConfig returns a musically sensible default mapping.
func DefaultConfig() Config {
	return Config{BaseOctave: 4, Tempo: 120, Scale: ScaleMajorPentatonic, Key: KeyAuto, Beat: 2, Meter: 4, Intro: true, Harmony: true}
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
	Title         string
	Opening       string // recognised opening name, if any (for labelling)
	Tempo         int
	Notes         []Note      // the sequential melody
	IntroNotes    int         // how many leading Notes are the opening motif
	Accompaniment []TimedNote // bass + chord pad played underneath the melody
	Config        Config
}

// AccVoice identifies which accompaniment layer a TimedNote belongs to, so the
// renderers can give the bass and the chord pad their own timbre and register.
type AccVoice int

const (
	AccBass AccVoice = iota // the low root note holding down the harmony
	AccPad                  // the sustained chord pad filling out the harmony
)

// TimedNote is a note scheduled to start at an absolute position in the piece,
// used for the accompaniment that plays concurrently with the sequential
// melody. Start and Duration are measured in eighth-note units.
type TimedNote struct {
	Start    int
	Duration int
	Pitches  []int
	Velocity int
	Voice    AccVoice
}

// Build converts a parsed game into a Score using the supplied configuration.
// Moves are laid out on a steady beat grid, the note that opens each bar is
// accented so the meter is easy to feel, a short opening motif is played first
// as a recognisable hook, and (when enabled) a bass line and chord pad are laid
// underneath so the result sounds like a song.
func Build(game *pgn.Game, cfg Config) Score {
	cfg = cfg.resolve(game)
	s := Score{Title: game.Title(), Tempo: cfg.Tempo, Config: cfg}
	barEighths := cfg.Beat * cfg.Meter
	pos := 0 // running position in eighth units
	var starts []int
	emit := func(n Note) {
		if barEighths > 0 && pos%barEighths == 0 {
			n.Velocity = accentDownbeat(n.Velocity)
		}
		starts = append(starts, pos)
		s.Notes = append(s.Notes, n)
		pos += n.Duration
	}

	intro, opening := cfg.introMotif(game)
	if opening == "" {
		opening = strings.TrimSpace(game.Tags["Opening"])
	}
	s.Opening = opening
	for _, n := range intro {
		emit(n)
	}
	s.IntroNotes = len(intro)
	for _, mv := range game.Moves {
		emit(noteFor(mv, cfg))
	}
	s.Accompaniment = cfg.accompaniment(s.Notes, starts)
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

// accompaniment builds the harmonic foundation: for every bar a low bass note
// and a sustained chord pad, both rooted on a scale degree drawn from the
// melody sounding on that bar's downbeat. Because the chord is built from the
// melody's own note and stacked in scale thirds, it is always consonant and in
// key, and because it follows the game it stays deterministic.
func (cfg Config) accompaniment(notes []Note, starts []int) []TimedNote {
	if !cfg.Harmony || len(notes) == 0 {
		return nil
	}
	barEighths := cfg.Beat * cfg.Meter
	if barEighths <= 0 {
		return nil
	}
	total := starts[len(starts)-1] + notes[len(notes)-1].Duration
	steps := scaleSteps[cfg.Scale]
	base := 12*(cfg.BaseOctave+1) + cfg.Key

	var acc []TimedNote
	for barStart := 0; barStart < total; barStart += barEighths {
		root := cfg.chordRoot(notes, starts, barStart, barEighths)

		// Bass: the chord root, two octaves below the melody's base register.
		bass := base + steps[root%len(steps)] - 24
		acc = append(acc, TimedNote{
			Start:    barStart,
			Duration: barEighths,
			Pitches:  []int{bass},
			Velocity: 70,
			Voice:    AccBass,
		})

		// Pad: a triad stacked in scale thirds (root, +2, +4 degrees), one
		// octave below the melody base so it sits under the tune.
		triad := []int{
			base + cfg.degreeOffset(root) - 12,
			base + cfg.degreeOffset(root+2) - 12,
			base + cfg.degreeOffset(root+4) - 12,
		}
		acc = append(acc, TimedNote{
			Start:    barStart,
			Duration: barEighths,
			Pitches:  triad,
			Velocity: 44,
			Voice:    AccPad,
		})
	}
	return acc
}

// chordRoot picks the scale degree to harmonise a bar with: the degree of the
// melody note sounding on the bar's downbeat (or the first note that starts in
// the bar), falling back to the tonic when the bar holds no melody.
func (cfg Config) chordRoot(notes []Note, starts []int, barStart, barEighths int) int {
	for i, st := range starts {
		if st <= barStart && barStart < st+notes[i].Duration && len(notes[i].Pitches) > 0 {
			return cfg.degreeOf(notes[i].Pitches[0])
		}
	}
	for i, st := range starts {
		if st >= barStart && st < barStart+barEighths && len(notes[i].Pitches) > 0 {
			return cfg.degreeOf(notes[i].Pitches[0])
		}
	}
	return 0
}

// degreeOf returns the scale degree (index into the scale) nearest to a pitch,
// ignoring octave. Used to read a chord root back off the in-key melody.
func (cfg Config) degreeOf(pitch int) int {
	steps := scaleSteps[cfg.Scale]
	tonic := 12*(cfg.BaseOctave+1) + cfg.Key
	pc := ((pitch-tonic)%12 + 12) % 12
	best, bestDist := 0, 100
	for i, s := range steps {
		d := pc - s
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			bestDist = d
			best = i
		}
	}
	return best
}

// degreeOffset converts a scale degree to a semitone offset above the tonic.
// Degrees beyond the scale wrap into higher octaves (and negative degrees into
// lower ones), so a motif can range freely while always staying in key.
func (cfg Config) degreeOffset(degree int) int {
	steps := scaleSteps[cfg.Scale]
	n := len(steps)
	idx := degree % n
	oct := degree / n
	if idx < 0 {
		idx += n
		oct--
	}
	return steps[idx] + 12*oct
}

// openingMotifVelocity is how loud the intro hook plays: present and clear, but
// not as punchy as the game's accents.
const openingMotifVelocity = 92

// introMotif builds the short opening hook for a game and returns it together
// with the recognised opening name (empty when unknown). The hook is the same
// for every game that starts with the same opening, which is what lets players
// recognise an opening by its tune. It is padded to whole bars so the game
// itself still begins on a downbeat.
func (cfg Config) introMotif(game *pgn.Game) ([]Note, string) {
	if !cfg.Intro {
		return nil, ""
	}
	tokens := openingTokens(game)
	if len(tokens) == 0 {
		return nil, ""
	}
	name, degrees := lookupOpening(tokens, len(scaleSteps[cfg.Scale]))
	if len(degrees) == 0 {
		return nil, name
	}

	base := 12*(cfg.BaseOctave+1) + cfg.Key
	notes := make([]Note, 0, len(degrees))
	for _, d := range degrees {
		notes = append(notes, Note{
			Pitches:    []int{base + cfg.degreeOffset(d)},
			Duration:   cfg.Beat,
			Velocity:   openingMotifVelocity,
			Color:      pgn.White,
			Instrument: InstViolin, // a clear, singing lead for the hook
			SAN:        "opening",
		})
	}

	// Pad the motif out to a whole number of bars so the first move lands on a
	// downbeat. The final note simply rings a little longer.
	if barEighths := cfg.Beat * cfg.Meter; barEighths > 0 {
		total := 0
		for _, n := range notes {
			total += n.Duration
		}
		if rem := total % barEighths; rem != 0 {
			notes[len(notes)-1].Duration += barEighths - rem
		}
	}
	return notes, name
}

// openingTokens returns the normalised SAN of the game's first few plies, with
// check/mate and annotation marks stripped, for matching against known
// openings and for deriving a fallback motif.
func openingTokens(game *pgn.Game) []string {
	const maxPlies = 8
	var toks []string
	for i, mv := range game.Moves {
		if i >= maxPlies {
			break
		}
		toks = append(toks, strings.TrimRight(mv.SAN, "+#!?"))
	}
	return toks
}

// opening pairs a leading SAN move sequence with its name and signature motif
// (a sequence of scale degrees). Degrees are kept within 0..5 so every motif
// stays musical in all supported scales.
type opening struct {
	moves []string
	name  string
	motif []int
}

// openingTable lists well-known openings, each with a hand-picked motif. Longer
// (more specific) entries win over shorter ones, so a Ruy Lopez is recognised
// ahead of the generic King's Pawn opening.
var openingTable = []opening{
	{[]string{"e4"}, "King's Pawn Opening", []int{0, 2, 4, 2, 0}},
	{[]string{"d4"}, "Queen's Pawn Opening", []int{0, 1, 3, 1, 0}},
	{[]string{"c4"}, "English Opening", []int{4, 2, 0, 2, 4}},
	{[]string{"Nf3"}, "Réti Opening", []int{0, 3, 5, 3, 0}},
	{[]string{"f4"}, "Bird's Opening", []int{2, 4, 3, 1, 0}},
	{[]string{"e4", "c5"}, "Sicilian Defense", []int{4, 3, 2, 1, 0}},
	{[]string{"e4", "e6"}, "French Defense", []int{0, 1, 2, 1, 0}},
	{[]string{"e4", "c6"}, "Caro-Kann Defense", []int{0, 2, 1, 3, 0}},
	{[]string{"e4", "d5"}, "Scandinavian Defense", []int{5, 4, 3, 2, 0}},
	{[]string{"e4", "d6"}, "Pirc Defense", []int{1, 3, 2, 4, 0}},
	{[]string{"e4", "g6"}, "Modern Defense", []int{3, 1, 4, 2, 0}},
	{[]string{"e4", "Nf6"}, "Alekhine Defense", []int{4, 5, 3, 2, 1, 0}},
	{[]string{"e4", "Nc6"}, "Nimzowitsch Defense", []int{2, 1, 3, 2, 0}},
	{[]string{"e4", "e5", "Nc3"}, "Vienna Game", []int{2, 4, 2, 0}},
	{[]string{"e4", "e5", "f4"}, "King's Gambit", []int{0, 4, 3, 5, 4, 0}},
	{[]string{"e4", "e5", "Nf3", "Nf6"}, "Petrov Defense", []int{0, 1, 0, 2, 0}},
	{[]string{"e4", "e5", "Nf3", "d6"}, "Philidor Defense", []int{0, 1, 2, 3, 1, 0}},
	{[]string{"e4", "e5", "Nf3", "Nc6", "Bb5"}, "Ruy Lopez", []int{0, 2, 4, 5, 4, 0}},
	{[]string{"e4", "e5", "Nf3", "Nc6", "Bc4"}, "Italian Game", []int{0, 4, 2, 4, 5, 0}},
	{[]string{"e4", "e5", "Nf3", "Nc6", "d4"}, "Scotch Game", []int{0, 4, 5, 4, 2, 0}},
	{[]string{"e4", "e5", "Nf3", "Nc6", "Nc3"}, "Four Knights Game", []int{0, 2, 4, 2, 3, 0}},
	{[]string{"d4", "d5", "c4"}, "Queen's Gambit", []int{0, 2, 4, 3, 2, 0}},
	{[]string{"d4", "d5", "c4", "c6"}, "Slav Defense", []int{0, 2, 3, 2, 0}},
	{[]string{"d4", "d5", "c4", "e6"}, "Queen's Gambit Declined", []int{0, 2, 3, 2, 4, 0}},
	{[]string{"d4", "d5", "c4", "dxc4"}, "Queen's Gambit Accepted", []int{0, 4, 2, 5, 3, 0}},
	{[]string{"d4", "d5", "c4", "c6", "Nf3", "Nf6", "Nc3", "e6"}, "Semi-Slav Defense", []int{0, 2, 3, 1, 4, 0}},
	{[]string{"d4", "d5", "Bf4"}, "London System", []int{0, 2, 1, 2, 3, 0}},
	{[]string{"d4", "Nf6", "c4", "g6"}, "King's Indian Defense", []int{0, 3, 2, 5, 4, 0}},
	{[]string{"d4", "Nf6", "c4", "g6", "Nc3", "d5"}, "Grünfeld Defense", []int{5, 3, 4, 2, 1, 0}},
	{[]string{"d4", "Nf6", "c4", "e6"}, "Indian Defense", []int{2, 0, 3, 1, 0}},
	{[]string{"d4", "Nf6", "c4", "e6", "Nc3", "Bb4"}, "Nimzo-Indian Defense", []int{2, 4, 5, 3, 1, 0}},
	{[]string{"d4", "Nf6", "c4", "e6", "Nf3", "Bb4"}, "Bogo-Indian Defense", []int{2, 5, 3, 4, 1, 0}},
	{[]string{"d4", "Nf6", "c4", "e6", "g3"}, "Catalan Opening", []int{0, 3, 2, 5, 4, 0}},
	{[]string{"d4", "Nf6", "c4", "c5"}, "Benoni Defense", []int{1, 3, 5, 3, 1, 0}},
	{[]string{"d4", "Nf6", "Bg5"}, "Trompowsky Attack", []int{3, 2, 4, 1, 0}},
	{[]string{"d4", "f5"}, "Dutch Defense", []int{3, 1, 4, 2, 0}},
}

// lookupOpening finds the most specific known opening that prefixes the game's
// moves, returning its name and motif. When nothing matches it returns an empty
// name and a motif derived deterministically from the moves, so even unknown
// lines get their own consistent signature.
func lookupOpening(tokens []string, scaleLen int) (string, []int) {
	bestLen := -1
	var bestName string
	var bestMotif []int
	for _, o := range openingTable {
		if len(o.moves) > len(tokens) || len(o.moves) <= bestLen {
			continue
		}
		if hasPrefix(tokens, o.moves) {
			bestLen = len(o.moves)
			bestName = o.name
			bestMotif = o.motif
		}
	}
	if bestLen >= 0 {
		return bestName, bestMotif
	}
	return "", deriveMotif(tokens, scaleLen)
}

// hasPrefix reports whether prefix matches the start of tokens.
func hasPrefix(tokens, prefix []string) bool {
	if len(prefix) > len(tokens) {
		return false
	}
	for i, p := range prefix {
		if tokens[i] != p {
			return false
		}
	}
	return true
}

// deriveMotif produces a deterministic five-note signature for an unrecognised
// opening by hashing its moves, so the same line always sounds the same. The
// phrase resolves back to the tonic for a sense of closure.
func deriveMotif(tokens []string, scaleLen int) []int {
	if scaleLen <= 0 {
		return nil
	}
	h := fnv.New64a()
	h.Write([]byte(strings.Join(tokens, " ")))
	x := h.Sum64()
	motif := make([]int, 0, 5)
	for i := 0; i < 4; i++ {
		motif = append(motif, int(x%uint64(scaleLen)))
		x /= uint64(scaleLen)
	}
	motif = append(motif, 0) // end on the tonic
	return motif
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
