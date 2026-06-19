package music

import (
	"bytes"
	"encoding/binary"
)

// MIDI constants.
const (
	ticksPerQuarter = 480
	ticksPerEighth  = ticksPerQuarter / 2

	// Channel 9 is the General MIDI percussion channel; we use it for the
	// special-move sound effects (captures, checks, mates, castling).
	percussionChannel = 9
)

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

	for _, n := range s.Notes {
		ch := midiChannel(n.Instrument)
		dur := uint32(n.Duration * ticksPerEighth)
		vel := byte(clampVelocity(n.Velocity))

		// Gather every simultaneous voice: the move's pitches plus any
		// percussion effects layered on the percussion channel.
		type voice struct{ ch, key, vel byte }
		var voices []voice
		for _, p := range n.Pitches {
			voices = append(voices, voice{ch, byte(p), vel})
		}
		for _, hit := range percussionFor(n.Effects) {
			voices = append(voices, voice{percussionChannel, hit.note, hit.velocity})
		}

		// Note-on for every voice at the current time (delta 0).
		for _, v := range voices {
			writeVarLen(&track, 0)
			track.Write([]byte{0x90 | v.ch, v.key, v.vel})
		}
		// Note-off for every voice after the note's duration. The first off
		// carries the duration delta; the rest happen simultaneously (delta 0).
		for i, v := range voices {
			if i == 0 {
				writeVarLen(&track, dur)
			} else {
				writeVarLen(&track, 0)
			}
			track.Write([]byte{0x80 | v.ch, v.key, 0})
		}
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
