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
		dur := float64(n.Duration) * secondsPerEighth
		melody = append(melody, renderNote(n, dur)...)
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

// voice describes the timbre of one instrument: the relative amplitude of each
// harmonic and an optional vibrato depth (as a fraction of the fundamental).
type voice struct {
	harmonics []float64
	vibrato   float64
}

var voices = [music.InstrumentCount]voice{
	music.InstPiano:  {harmonics: []float64{1, 0.5, 0.25, 0.12, 0.06}},
	music.InstHorn:   {harmonics: []float64{1, 0.6, 0.3, 0.12}},
	music.InstOrgan:  {harmonics: []float64{1, 0, 0.7, 0, 0.5, 0, 0.3}}, // hollow, odd harmonics
	music.InstTuba:   {harmonics: []float64{1, 0.7, 0.35, 0.15}},
	music.InstViolin: {harmonics: []float64{1, 0.8, 0.6, 0.45, 0.3, 0.2}, vibrato: 0.006},
	music.InstChoir:  {harmonics: []float64{1, 0.5, 0.35, 0.25, 0.15}, vibrato: 0.004},
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

	// Normalise the chord by its voice count and apply the envelope.
	scale := amplitude
	if len(n.Pitches) > 1 {
		scale /= math.Sqrt(float64(len(n.Pitches)))
	}
	for i := range out {
		t := float64(i) / sampleRate
		out[i] *= scale * envelope(t, dur)
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

// timbre is an additive harmonic stack with optional vibrato, giving each
// instrument its own character.
func timbre(freq, t float64, v voice) float64 {
	f := freq
	if v.vibrato > 0 {
		f *= 1 + v.vibrato*math.Sin(2*math.Pi*5.5*t) // ~5.5 Hz vibrato
	}
	w := 2 * math.Pi * f * t
	var sum, norm float64
	for h, amp := range v.harmonics {
		if amp == 0 {
			continue
		}
		sum += amp * math.Sin(float64(h+1)*w)
		norm += amp
	}
	if norm == 0 {
		return 0
	}
	return sum / norm
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
		addNoiseBurst(out, 0.05, 0.30) // sharp percussive hit for a capture
	}
	if e&music.EffectCheck != 0 {
		addBellPing(out, 2300, 0.18, 0.25) // bright triangle-like ping
	}
	if e&music.EffectMate != 0 {
		// Checkmate ends the game with a deep, powerful drum hit.
		addDeepDrum(out, math.Min(dur, 1.2))
	}
	if e&music.EffectCastle != 0 {
		addShaker(out, 0.35) // soft swell under the castling triad
	}
}

// addNoiseBurst mixes exponentially-decaying white noise (a cymbal/snare hit).
func addNoiseBurst(out []float64, decay, amp float64) {
	for i := range out {
		t := float64(i) / sampleRate
		out[i] += amp * (2*effectRNG.Float64() - 1) * math.Exp(-t/decay)
	}
}

// addBellPing mixes a decaying sine at freq (a triangle/bell ping).
func addBellPing(out []float64, freq, decay, amp float64) {
	for i := range out {
		t := float64(i) / sampleRate
		out[i] += amp * math.Sin(2*math.Pi*freq*t) * math.Exp(-t/decay)
	}
}

// addShaker mixes a gently-swelling band of noise (a shaker/maraca swell).
func addShaker(out []float64, peak float64) {
	n := len(out)
	if n == 0 {
		return
	}
	prev := 0.0
	for i := range out {
		// One-pole high-passed noise gives a brighter, shaker-like hiss.
		white := 2*effectRNG.Float64() - 1
		hp := white - prev
		prev = white
		// Triangular swell that rises then falls across the note.
		pos := float64(i) / float64(n)
		env := 1 - math.Abs(2*pos-1)
		out[i] += peak * hp * env
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

// envelope is a simple ADSR shape (attack, decay, sustain, release).
func envelope(t, dur float64) float64 {
	const attack = 0.008
	const decay = 0.06
	const sustain = 0.7
	release := math.Min(0.12, dur*0.4)

	switch {
	case t < attack:
		return t / attack
	case t < attack+decay:
		return 1 - (1-sustain)*(t-attack)/decay
	case t < dur-release:
		return sustain
	case t < dur:
		return sustain * (dur - t) / release
	default:
		return 0
	}
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
