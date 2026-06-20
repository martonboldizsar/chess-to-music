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
	InstPiano      Instrument = iota // struck string, long ring
	InstHorn                         // bright sustained brass
	InstOrgan                        // hollow reed organ with a tremolo pulse
	InstTuba                         // deep sustained sub-octave drone
	InstBassGuitar                   // deep, round plucked bass string
	InstJawHarp                      // twangy plucked metal with a sweeping formant
	InstCello                        // warm bowed sustained string
	InstXylophone                    // bright, short mallet "ting"
	InstrumentCount
)

// instrumentNames are the stable identifiers used by the CLI/API and the web
// UI to refer to instruments. The order matches the Instrument constants.
var instrumentNames = [InstrumentCount]string{
	InstPiano:      "piano",
	InstHorn:       "horn",
	InstOrgan:      "organ",
	InstTuba:       "tuba",
	InstBassGuitar: "bass guitar",
	InstJawHarp:    "jaw harp",
	InstCello:      "cello",
	InstXylophone:  "xylophone",
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
	EffectCheck                      // check: no added sound (only a louder note)
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
	// Rhythm, when non-empty, re-articulates the single pitch as a sequence of
	// attacks of these eighth-unit lengths (summing to Duration) instead of one
	// sustained note. It lets a move carry a recognisable rhythmic figure while
	// still counting as one note for animation and bar-keeping.
	Rhythm []int
	SAN    string // source move, kept for ABC comments
}

// AttackDurations returns the note's internal attack pattern in eighth units:
// the rhythm figure when set (re-articulating the same pitch), otherwise a
// single attack spanning the whole duration.
func (n Note) AttackDurations() []int {
	if len(n.Rhythm) > 0 {
		return n.Rhythm
	}
	return []int{n.Duration}
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
	// Chorus reprises the opening hook at the end as a chorus, giving the music
	// an ABA song form that resolves on the tonic. Hearing the hook return makes
	// the tune more memorable.
	Chorus bool
	// FileInstruments chooses the instrument for each file (column) a–h, indexed
	// 0 (a-file) .. 7 (h-file). The file a move lands on selects its timbre, so
	// every column has its own voice. Players can remap these.
	FileInstruments [8]Instrument
	// PieceRhythms chooses the rhythmic figure each kind of piece plays, by
	// rhythm-pattern name (see rhythmPatterns). The moving piece selects the
	// groove, so you can hear which piece moved. Players can remap these.
	PieceRhythms map[pgn.Piece]string
}

// KeyAuto is the sentinel Config.Key value meaning "derive the key from the
// game" instead of using a fixed tonic.
const KeyAuto = -1

