// Package audio synthesises a Score into PCM audio, writes it as a WAV file,
// and (when ffmpeg is available) converts that WAV into an MP3.
package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"

	"chess-to-music/internal/music"
)

const (
	sampleRate = 44100
	amplitude  = 0.24 // headroom so summed chord partials don't clip
)

// RenderWAV synthesises the score to a 16-bit mono WAV file in memory.
//
// Each note is rendered as a short additive-synthesis tone (a few harmonics)
// shaped by an ADSR envelope. Every chess piece has its own timbre, special
// moves (captures, checks, mates, castling) mix in a synthesised sound effect,
// and any accompaniment (bass + chord pad) is overlaid underneath the melody.
func RenderWAV(s music.Score) []byte {
	secondsPerEighth := 30.0 / float64(s.Tempo) // 60 / tempo / 2

	// The melody is sequential: each note follows the previous one.
	var melody []float64
	for _, n := range s.Notes {
		for i, d := range n.AttackDurations() {
			dur := float64(d) * secondsPerEighth
			sub := n
			if i > 0 {
				sub.Effects = 0 // play special-move effects only on the first attack
			}
			melody = append(melody, renderNote(sub, dur)...)
		}
	}

	// Size the mix buffer to cover the melody and any accompaniment that runs
	// past it, then overlay the melody and the accompaniment at their offsets.
	total := len(melody)
	for _, a := range s.Accompaniment {
		if end := int(float64(a.Start+a.Duration) * secondsPerEighth * sampleRate); end > total {
			total = end
		}
	}
	mix := make([]float64, total)
	copy(mix, melody)
	for _, a := range s.Accompaniment {
		off := int(float64(a.Start) * secondsPerEighth * sampleRate)
		dur := float64(a.Duration) * secondsPerEighth
		for i, v := range renderAccompaniment(a, dur) {
			if off+i < len(mix) {
				mix[off+i] += v
			}
		}
	}

	return encodeWAV(mix)
}

// voice describes the timbre of one instrument. Besides the harmonic spectrum
// and vibrato, each voice carries its own amplitude envelope and spectral
// behaviour, because the attack and the way a tone evolves are what the ear
// uses most to tell instruments apart — far more than the static spectrum
// alone. The eight voices are deliberately exaggerated and made as different
// from each other as possible (a deep sub-octave drone, a pulsing tremolo, a
// short pluck, a long bell, a heavy wobble, an airy breath…) so that even an
// untrained listener can tell which file a move landed on.
type voice struct {
	harmonics []float64 // relative amplitude of each harmonic (1st, 2nd, …)

	vibrato     float64 // vibrato depth as a fraction of the fundamental (0 = none)
	vibratoRate float64 // vibrato speed in Hz (0 = a default 5.5 Hz)

	// subOctave mixes in a sine one octave below the fundamental, which the
	// harmonic series cannot reach. It makes a voice read as deep and dark.
	subOctave float64

	// tremolo wobbles the amplitude (not the pitch) at tremoloRate Hz, the
	// instantly recognisable pulsing of a tremolo organ.
	tremoloRate  float64
	tremoloDepth float64 // 0..1

	attack  float64 // onset time in seconds (short = sharp, long = gradual swell)
	release float64 // fade-out time in seconds (0 = a sensible default)

	// percussive voices have no sustain: after the attack the tone decays
	// exponentially toward silence with time-constant decay, the way a struck
	// or plucked string rings out. Sustained voices instead hold at sustain
	// until released.
	percussive bool
	decay      float64 // decay time-constant in seconds (percussive voices)
	sustain    float64 // held level after the attack (sustained voices, 0..1)

	// bright makes the higher harmonics fade over the course of the note, so the
	// tone gets mellower as it rings (strong on piano, subtle on brass). 0 keeps
	// the spectrum static, as a pipe organ's is.
	bright float64

	// twangDepth (0..1) sweeps a resonant formant down through the harmonics
	// over the note, the unmistakable descending "boing" of a jaw harp.
	twangDepth float64

	// breath mixes in a little airy noise, the signature of wind voices like the
	// flute (and, faintly, a choir).
	breath float64
}

