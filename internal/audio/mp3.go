package audio

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

// ErrNoFFmpeg indicates that the ffmpeg binary could not be found, so MP3
// encoding was skipped.
var ErrNoFFmpeg = fmt.Errorf("ffmpeg not found in PATH")

// HasFFmpeg reports whether an ffmpeg binary is available for MP3 encoding.
func HasFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// ConvertToMP3 encodes an existing WAV file into an MP3 using ffmpeg's LAME
// encoder. It returns ErrNoFFmpeg (without an error otherwise) when ffmpeg is
// not installed, so callers can degrade gracefully and keep the WAV output.
func ConvertToMP3(wavPath, mp3Path string) error {
	if !HasFFmpeg() {
		return ErrNoFFmpeg
	}
	cmd := exec.Command("ffmpeg", "-y",
		"-i", wavPath,
		"-codec:a", "libmp3lame",
		"-qscale:a", "2",
		mp3Path,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg conversion failed: %w", err)
	}
	return nil
}

// WAVToMP3Bytes encodes WAV audio held in memory into MP3 bytes by piping it
// through ffmpeg's stdin/stdout, avoiding any temporary files. It returns
// ErrNoFFmpeg when ffmpeg is unavailable so callers can fall back to WAV.
func WAVToMP3Bytes(wav []byte) ([]byte, error) {
	if !HasFFmpeg() {
		return nil, ErrNoFFmpeg
	}
	cmd := exec.Command("ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-i", "pipe:0",
		"-f", "mp3",
		"-codec:a", "libmp3lame",
		"-qscale:a", "2",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(wav)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w: %s", err, stderr.String())
	}
	return out.Bytes(), nil
}
