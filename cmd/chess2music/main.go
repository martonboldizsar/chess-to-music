// Command chess2music turns a chess game (PGN) into music. It reads a game in
// the standard PGN export format used by chess.com and lichess, then writes:
//
//   - <prefix>.abc  human-readable ABC notation listing every note
//   - <prefix>.mid  a Standard MIDI File (machine-readable music interchange)
//   - <prefix>.wav  synthesised audio
//   - <prefix>.mp3  the audio as MP3 (when ffmpeg is installed)
//   - <prefix>.mp4  an animated board video (with -video, when ffmpeg is installed)
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"chess-to-music/internal/audio"
	"chess-to-music/internal/board"
	"chess-to-music/internal/music"
	"chess-to-music/internal/pgn"
	"chess-to-music/internal/render"
	"chess-to-music/internal/video"
)

func main() {
	in := flag.String("in", "", "input PGN file (defaults to standard input)")
	out := flag.String("out", "game", "output file prefix")
	tempo := flag.Int("tempo", 120, "playback tempo in quarter-note beats per minute")
	baseOctave := flag.Int("base-octave", 4, "base octave for White's pitches (4 contains middle C)")
	noAudio := flag.Bool("no-audio", false, "skip WAV/MP3 audio rendering")
	makeVideo := flag.Bool("video", false, "also render an animated board video (<prefix>.mp4, needs ffmpeg)")
	view := flag.String("view", "lichess", "board view for the video: lichess or chesscom")
	flag.Parse()

	if err := run(*in, *out, *tempo, *baseOctave, *noAudio, *makeVideo, *view); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(inPath, prefix string, tempo, baseOctave int, noAudio, makeVideo bool, view string) error {
	// Read the PGN, either from a file or from standard input.
	var data []byte
	var err error
	if inPath == "" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(inPath)
	}
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	game, err := pgn.ParseFirst(strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("parsing PGN: %w", err)
	}
	if len(game.Moves) == 0 {
		return fmt.Errorf("no moves found in PGN input")
	}

	cfg := music.DefaultConfig()
	cfg.Tempo = tempo
	cfg.BaseOctave = baseOctave
	score := music.Build(game, cfg)

	// ABC notation: the human-readable list of notes.
	abcPath := prefix + ".abc"
	if err := os.WriteFile(abcPath, []byte(score.WriteABC()), 0o644); err != nil {
		return fmt.Errorf("writing ABC: %w", err)
	}
	fmt.Printf("wrote %s (ABC notation, %d notes)\n", abcPath, len(score.Notes))

	// Standard MIDI File: the machine-readable music format.
	midiPath := prefix + ".mid"
	if err := os.WriteFile(midiPath, score.WriteMIDI(), 0o644); err != nil {
		return fmt.Errorf("writing MIDI: %w", err)
	}
	fmt.Printf("wrote %s (Standard MIDI File)\n", midiPath)

	if noAudio {
		return nil
	}

	// Audio: synthesise to WAV, then convert to MP3 if ffmpeg is present.
	wavPath := prefix + ".wav"
	wav := audio.RenderWAV(score)
	if err := os.WriteFile(wavPath, wav, 0o644); err != nil {
		return fmt.Errorf("writing WAV: %w", err)
	}
	fmt.Printf("wrote %s (audio)\n", wavPath)

	mp3Path := prefix + ".mp3"
	switch err := audio.ConvertToMP3(wavPath, mp3Path); err {
	case nil:
		fmt.Printf("wrote %s (MP3)\n", mp3Path)
	case audio.ErrNoFFmpeg:
		fmt.Println("note: ffmpeg not found, skipped MP3 (the WAV file is still available)")
	default:
		return fmt.Errorf("converting to MP3: %w", err)
	}

	// Animated board video (optional).
	if makeVideo {
		if err := writeVideo(prefix, game, score, wav, view); err != nil {
			return err
		}
	}
	return nil
}

// writeVideo replays the game on a board and renders an MP4 of the pieces
// sliding in time with the music.
func writeVideo(prefix string, game *pgn.Game, score music.Score, wav []byte, view string) error {
	if !video.Available() {
		fmt.Println("note: ffmpeg not found, skipped MP4 video")
		return nil
	}
	theme, ok := render.ThemeByName(view)
	if !ok {
		return fmt.Errorf("unknown board view %q (use lichess or chesscom)", view)
	}
	positions, plies, err := board.Replay(game)
	if err != nil {
		return fmt.Errorf("replaying game: %w", err)
	}
	mp4, err := video.RenderMP4(score, plies, positions, theme, wav)
	if err != nil {
		return fmt.Errorf("rendering video: %w", err)
	}
	mp4Path := prefix + ".mp4"
	if err := os.WriteFile(mp4Path, mp4, 0o644); err != nil {
		return fmt.Errorf("writing MP4: %w", err)
	}
	fmt.Printf("wrote %s (animated %s board video)\n", mp4Path, theme.Label)
	return nil
}