var voices = [music.InstrumentCount]voice{
	// Tuba (a-file): a deep, dark foghorn — dominated by a sub-octave sine with
	// almost no upper harmonics and no modulation. The lowest, roundest voice,
	// and the easiest to pick out at the bottom of the mix.
	music.InstTuba: {
		harmonics: []float64{1, 0.22, 0.07},
		subOctave: 1.4,
		attack:    0.05,
		sustain:   0.9,
		release:   0.14,
	},
	// Jaw harp (b-file): a twangy plucked reed — a sharp pluck whose bright
	// resonance sweeps downward into a buzzy drone (the classic "boing"). The
	// sweeping formant makes it unlike any other voice.
	music.InstJawHarp: {
		harmonics:  []float64{1, 0.8, 0.8, 0.7, 0.7, 0.6, 0.55, 0.45, 0.4, 0.3},
		attack:     0.003,
		percussive: true,
		decay:      0.5,
		subOctave:  0.5,
		twangDepth: 1.0,
	},
	// Organ (c-file): a buzzy reed organ — hollow odd-harmonic spectrum, instant
	// attack, dead-flat sustain and a fast tremolo wobble that nothing else has.
	music.InstOrgan: {
		harmonics:    []float64{1, 0, 0.85, 0, 0.65, 0, 0.5, 0, 0.35},
		attack:       0.005,
		sustain:      1.0,
		release:      0.05,
		tremoloRate:  7.5,
		tremoloDepth: 0.35,
	},
	// Horn (d-file): bold, bright brass — a full saw-like spectrum, medium
	// attack, flat sustain and no modulation. Steady, forward and brassy.
	music.InstHorn: {
		harmonics: []float64{1, 0.82, 0.66, 0.52, 0.4, 0.3, 0.22, 0.15, 0.1},
		attack:    0.04,
		sustain:   0.9,
		release:   0.1,
		bright:    0.12,
	},
	// Viola (e-file): a plucked string (pizzicato) — a sharp pluck with a short,
	// woody decay and no sustain. A quick "ponk" that stands apart from every
	// sustained voice and is much shorter than the piano's ring.
	music.InstViola: {
		harmonics:  []float64{1, 0.75, 0.55, 0.32, 0.16},
		attack:     0.003,
		percussive: true,
		decay:      0.26,
		bright:     2.8,
	},
	// Piano (f-file): a struck string — a fast, bright attack that rings out and
	// mellows over a long decay. Bell-like, and clearly longer than the pluck.
	music.InstPiano: {
		harmonics:  []float64{1, 0.6, 0.4, 0.28, 0.18, 0.1, 0.05},
		attack:     0.004,
		percussive: true,
		decay:      1.5,
		bright:     1.0,
	},
	// Guitar (g-file): a warm plucked string — a soft pluck with a full, rounded
	// spectrum and a medium decay that mellows as it rings. Clearly plucked (not
	// bowed), warmer than the piano and far longer than the pizzicato.
	music.InstGuitar: {
		harmonics:  []float64{1, 0.8, 0.62, 0.45, 0.3, 0.2, 0.12, 0.06},
		attack:     0.005,
		percussive: true,
		decay:      0.75,
		bright:     0.9,
	},
	// Xylophone (h-file): a bright wooden mallet — a hard "ting" with a strong
	// upper partial that decays almost instantly to a pure tone. The shortest,
	// brightest, most percussive voice.
	music.InstXylophone: {
		harmonics:  []float64{1, 0, 0.7, 0, 0.4, 0, 0.25},
		attack:     0.002,
		percussive: true,
		decay:      0.16,
		bright:     3.5,
	},
}

