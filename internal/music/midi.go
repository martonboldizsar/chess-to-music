package music

import (
	"bytes"
	"encoding/binary"
	"sort"
)

// MIDI constants.
const (
	ticksPerQuarter = 480
	ticksPerEighth  = ticksPerQuarter / 2

	// Channel 9 is the General MIDI percussion channel; we use it for the
	// special-move sound effects (captures, checks, mates, castling).
	percussionChannel = 9

	// Dedicated channels for the accompaniment so it never collides with the
	// melody instruments (which occupy channels 0..5).
	bassChannel = 11
	padChannel  = 12
)

// gmAccBass and gmAccPad are the General MIDI programs for the accompaniment
// bass line and chord pad.
const (
	gmAccBass = 32 // Acoustic Bass
	gmAccPad  = 48 // String Ensemble 1
)

// accompChannel returns the MIDI channel for an accompaniment voice.
func accompChannel(v AccVoice) byte {
	if v == AccPad {
		return padChannel
	}
	return bassChannel
}

// gmProgram gives each Instrument a General MIDI program number (0-based) so a
// standard synthesiser plays each chess piece with its own voice.
var gmProgram = [InstrumentCount]byte{
	InstPiano:  0,  // Acoustic Grand Piano (pawns)
	InstHorn:   60, // French Horn (knights)
	InstOrgan:  19, // Church Organ (bishops)
	InstTuba:   58, // Tuba (rooks)
	InstViolin: 40, // Violin (queens)
	InstChoir:  52, // Choir Aahs (kings)
}

// midiChannel assigns each instrument a dedicated MIDI channel, skipping the
// percussion channel (9). With six instruments this never collides.
func midiChannel(inst Instrument) byte {
	ch := byte(inst)
	if ch >= percussionChannel {
		ch++
	}
	return ch
}

// gmPercussion lists the GM percussion notes triggered by each effect.
type gmHit struct {
	note     byte
	velocity byte
}

func percussionFor(e Effect) []gmHit {
	var hits []gmHit
	if e&EffectCapture != 0 {
		hits = append(hits, gmHit{38, 110}) // Acoustic Snare
	}
	if e&EffectCheck != 0 {
		hits = append(hits, gmHit{81, 100}) // Open Triangle
	}
	if e&EffectMate != 0 {
		// Checkmate: a deep, robust drum hit (low + acoustic bass drum).
		hits = append(hits, gmHit{35, 127}) // Acoustic Bass Drum
		hits = append(hits, gmHit{41, 120}) // Low Floor Tom
	}
	if e&EffectCastle != 0 {
		hits = append(hits, gmHit{82, 90}) // Shaker
	}
	return hits
}

// WriteMIDI renders the score as a Standard MIDI File (SMF format 0), the most
// common machine-readable music interchange format. The melody is sequential:
// each move sounds after the previous one. Each piece plays on its own MIDI
// channel and instrument, and special moves add percussion effects.
func (s Score) WriteMIDI() []byte {
	var track bytes.Buffer

	// Tempo meta event (microseconds per quarter note).
	usPerQuarter := 60_000_000 / s.Tempo
	writeVarLen(&track, 0)
	track.Write([]byte{0xFF, 0x51, 0x03,
		byte(usPerQuarter >> 16), byte(usPerQuarter >> 8), byte(usPerQuarter)})

	// Assign every instrument its program on its dedicated channel at time 0.
	for inst := Instrument(0); inst < InstrumentCount; inst++ {
		writeVarLen(&track, 0)
		track.Write([]byte{0xC0 | midiChannel(inst), gmProgram[inst]})
	}
	// Accompaniment programs on their dedicated channels.
	writeVarLen(&track, 0)
	track.Write([]byte{0xC0 | bassChannel, gmAccBass})
	writeVarLen(&track, 0)
	track.Write([]byte{0xC0 | padChannel, gmAccPad})

	// Collect every note-on/off as an absolute-time event, then sort and emit
	// them as delta times. This lets the concurrent accompaniment interleave
	// correctly with the sequential melody.
	type event struct {
		tick  uint32
		order int // note-offs (0) before note-ons (1) at the same tick
		data  []byte
	}
	var events []event
	add := func(tick uint32, order int, b ...byte) {
		events = append(events, event{tick, order, append([]byte(nil), b...)})
	}

	// Melody: each note follows the previous one.
	var cum uint32
	for _, n := range s.Notes {
		ch := midiChannel(n.Instrument)
		dur := uint32(n.Duration) * ticksPerEighth
		vel := byte(clampVelocity(n.Velocity))
		start, end := cum, cum+dur
		for _, p := range n.Pitches {
			add(start, 1, 0x90|ch, byte(p), vel)
			add(end, 0, 0x80|ch, byte(p), 0)
		}
		for _, hit := range percussionFor(n.Effects) {
			add(start, 1, 0x90|percussionChannel, hit.note, hit.velocity)
			add(end, 0, 0x80|percussionChannel, hit.note, 0)
		}
		cum = end
	}

	// Accompaniment: bass and pad at absolute positions on their own channels.
	for _, a := range s.Accompaniment {
		ch := accompChannel(a.Voice)
		start := uint32(a.Start) * ticksPerEighth
		end := uint32(a.Start+a.Duration) * ticksPerEighth
		vel := byte(clampVelocity(a.Velocity))
		for _, p := range a.Pitches {
			add(start, 1, 0x90|ch, byte(p), vel)
			add(end, 0, 0x80|ch, byte(p), 0)
		}
	}

	sort.SliceStable(events, func(i, j int) bool {
		if events[i].tick != events[j].tick {
			return events[i].tick < events[j].tick
		}
		return events[i].order < events[j].order
	})

	var prev uint32
	for _, e := range events {
		writeVarLen(&track, e.tick-prev)
		track.Write(e.data)
		prev = e.tick
	}

	// End-of-track meta event.
	writeVarLen(&track, 0)
	track.Write([]byte{0xFF, 0x2F, 0x00})

	var out bytes.Buffer
	// Header chunk: format 0, one track, division in ticks per quarter note.
	out.WriteString("MThd")
	binary.Write(&out, binary.BigEndian, uint32(6))
	binary.Write(&out, binary.BigEndian, uint16(0))
	binary.Write(&out, binary.BigEndian, uint16(1))
	binary.Write(&out, binary.BigEndian, uint16(ticksPerQuarter))

	// Track chunk.
	out.WriteString("MTrk")
	binary.Write(&out, binary.BigEndian, uint32(track.Len()))
	out.Write(track.Bytes())

	return out.Bytes()
}

func clampVelocity(v int) int {
	if v < 1 {
		return 1
	}
	if v > 127 {
		return 127
	}
	return v
}

// writeVarLen writes a MIDI variable-length quantity (used for delta times).
func writeVarLen(b *bytes.Buffer, value uint32) {
	buffer := value & 0x7F
	for value >>= 7; value > 0; value >>= 7 {
		buffer <<= 8
		buffer |= (value & 0x7F) | 0x80
	}
	for {
		b.WriteByte(byte(buffer))
		if buffer&0x80 != 0 {
			buffer >>= 8
		} else {
			break
		}
	}
}
