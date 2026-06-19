// Package video renders a chess game as an MP4 in which the pieces slide across
// the board in time with the generated music. Frames are rasterised with the
// render package and muxed together with the audio track using ffmpeg.
package video

import (
	"bytes"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"chess-to-music/internal/board"
	"chess-to-music/internal/music"
	"chess-to-music/internal/render"
)

// ErrNoFFmpeg indicates that ffmpeg is unavailable, so video cannot be encoded.
var ErrNoFFmpeg = fmt.Errorf("ffmpeg not found in PATH")

// Available reports whether ffmpeg (required for encoding) is installed.
func Available() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

const (
	boardSize = 360 // pixel side length of the rendered board (multiple of 8)
	fps       = 12
	tailHold  = 1.2 // seconds the final position lingers after the music ends
)

// frameSpec captures everything needed to render one frame.
type frameSpec struct {
	base       board.Position
	highlights []board.Square
	movers     []render.Mover
}

// RenderMP4 produces an MP4 (H.264 + AAC) animating the game in sync with wav.
// positions must contain the initial position followed by the position after
// each ply; plies and score.Notes correspond one-to-one to the game's moves.
func RenderMP4(score music.Score, plies []board.Ply, positions []board.Position, theme render.Theme, wav []byte) ([]byte, error) {
	if !Available() {
		return nil, ErrNoFFmpeg
	}
	if len(positions) == 0 {
		return nil, fmt.Errorf("no positions to render")
	}

	specs := buildFrames(score, plies, positions)
	if len(specs) == 0 {
		return nil, fmt.Errorf("no frames to render")
	}

	dir, err := os.MkdirTemp("", "chess-video-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	if err := renderFrames(specs, theme, dir); err != nil {
		return nil, err
	}

	wavPath := filepath.Join(dir, "audio.wav")
	if err := os.WriteFile(wavPath, wav, 0o600); err != nil {
		return nil, fmt.Errorf("writing audio: %w", err)
	}
	outPath := filepath.Join(dir, "out.mp4")

	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error", "-y",
		"-framerate", fmt.Sprint(fps),
		"-i", filepath.Join(dir, "f%06d.png"),
		"-i", wavPath,
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		outPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg encode failed: %w: %s", err, stderr.String())
	}

	return os.ReadFile(outPath)
}

// buildFrames computes the per-frame animation specs for the whole game, keeping
// the cumulative frame count locked to the audio timeline to avoid drift.
func buildFrames(score music.Score, plies []board.Ply, positions []board.Position) []frameSpec {
	secondsPerEighth := 30.0 / float64(score.Tempo)

	n := len(plies)
	if len(score.Notes) < n {
		n = len(score.Notes)
	}

	var specs []frameSpec
	cumTime := 0.0
	framesDone := 0
	for i := 0; i < n; i++ {
		dur := float64(score.Notes[i].Duration) * secondsPerEighth
		endTime := cumTime + dur
		frameEnd := int(endTime*fps + 0.5)
		count := frameEnd - framesDone
		if count < 1 {
			count = 1
		}

		ply := plies[i]
		base := positions[i]
		removeSquare(&base, ply.From.From)
		if ply.HasCastle {
			removeSquare(&base, ply.RookFrom)
		}
		if ply.Capture {
			removeSquare(&base, ply.CaptureAt)
		}
		highlights := []board.Square{ply.From.From, ply.From.To}

		slideDur := dur * 0.6
		if slideDur > 0.30 {
			slideDur = 0.30
		}
		for j := 0; j < count; j++ {
			t := float64(j) / fps
			p := 1.0
			if slideDur > 0 {
				p = t / slideDur
			}
			if p > 1 {
				p = 1
			}
			e := easeInOut(p)

			movers := []render.Mover{{
				Kind:  ply.Piece,
				Color: ply.Color,
				File:  lerp(float64(ply.From.From.File), float64(ply.From.To.File), e),
				Rank:  lerp(float64(ply.From.From.Rank), float64(ply.From.To.Rank), e),
			}}
			if ply.HasCastle {
				rook := positions[i][ply.RookFrom.Rank][ply.RookFrom.File]
				movers = append(movers, render.Mover{
					Kind:  rook.Kind,
					Color: rook.Color,
					File:  lerp(float64(ply.RookFrom.File), float64(ply.RookTo.File), e),
					Rank:  lerp(float64(ply.RookFrom.Rank), float64(ply.RookTo.Rank), e),
				})
			}
			specs = append(specs, frameSpec{base: base, highlights: highlights, movers: movers})
		}

		cumTime = endTime
		framesDone = frameEnd
	}

	// Hold the final position for a moment after the music ends.
	final := positions[len(positions)-1]
	var lastHL []board.Square
	if n > 0 {
		lastHL = []board.Square{plies[n-1].From.From, plies[n-1].From.To}
	}
	tailFrames := int(math.Round(tailHold * float64(fps)))
	for j := 0; j < tailFrames; j++ {
		specs = append(specs, frameSpec{base: final, highlights: lastHL})
	}
	return specs
}

// renderFrames rasterises every frame to a PNG file, using a pool of renderers
// (one per worker, since font faces are not safe for concurrent use).
func renderFrames(specs []frameSpec, theme render.Theme, dir string) error {
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(specs) {
		workers = len(specs)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	setErr := func(e error) { once.Do(func() { firstErr = e }) }

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := render.NewRenderer(theme, boardSize)
			if err != nil {
				setErr(err)
				return
			}
			for idx := range jobs {
				s := specs[idx]
				img := r.Frame(s.base, s.highlights, s.movers)
				path := filepath.Join(dir, fmt.Sprintf("f%06d.png", idx+1))
				f, err := os.Create(path)
				if err != nil {
					setErr(err)
					return
				}
				if err := png.Encode(f, img); err != nil {
					f.Close()
					setErr(err)
					return
				}
				if err := f.Close(); err != nil {
					setErr(err)
					return
				}
			}
		}()
	}
	for i := range specs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return firstErr
}

func removeSquare(pos *board.Position, s board.Square) {
	if s.File < 0 || s.Rank < 0 {
		return
	}
	pos[s.Rank][s.File] = board.Piece{}
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func easeInOut(p float64) float64 {
	if p < 0.5 {
		return 2 * p * p
	}
	q := -2*p + 2
	return 1 - q*q/2
}