// renderNote produces the PCM samples for a single note or chord.
func renderNote(n music.Note, dur float64) []float64 {
	total := int(dur * sampleRate)
	if total <= 0 {
		return nil
	}
	out := make([]float64, total)

	gain := float64(n.Velocity) / 127.0
	v := voices[n.Instrument]

	for _, p := range n.Pitches {
		freq := midiToFreq(p)
		for i := 0; i < total; i++ {
			t := float64(i) / sampleRate
			out[i] += gain * timbre(freq, t, v)
		}
	}

	// Normalise the chord by its voice count and apply the per-instrument
	// envelope, mixing in breath noise for wind voices as we go.
	scale := amplitude
	if len(n.Pitches) > 1 {
		scale /= math.Sqrt(float64(len(n.Pitches)))
	}
	breathLP := 0.0
	for i := range out {
		t := float64(i) / sampleRate
		env := voiceEnvelope(v, t, dur)
		out[i] *= scale * env
		if v.breath > 0 {
			// Low-passed deterministic noise gives an airy "breath" without any
			// shared RNG state, so the render stays reproducible.
			white := 2*hashNoise(i) - 1
			breathLP += 0.12 * (white - breathLP)
			out[i] += scale * env * gain * v.breath * breathLP
		}
	}

	// Mix in any special-move sound effects on top of the tone.
	addEffects(out, n.Effects, dur)
	return out
}

// accBassVoice and accPadVoice are the timbres of the accompaniment layers: a
// rounded low bass and a soft, fuller chord pad. They are deliberately mellow
// so they support the melody without competing with it.
var (
	accBassVoice = voice{harmonics: []float64{1, 0.5, 0.22}}
	accPadVoice  = voice{harmonics: []float64{1, 0.6, 0.4, 0.25, 0.15}, vibrato: 0.003}
)

// renderAccompaniment synthesises one bass note or chord-pad event, using a
// gentle swell so the harmony sits softly underneath the melody.
func renderAccompaniment(a music.TimedNote, dur float64) []float64 {
	total := int(dur * sampleRate)
	if total <= 0 || len(a.Pitches) == 0 {
		return nil
	}
	out := make([]float64, total)

	v := accBassVoice
	amp := 0.15
	if a.Voice == music.AccPad {
		v = accPadVoice
		amp = 0.09
	}
	gain := float64(a.Velocity) / 127.0

	for _, p := range a.Pitches {
		freq := midiToFreq(p)
		for i := 0; i < total; i++ {
			t := float64(i) / sampleRate
			out[i] += gain * timbre(freq, t, v)
		}
	}

	scale := amp
	if len(a.Pitches) > 1 {
		scale /= math.Sqrt(float64(len(a.Pitches)))
	}
	for i := range out {
		t := float64(i) / sampleRate
		out[i] *= scale * padEnvelope(t, dur)
	}
	return out
}

// padEnvelope shapes an accompaniment note: a slow swell in, a full sustain,
// and a gentle fade so chords blend smoothly from bar to bar.
func padEnvelope(t, dur float64) float64 {
	attack := math.Min(0.08, dur*0.25)
	release := math.Min(0.15, dur*0.3)
	switch {
	case t < attack:
		return t / attack
	case t < dur-release:
		return 1
	case t < dur:
		return (dur - t) / release
	default:
		return 0
	}
}

// timbre is an additive harmonic stack with optional vibrato, a sub-octave and
// amplitude tremolo, giving each instrument its own character.
func timbre(freq, t float64, v voice) float64 {
	f := freq
	if v.vibrato > 0 {
		rate := v.vibratoRate
		if rate == 0 {
			rate = 5.5
		}
		f *= 1 + v.vibrato*math.Sin(2*math.Pi*rate*t)
	}
	w := 2 * math.Pi * f * t
	// Twang: a resonant formant whose centre harmonic sweeps from high down to
	// low across the note, giving the jaw harp's descending "boing".
	var formantCenter, formantWidth float64
	if v.twangDepth > 0 {
		formantCenter = 1 + 9*math.Exp(-t/0.18) // sweeps ~10 → 1 over the note
		formantWidth = 2.5
	}
	var sum, norm float64
	for h, amp := range v.harmonics {
		if amp == 0 {
			continue
		}
		a := amp
		// Brightness decay: upper harmonics fade as the note rings, so the tone
		// mellows over time (pronounced on piano, none on the organ).
		if v.bright > 0 && h > 0 {
			a *= math.Exp(-v.bright * float64(h) * t)
		}
		// Twang formant: boost harmonics near the sweeping centre, cut the rest.
		if v.twangDepth > 0 {
			d := (float64(h+1) - formantCenter) / formantWidth
			res := math.Exp(-0.5 * d * d)
			a *= 1 - v.twangDepth + v.twangDepth*res
		}
		sum += a * math.Sin(float64(h+1)*w)
		norm += amp
	}
	// Sub-octave: a sine an octave below the fundamental, for a deep, dark voice.
	if v.subOctave > 0 {
		sum += v.subOctave * math.Sin(0.5*w)
		norm += v.subOctave
	}
	if norm == 0 {
		return 0
	}
	out := sum / norm
	// Tremolo: a pulsing amplitude wobble (the organ's signature).
	if v.tremoloDepth > 0 {
		rate := v.tremoloRate
		if rate == 0 {
			rate = 6
		}
		out *= 1 - v.tremoloDepth*(0.5+0.5*math.Sin(2*math.Pi*rate*t))
	}
	return out
}

