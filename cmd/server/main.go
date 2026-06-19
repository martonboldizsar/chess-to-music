// Command server exposes chess-to-music over HTTP: it serves the Svelte web UI
// and a small JSON API that turns a pasted/uploaded PGN game into audio.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"chess-to-music/internal/audio"
	"chess-to-music/internal/board"
	"chess-to-music/internal/db"
	"chess-to-music/internal/music"
	"chess-to-music/internal/pgn"
	"chess-to-music/internal/render"
	"chess-to-music/internal/video"
	"chess-to-music/web"
)

// defaultDatabaseURL is used when DATABASE_URL is not set; it matches the
// docker-compose Postgres service.
const defaultDatabaseURL = "postgres://chess:chess@localhost:5432/chess?sslmode=disable"

// server holds shared dependencies for the HTTP handlers.
type server struct {
	store *db.Store // may be nil when no database is available
}

// pieceByName maps the API's piece identifiers to PGN piece values.
var pieceByName = map[string]pgn.Piece{
	"pawn":   pgn.Pawn,
	"knight": pgn.Knight,
	"bishop": pgn.Bishop,
	"rook":   pgn.Rook,
	"queen":  pgn.Queen,
	"king":   pgn.King,
}

// generateRequest is the JSON body accepted by POST /api/generate.
type generateRequest struct {
	PGN         string            `json:"pgn"`
	Tempo       int               `json:"tempo"`
	BaseOctave  int               `json:"baseOctave"`
	Instruments map[string]string `json:"instruments"` // piece name -> instrument name
	Format      string            `json:"format"`      // "mp3" (audio) or "mp4" (animated video)
	BoardTheme  string            `json:"boardTheme"`  // "lichess" or "chesscom" (mp4 only)
}

func main() {
	addr := flag.String("addr", "", "address to listen on (defaults to $PORT, then :8080)")
	dbURL := flag.String("db", "", "PostgreSQL connection URL (defaults to $DATABASE_URL or the docker-compose service)")
	flag.Parse()

	// Resolve the listen address. Hosting platforms such as Render inject the
	// port to bind to via $PORT; locally we fall back to :8080.
	listenAddr := *addr
	if listenAddr == "" {
		if port := os.Getenv("PORT"); port != "" {
			listenAddr = ":" + port
		} else {
			listenAddr = ":8080"
		}
	}

	srv := &server{}

	// Connect to Postgres if available. The game library is optional: when the
	// database can't be reached the audio generation still works.
	url := *dbURL
	if url == "" {
		if env := os.Getenv("DATABASE_URL"); env != "" {
			url = env
		} else {
			url = defaultDatabaseURL
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if store, err := db.Open(ctx, url); err != nil {
		log.Printf("warning: database unavailable, game library disabled: %v", err)
	} else if err := store.Migrate(ctx); err != nil {
		log.Printf("warning: schema migration failed, game library disabled: %v", err)
		store.Close()
	} else if err := store.Seed(ctx); err != nil {
		log.Printf("warning: seeding games failed: %v", err)
		srv.store = store
	} else {
		log.Printf("connected to database; game library ready")
		srv.store = store
	}
	if srv.store != nil {
		defer srv.store.Close()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/options", handleOptions)
	mux.HandleFunc("POST /api/generate", handleGenerate)
	mux.HandleFunc("GET /api/games", srv.handleListGames)
	mux.HandleFunc("GET /api/games/{id}", srv.handleGetGame)
	mux.HandleFunc("POST /api/games", srv.handleSaveGame)

	// Serve the embedded Svelte build at the root.
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Fatalf("locating embedded web assets: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(dist)))

	log.Printf("chess-to-music server listening on %s", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatal(err)
	}
}

// handleOptions reports the available pieces and instruments so the UI can
// build its dropdowns without hard-coding the lists.
func handleOptions(w http.ResponseWriter, r *http.Request) {
	type boardTheme struct {
		Name  string `json:"name"`
		Label string `json:"label"`
	}
	type option struct {
		Pieces      []string          `json:"pieces"`
		Instruments []string          `json:"instruments"`
		Defaults    map[string]string `json:"defaults"`
		HasMP3      bool              `json:"hasMp3"`
		HasVideo    bool              `json:"hasVideo"`
		BoardThemes []boardTheme      `json:"boardThemes"`
	}
	defaults := map[string]string{}
	cfg := music.DefaultConfig()
	for name, piece := range pieceByName {
		defaults[name] = cfg.InstrumentForPiece(piece).String()
	}
	var themes []boardTheme
	for _, name := range render.ThemeNames() {
		if t, ok := render.ThemeByName(name); ok {
			themes = append(themes, boardTheme{Name: t.Name, Label: t.Label})
		}
	}
	writeJSON(w, http.StatusOK, option{
		Pieces:      []string{"pawn", "knight", "bishop", "rook", "queen", "king"},
		Instruments: music.InstrumentNames(),
		Defaults:    defaults,
		HasMP3:      audio.HasFFmpeg(),
		HasVideo:    video.Available(),
		BoardThemes: themes,
	})
}

// handleGenerate parses the PGN, builds the score with the requested instrument
// mapping, and returns audio (MP3 if ffmpeg is available, otherwise WAV).
func handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req generateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	game, err := pgn.ParseFirst(strings.NewReader(req.PGN))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("could not parse PGN: %v", err))
		return
	}
	if len(game.Moves) == 0 {
		writeError(w, http.StatusBadRequest, "no moves found in the PGN input")
		return
	}

	cfg := music.DefaultConfig()
	if req.Tempo >= 20 && req.Tempo <= 400 {
		cfg.Tempo = req.Tempo
	}
	if req.BaseOctave >= 1 && req.BaseOctave <= 7 {
		cfg.BaseOctave = req.BaseOctave
	}

	cfg.Instruments = map[pgn.Piece]music.Instrument{}
	for pieceName, instName := range req.Instruments {
		piece, ok := pieceByName[pieceName]
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown piece %q", pieceName))
			return
		}
		inst, ok := music.ParseInstrument(instName)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown instrument %q", instName))
			return
		}
		cfg.Instruments[piece] = inst
	}

	score := music.Build(game, cfg)
	wav := audio.RenderWAV(score)

	// MP4: animate the board in sync with the music.
	if strings.EqualFold(req.Format, "mp4") {
		generateVideo(w, game, score, wav, req.BoardTheme)
		return
	}

	mp3, err := audio.WAVToMP3Bytes(wav)
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Disposition", `inline; filename="chess-music.mp3"`)
		w.Write(mp3)
	case errors.Is(err, audio.ErrNoFFmpeg):
		// Fall back to WAV when ffmpeg is not installed.
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("Content-Disposition", `inline; filename="chess-music.wav"`)
		w.Write(wav)
	default:
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("audio conversion failed: %v", err))
	}
}