// DefaultConfig returns a musically sensible default mapping. The key defaults
// to a fixed tonic (C) rather than "auto" so that a given rank sounds the same
// in every game, which is what makes games learnable by ear. Moves are voiced
// as rank→pitch, file→instrument and piece→rhythm; the file-instrument palette
// and per-piece rhythms below are the user-overridable defaults.
func DefaultConfig() Config {
	return Config{
		BaseOctave:      4,
		Tempo:           120,
		Scale:           ScaleMajorPentatonic,
		Key:             0,
		Beat:            2,
		Meter:           4,
		Intro:           true,
		Harmony:         true,
		Chorus:          true,
		FileInstruments: defaultFileInstruments,
		PieceRhythms:    defaultPieceRhythms(),
	}
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

// defaultFileInstruments is the file→timbre palette: the eight files a–h each
// get their own instrument. They are chosen to be as different from one another
// as possible — deep drone, twang, pulsing organ, brass, plucks of different
// lengths, a bright mallet — so an untrained ear can tell which file a move
// landed on. Users can override any of these via Config.
var defaultFileInstruments = [8]Instrument{
	InstTuba,       // a-file: deep sustained drone
	InstJawHarp,    // b-file: twangy plucked metal
	InstOrgan,      // c-file: pulsing reed organ
	InstHorn,       // d-file: bright sustained brass
	InstCello,      // e-file: warm bowed sustained string
	InstPiano,      // f-file: struck string, long ring
	InstBassGuitar, // g-file: deep, round plucked bass string
	InstXylophone,  // h-file: bright mallet "ting"
}

// fileInstrument returns the file's timbre, honouring the per-file overrides in
// cfg and falling back to the first voice when the file is out of range.
func (cfg Config) fileInstrument(file int) Instrument {
	if file < 0 || file >= len(cfg.FileInstruments) {
		return cfg.FileInstruments[0]
	}
	return cfg.FileInstruments[file]
}

// rhythmPattern is a named rhythmic figure: a list of eighth-unit attack
// lengths that together fill one 4/4 bar (summing to eight eighths). The moving
// piece selects a pattern, so a move keeps its single bar but its groove
// announces which piece moved.
type rhythmPattern struct {
	name  string
	beats []int
}

// rhythmPatterns are the selectable grooves, in canonical (UI) order. Each fills
// one bar so every move still occupies exactly one bar regardless of pattern.
var rhythmPatterns = []rhythmPattern{
	{"march", []int{2, 2, 2, 2}},         // four even quarters
	{"accent-front", []int{4, 2, 2}},     // long note in front
	{"accent-middle", []int{2, 4, 2}},    // long note in the middle
	{"accent-back", []int{2, 2, 4}},      // long note at the back
	{"stride", []int{4, 4}},              // two slow halves
	{"syncopated", []int{1, 2, 2, 2, 1}}, // off-beat lean
	{"gallop", []int{1, 1, 2, 2, 2}},     // quick double upbeat
	{"held", []int{8}},                   // one sustained whole note
}

// RhythmNames returns the selectable rhythm-pattern identifiers in canonical
// order, for CLI/API listing and UI dropdowns.
func RhythmNames() []string {
	names := make([]string, len(rhythmPatterns))
	for i, p := range rhythmPatterns {
		names[i] = p.name
	}
	return names
}

// rhythmBeats resolves a rhythm-pattern name to its eighth-unit figure,
// reporting whether the name was recognised.
func rhythmBeats(name string) ([]int, bool) {
	for _, p := range rhythmPatterns {
		if p.name == name {
			return p.beats, true
		}
	}
	return nil, false
}

// ValidRhythm reports whether name is a known rhythm-pattern identifier.
func ValidRhythm(name string) bool {
	_, ok := rhythmBeats(name)
	return ok
}

// defaultPieceRhythms assigns each piece a recognisable default groove: pawns
// march in even quarters, the king strides in two halves, the knight/rook/
// bishop place their long note at the front/middle/back of the bar, and the
// queen leans with an off-beat syncopation.
func defaultPieceRhythms() map[pgn.Piece]string {
	return map[pgn.Piece]string{
		pgn.Pawn:   "march",
		pgn.Knight: "accent-front",
		pgn.Rook:   "accent-middle",
		pgn.Bishop: "accent-back",
		pgn.King:   "stride",
		pgn.Queen:  "syncopated",
	}
}

// pieceRhythm returns the rhythmic figure (a list of eighth-unit note lengths)
// that identifies a piece, from the per-piece rhythm names in cfg. Each figure
// fills one 4/4 bar, so a move still occupies a single bar but its groove
// announces which piece moved. Unknown or unset pieces fall back to a march.
func (cfg Config) pieceRhythm(p pgn.Piece) []int {
	if name, ok := cfg.PieceRhythms[p]; ok {
		if beats, ok := rhythmBeats(name); ok {
			return beats
		}
	}
	beats, _ := rhythmBeats("march")
	return beats
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
// as a recognisable hook, a bass line and chord pad are laid underneath, and
// (when enabled) the hook returns at the end as a chorus so the result has an
// ABA song form.
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

	// Reprise the opening hook as a closing chorus. Pad the game out to a whole
	// bar first so the chorus starts cleanly on a downbeat.
	if outro := cfg.outroChorus(game); len(outro) > 0 {
		if barEighths > 0 && len(s.Notes) > 0 {
			if rem := pos % barEighths; rem != 0 {
				pad := barEighths - rem
				s.Notes[len(s.Notes)-1].Duration += pad
				pos += pad
			}
		}
		for _, n := range outro {
			emit(n)
		}
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

// chorusVelocity is how loud the closing chorus plays: a touch stronger than
// the intro so the returning hook lands like a climax.
const chorusVelocity = 100

// openingDegrees returns the scale-degree motif for the game's opening (curated
// when the line is known, otherwise derived) plus the recognised opening name.
func (cfg Config) openingDegrees(game *pgn.Game) ([]int, string) {
	tokens := openingTokens(game)
	if len(tokens) == 0 {
		return nil, ""
	}
	name, degrees := lookupOpening(tokens, len(scaleSteps[cfg.Scale]))
	return degrees, name
}

// renderMotif turns a sequence of scale degrees into melody notes played by a
// clear violin lead, padding the final note so the phrase fills whole bars and
// whatever follows lands on a downbeat.
func (cfg Config) renderMotif(degrees []int, velocity int, san string) []Note {
	if len(degrees) == 0 {
		return nil
	}
	base := 12*(cfg.BaseOctave+1) + cfg.Key
	notes := make([]Note, 0, len(degrees))
	for _, d := range degrees {
		notes = append(notes, Note{
			Pitches:    []int{base + cfg.degreeOffset(d)},
			Duration:   cfg.Beat,
			Velocity:   velocity,
			Color:      pgn.White,
			Instrument: InstXylophone, // a clear, bright plucked lead for the hook
			SAN:        san,
		})
	}
	if barEighths := cfg.Beat * cfg.Meter; barEighths > 0 {
		total := 0
		for _, n := range notes {
			total += n.Duration
		}
		if rem := total % barEighths; rem != 0 {
			notes[len(notes)-1].Duration += barEighths - rem
		}
	}
	return notes
}

// introMotif builds the short opening hook for a game and returns it together
// with the recognised opening name (empty when unknown). The hook is the same
// for every game that starts with the same opening, which is what lets players
// recognise an opening by its tune. It is padded to whole bars so the game
// itself still begins on a downbeat.
func (cfg Config) introMotif(game *pgn.Game) ([]Note, string) {
	if !cfg.Intro {
		return nil, ""
	}
	degrees, name := cfg.openingDegrees(game)
	return cfg.renderMotif(degrees, openingMotifVelocity, "opening"), name
}

// outroChorus reprises the opening hook at the end of the piece as a chorus and
// closes on a sustained tonic, giving the music an ABA song form with a clear
// resolution. The returning hook is what makes the tune stick in the memory.
func (cfg Config) outroChorus(game *pgn.Game) []Note {
	if !cfg.Chorus {
		return nil
	}
	degrees, _ := cfg.openingDegrees(game)
	chorus := cfg.renderMotif(degrees, chorusVelocity, "chorus")
	if len(chorus) == 0 {
		return nil
	}
	// End on a held tonic for a full bar: a satisfying, song-like close.
	dur := cfg.Beat * cfg.Meter
	if dur <= 0 {
		dur = cfg.Beat
	}
	chorus = append(chorus, Note{
		Pitches:    []int{12*(cfg.BaseOctave+1) + cfg.Key},
		Duration:   dur,
		Velocity:   openingMotifVelocity,
		Color:      pgn.White,
		Instrument: InstXylophone,
		SAN:        "ending",
	})
	return chorus
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

// noteFor maps a single move to a Note. The move's rank (row) becomes an
// in-scale pitch within about one octave, its file (column) chooses the
// instrument, and the piece type selects a per-bar rhythmic figure.
func noteFor(mv pgn.Move, cfg Config) Note {
	n := Note{
		Color:    mv.Color,
		SAN:      mv.SAN,
		Velocity: velocityFor(mv),
		Effects:  effectsFor(mv),
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
		// (a major triad in major scales, a minor triad in minor ones). It takes
		// the timbre of the king's destination file (g-side or c-side).
		steps := scaleSteps[cfg.Scale]
		root := base
		kingFile := 6 // g-file (kingside)
		if mv.Castle == pgn.QueenSide {
			root += 7    // a different colour (up a fifth) for the two castling sides
			kingFile = 2 // c-file (queenside)
		}
		third := steps[2%len(steps)]
		fifth := steps[4%len(steps)]
		n.Pitches = []int{root, root + third, root + fifth}
		n.Instrument = cfg.fileInstrument(kingFile)
		n.Duration = 2 * cfg.Beat // a two-beat fanfare
		return n
	}

	// The rank (row) becomes an in-scale pitch within about one octave.
	pitch := base + cfg.degreeOffset(mv.Rank)
	if mv.Promotion != 0 {
		pitch += 12 // a promotion lifts the new piece up an octave
	}
	n.Pitches = []int{pitch}
	n.Instrument = cfg.fileInstrument(mv.File)
	n.Rhythm = cfg.pieceRhythm(mv.Piece)
	total := 0
	for _, d := range n.Rhythm {
		total += d
	}
	n.Duration = total
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