// effectRNG is seeded deterministically so the synthesised noise effects are
// reproducible from run to run.
var effectRNG = rand.New(rand.NewSource(1))

// addEffects layers synthesised percussion/flourishes onto a note's samples.
func addEffects(out []float64, e music.Effect, dur float64) {
	if e == 0 {
		return
	}
	if e&music.EffectCapture != 0 {
		addDrumStrike(out) // a sharp drumstick hit for a capture
	}
	if e&music.EffectMate != 0 {
		// Checkmate ends the game with a deep, powerful drum hit.
		addDeepDrum(out, math.Min(dur, 1.2))
	}
	if e&music.EffectCastle != 0 {
		addDrumRoll(out, dur) // a short drum roll building into the castling triad
	}
}

// addDrumStrike mixes a sharp, dry drumstick hit (a stick striking a drum head)
// rather than a fizzy noise wash: a tight, fast-decaying pitched "tock" body
// gives it a defined knock, and a very short band-limited noise transient adds
// the initial "click" of the stick. Both decay quickly so the hit is punchy and
// articulate instead of hissy.
func addDrumStrike(out []float64) {
	addStickTap(out, 0, 1.0)
}

// addStickTap mixes a single drumstick tap into out starting at sample offset
// start, scaled by gain. It is the shared building block of the capture hit and
// the castling drum roll, so every tap has the same dry, wooden character.
func addStickTap(out []float64, start int, gain float64) {
	const bodyFreq = 190.0  // pitch of the drum "tock"
	const bodyDecay = 0.045 // fast decay keeps it dry and percussive
	const bodyAmp = 0.55
	const clickDecay = 0.006 // very short stick "click"
	const clickAmp = 0.35

	bp := 0.0 // running band-pass state for the click transient
	lp := 0.0
	for i := start; i < len(out); i++ {
		t := float64(i-start) / sampleRate

		// Pitched body: a sine "tock" with a snappy exponential decay.
		body := bodyAmp * math.Sin(2*math.Pi*bodyFreq*t) * math.Exp(-t/bodyDecay)

		// Stick click: band-passed noise (low-pass of a high-pass) so it reads
		// as a wooden "tack" instead of broadband fizz, gated to the attack.
		white := 2*effectRNG.Float64() - 1
		hp := white - lp
		lp = white
		bp += 0.45 * (hp - bp) // one-pole smoothing to tame the harsh top end
		click := clickAmp * bp * math.Exp(-t/clickDecay)

		out[i] += gain * (body + click)

		// Stop once this tap has fully decayed, so a roll's later taps are not
		// drowned by the tail of earlier ones.
		if t > 0.12 {
			break
		}
	}
}

// addDrumRoll mixes a short drum roll: a run of quick stick taps that crescendo
// into a final accented hit on the downbeat, giving castling a rolling fanfare
// instead of a fizzy swell. The roll is sized to the note so it always lands
// its accent at the start of the castling triad.
func addDrumRoll(out []float64, dur float64) {
	if len(out) == 0 {
		return
	}
	// Roll over the first part of the note (at most ~0.45s) so the triad still
	// rings clearly afterwards.
	rollDur := math.Min(dur*0.6, 0.45)
	const tapInterval = 0.038 // ~26 taps/sec: a tight roll
	taps := int(rollDur / tapInterval)
	if taps < 4 {
		taps = 4
	}
	for k := 0; k < taps; k++ {
		start := int(float64(k) * tapInterval * sampleRate)
		if start >= len(out) {
			break
		}
		// Crescendo from soft up to a full-strength accent on the last tap.
		gain := 0.25 + 0.55*float64(k)/float64(taps-1)
		addStickTap(out, start, gain)
	}
}