// generateVideo replays the game on a board, renders the animation in the
// chosen theme and returns an MP4 with the music as its soundtrack.
func generateVideo(w http.ResponseWriter, game *pgn.Game, score music.Score, wav []byte, themeName string) {
	if !video.Available() {
		writeError(w, http.StatusServiceUnavailable, "video generation requires ffmpeg, which is not installed on the server")
		return
	}
	if themeName == "" {
		themeName = "lichess"
	}
	theme, ok := render.ThemeByName(themeName)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown board theme %q", themeName))
		return
	}

	positions, plies, err := board.Replay(game)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("could not replay game: %v", err))
		return
	}

	mp4, err := video.RenderMP4(score, plies, positions, theme, wav)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("video generation failed: %v", err))
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `inline; filename="chess-music.mp4"`)
	w.Write(mp4)
}

// handleListGames returns the library of saved games (without PGN bodies).
func (s *server) handleListGames(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusOK, []db.Game{})
		return
	}
	games, err := s.store.ListGames(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("could not list games: %v", err))
		return
	}
	if games == nil {
		games = []db.Game{}
	}
	writeJSON(w, http.StatusOK, games)
}

// handleGetGame returns a single saved game including its PGN.
func (s *server) handleGetGame(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "game library is not available")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid game id")
		return
	}
	game, err := s.store.GetGame(r.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "game not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("could not load game: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, game)
}

// saveGameRequest is the JSON body accepted by POST /api/games.
type saveGameRequest struct {
	Title      string `json:"title"`
	PGN        string `json:"pgn"`
	BoardTheme string `json:"boardTheme"`
}

// handleSaveGame validates and stores a user-supplied game.
func (s *server) handleSaveGame(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "game library is not available")
		return
	}

	var req saveGameRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	game, err := pgn.ParseFirst(strings.NewReader(req.PGN))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("could not parse PGN: %v", err))
		return
	}
	if len(game.Moves) == 0 {
		writeError(w, http.StatusBadRequest, "no moves found in the PGN input")
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = game.Title()
	}

	theme := req.BoardTheme
	if theme == "" {
		theme = "lichess"
	}
	if _, ok := render.ThemeByName(theme); !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown board theme %q", theme))
		return
	}

	saved, err := s.store.SaveGame(r.Context(), db.Game{
		Title:      title,
		White:      game.Tags["White"],
		Black:      game.Tags["Black"],
		Event:      game.Tags["Event"],
		PGN:        req.PGN,
		Builtin:    false,
		BoardTheme: theme,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("could not save game: %v", err))
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
