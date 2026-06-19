// Package db stores chess games in PostgreSQL so users can pick from a library
// of pre-saved games and save their own uploads.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a requested game does not exist.
var ErrNotFound = errors.New("game not found")

// Game is a stored chess game and its metadata.
type Game struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	White     string    `json:"white"`
	Black     string    `json:"black"`
	Event     string    `json:"event"`
	PGN       string    `json:"pgn"`
	Builtin   bool      `json:"builtin"`
	CreatedAt time.Time `json:"createdAt"`
}

// Store is a handle to the games database.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to PostgreSQL using the given URL and verifies the connection.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the database connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

const schema = `
CREATE TABLE IF NOT EXISTS games (
    id         BIGSERIAL PRIMARY KEY,
    title      TEXT NOT NULL,
    white      TEXT NOT NULL DEFAULT '',
    black      TEXT NOT NULL DEFAULT '',
    event      TEXT NOT NULL DEFAULT '',
    pgn        TEXT NOT NULL,
    builtin    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);`

// Migrate creates the games table if it does not already exist.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}
	return nil
}

// ListGames returns all stored games ordered with the built-in library first,
// then the most recently saved user games. The PGN body is omitted to keep the
// list lightweight; load it with GetGame.
func (s *Store) ListGames(ctx context.Context) ([]Game, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, white, black, event, builtin, created_at
		FROM games
		ORDER BY builtin DESC, created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing games: %w", err)
	}
	defer rows.Close()

	var games []Game
	for rows.Next() {
		var g Game
		if err := rows.Scan(&g.ID, &g.Title, &g.White, &g.Black, &g.Event, &g.Builtin, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning game: %w", err)
		}
		games = append(games, g)
	}
	return games, rows.Err()
}

// GetGame returns a single game including its PGN body.
func (s *Store) GetGame(ctx context.Context, id int64) (Game, error) {
	var g Game
	err := s.pool.QueryRow(ctx, `
		SELECT id, title, white, black, event, pgn, builtin, created_at
		FROM games WHERE id = $1`, id).
		Scan(&g.ID, &g.Title, &g.White, &g.Black, &g.Event, &g.PGN, &g.Builtin, &g.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Game{}, ErrNotFound
	}
	if err != nil {
		return Game{}, fmt.Errorf("getting game: %w", err)
	}
	return g, nil
}

// SaveGame inserts a new user game and returns it with its assigned ID.
func (s *Store) SaveGame(ctx context.Context, g Game) (Game, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO games (title, white, black, event, pgn, builtin)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`,
		g.Title, g.White, g.Black, g.Event, g.PGN, g.Builtin).
		Scan(&g.ID, &g.CreatedAt)
	if err != nil {
		return Game{}, fmt.Errorf("saving game: %w", err)
	}
	return g, nil
}
