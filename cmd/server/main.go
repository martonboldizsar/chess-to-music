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
	"chess-to-music/internal/db"
	"chess-to-music/internal/music"
	"chess-to-music/internal/pgn"
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
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	dbURL := flag.String("db", "", "PostgreSQL connection URL (defaults to $DATABASE_URL or the docker-compose service)")
	flag.Parse()

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

	log.Printf("chess-to-music server listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

// handleOptions reports the available pieces and instruments so the UI can
// build its dropdowns without hard-coding the lists.
func handleOptions(w http.ResponseWriter, r *http.Request) {
	type option struct {
		Pieces      []string          `json:"pieces"`
		Instruments []string          `json:"instruments"`
		Defaults    map[string]string `json:"defaults"`
		HasMP3      bool              `json:"hasMp3"`
	}
	defaults := map[string]string{}
	cfg := music.DefaultConfig()
	for name, piece := range pieceByName {
		defaults[name] = cfg.InstrumentForPiece(piece).String()
	}
	writeJSON(w, http.StatusOK, option{
		Pieces:      []string{"pawn", "knight", "bishop", "rook", "queen", "king"},
		Instruments: music.InstrumentNames(),
		Defaults:    defaults,
		HasMP3:      audio.HasFFmpeg(),
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
	Title string `json:"title"`
	PGN   string `json:"pgn"`
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

	saved, err := s.store.SaveGame(r.Context(), db.Game{
		Title:   title,
		White:   game.Tags["White"],
		Black:   game.Tags["Black"],
		Event:   game.Tags["Event"],
		PGN:     req.PGN,
		Builtin: false,
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