// addDeepDrum mixes a deep, robust drum hit (like a big kick/taiko boom): a
// low sine whose pitch drops quickly from a punchy attack down to a sub-bass
// body, layered with a short low-passed noise transient for the "thump".
func addDeepDrum(out []float64, length float64) {
	const startFreq = 150.0 // punchy attack pitch (Hz)
	const endFreq = 48.0    // deep sub-bass body it settles into
	const pitchDecay = 0.04 // how fast the pitch sweeps down
	const bodyDecay = 0.32  // how long the low tone rings
	const bodyAmp = 0.85    // loud and robust

	n := int(length * sampleRate)
	if n > len(out) {
		n = len(out)
	}
	phase := 0.0
	lpPrev := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate

		// Exponential pitch glide from startFreq down to endFreq.
		freq := endFreq + (startFreq-endFreq)*math.Exp(-t/pitchDecay)
		phase += 2 * math.Pi * freq / sampleRate
		tone := math.Sin(phase) * math.Exp(-t/bodyDecay)

		// Low-passed noise transient adds a punchy "thwack" at the very start.
		white := 2*effectRNG.Float64() - 1
		lpPrev += 0.05 * (white - lpPrev) // gentle one-pole low-pass
		click := lpPrev * math.Exp(-t/0.015) * 0.6

		out[i] += bodyAmp * (tone + click)
	}
}

func midiToFreq(midi int) float64 {
	return 440 * math.Pow(2, float64(midi-69)/12)
}

// voiceEnvelope shapes a note's amplitude over time according to the voice's
// own attack/decay/sustain/release, which is the main reason the instruments
// sound distinct: a piano is struck and decays away, a flute swells in and
// holds, an organ snaps on and stays flat.
func voiceEnvelope(v voice, t, dur float64) float64 {
	if t < 0 || t >= dur {
		return 0
	}
	attack := v.attack
	if attack < 0.002 {
		attack = 0.002
	}
	// Smoothstep attack so slow-attack voices swell in gently.
	atk := 1.0
	if t < attack {
		x := t / attack
		atk = x * x * (3 - 2*x)
	}

	if v.percussive {
		// Struck/plucked: decay exponentially toward silence, with a short
		// release so the note ends cleanly instead of clicking.
		decay := v.decay
		if decay <= 0 {
			decay = 0.8
		}
		amp := atk * math.Exp(-t/decay)
		const relTime = 0.02
		if t > dur-relTime {
			amp *= (dur - t) / relTime
		}
		return amp
	}

	// Sustained: attack to full, a brief decay to the sustain level, a flat
	// hold, then a release fade.
	release := v.release
	if release <= 0 {
		release = math.Min(0.12, dur*0.4)
	}
	const decay = 0.05
	level := v.sustain
	switch {
	case t < attack:
		return atk
	case t < attack+decay:
		return 1 - (1-level)*(t-attack)/decay
	case t < dur-release:
		return level
	default:
		return level * (dur - t) / release
	}
}

// hashNoise returns deterministic white noise in [0,1) for a sample index, so
// breath noise is reproducible run to run without any shared RNG state (which
// keeps identical games rendering to identical audio).
func hashNoise(i int) float64 {
	x := uint32(i)
	x = (x ^ 61) ^ (x >> 16)
	x = x + (x << 3)
	x = x ^ (x >> 4)
	x = x * 0x27d4eb2d
	x = x ^ (x >> 15)
	return float64(x) / float64(^uint32(0))
}

// encodeWAV wraps float samples in [-1,1] as a 16-bit PCM mono WAV file.
func encodeWAV(samples []float64) []byte {
	var data bytes.Buffer
	for _, s := range samples {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		binary.Write(&data, binary.LittleEndian, int16(s*32767))
	}

	const (
		numChannels   = 1
		bitsPerSample = 16
	)
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataLen := data.Len()

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVE")

	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&buf, binary.LittleEndian, uint16(numChannels))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&buf, binary.LittleEndian, uint32(byteRate))
	binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample))

	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataLen))
	buf.Write(data.Bytes())

	return buf.Bytes()
}
