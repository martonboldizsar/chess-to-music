# chess-to-music

Turn a chess game into music. `chess-to-music` reads a game in standard **PGN**
(Portable Game Notation) — the export format used by chess.com and lichess — and
maps every move to a musical note, then writes the result in several standard
formats.

Each move's destination square becomes a pitch, the moving piece decides the
note length, and annotations (captures, checks, checkmate) control loudness.
White and Black are placed in different registers and given different
instruments so you can hear the two players converse.

## Outputs

For an input game, the tool writes four files (using `-out <prefix>`):

| File            | Format                              | Purpose                                        |
| --------------- | ----------------------------------- | ---------------------------------------------- |
| `<prefix>.abc`  | [ABC notation](https://abcnotation.com) | Human-readable text listing every musical note |
| `<prefix>.mid`  | Standard MIDI File (format 0)       | Machine-readable music interchange             |
| `<prefix>.wav`  | 16-bit PCM WAV                      | Synthesised audio                              |
| `<prefix>.mp3`  | MP3                                 | Audio as MP3 (requires `ffmpeg`)               |

ABC notation and Standard MIDI Files are the two most widely used standards for
representing notes as text/data, so the rendering can be opened, viewed,
played, or converted in almost any music tool.

## Requirements

- Go 1.24+
- [`ffmpeg`](https://ffmpeg.org) on your `PATH` for MP3 output (optional — the
  WAV file is always written; MP3 is skipped with a message if `ffmpeg` is
  missing).
- [Bun](https://bun.sh) to build the web frontend.
- (Optional) Docker + PostgreSQL for the saved-game library. The server runs
  fine without it — the library features are simply disabled.

## Build

```sh
go build ./...
```

## Usage

```sh
# From a PGN file:
go run ./cmd/chess2music -in testdata/sample.pgn -out game

# From standard input:
pbpaste | go run ./cmd/chess2music -out game
```

### Flags

| Flag           | Default | Description                                                    |
| -------------- | ------- | ------------------------------------------------------------- |
| `-in`          | stdin   | Input PGN file path                                           |
| `-out`         | `game`  | Output file prefix                                            |
| `-tempo`       | `120`   | Playback tempo in quarter-note beats per minute               |
| `-base-octave` | `4`     | Base octave for White's pitches (octave 4 contains middle C)  |
| `-no-audio`    | `false` | Skip WAV/MP3 rendering (only write `.abc` and `.mid`)         |

Only the first game in a PGN file is rendered.

## Web UI

A small Svelte 5 frontend (built with [Bun](https://bun.sh)) lets you paste or
upload a game, pick which piece plays which instrument, generate the music, then
play it in the browser or download the MP3.

The quickest way to run the whole stack (app + Postgres) is Docker:

```sh
docker compose up --build -d   # or: make up
# open http://localhost:8080
```

To run it locally instead, build the frontend once, then run the server (it
embeds the built assets):

```sh
docker compose up -d                  # optional: Postgres game library
cd web && bun install && bun run build && cd ..
go run ./cmd/server -addr :8080
# open http://localhost:8080
```

Common tasks are wrapped in a `Makefile` — run `make` to list them (e.g.
`make build`, `make run`, `make check`, `make docker-build`, `make up`,
`make down`).

For frontend development with hot reload, run the Go server and Vite together —
Vite proxies `/api` to the Go server:

```sh
go run ./cmd/server -addr :8080      # terminal 1 (API)
cd web && bun run dev                 # terminal 2 (UI on http://localhost:5173)
```

### Game library (PostgreSQL)

With a database available, the UI shows a dropdown of pre-saved games and a
“Save to library” button. Start Postgres with the bundled compose file:

```sh
docker compose up -d
```

On first start the server creates its schema and seeds six famous games (the
Immortal, Evergreen and Opera games, the Game of the Century, Kasparov–Topalov
1999, and Fischer–Spassky 1972 game 6). The connection URL comes from `-db`, the
`DATABASE_URL` environment variable, or defaults to the compose service
(`postgres://chess:chess@localhost:5432/chess?sslmode=disable`). If the database
is unreachable, the server logs a warning and disables only the library.

### HTTP API

| Endpoint          | Method | Description                                                        |
| ----------------- | ------ | ------------------------------------------------------------------ |
| `/api/options`    | GET    | Lists pieces, instruments and the default mapping                  |
| `/api/generate`   | POST   | JSON `{pgn, tempo, baseOctave, instruments}` → MP3 (or WAV) audio  |
| `/api/games`      | GET    | Lists saved games (built-in library first), without PGN bodies     |
| `/api/games/{id}` | GET    | Returns one saved game including its PGN                            |
| `/api/games`      | POST   | JSON `{title, pgn}` → saves a game and returns it                  |

## How moves become music

- **Pitch** — the destination file (a–h) maps to a C-major scale step, and the
  rank (1–8) shifts the octave, so moves up the board rise in pitch.
- **Duration** — pawn = eighth, knight/bishop/king = quarter, rook = dotted
  quarter, queen = half note.
- **Instruments** — each piece has its own voice: pawn = piano, knight = horn,
  bishop = organ, rook = tuba, queen = violin, king = choir. These can be
  remapped per piece (via the web UI or the API).
- **Dynamics** — captures, checks and checkmate get progressively louder
  accents.
- **Effects** — special moves add a sound on top of the note: captures get a
  percussive hit, checks a bright ping, checkmate a deep drum hit, and castling a
  shaker swell.
- **Players** — White plays in the base register; Black plays a fifth higher.
- **Castling** — rendered as a major triad (a small "fanfare").
- **Promotion** — the promoted pawn jumps up an octave.

## Project layout

```
cmd/chess2music     command-line entry point
cmd/server          HTTP server + JSON API for the web UI
internal/pgn        PGN/SAN parser
internal/music      move-to-note mapping, ABC and MIDI writers
internal/audio      WAV synthesis and MP3 conversion
internal/db         PostgreSQL game library (pgx) + seed games
web                 Svelte 5 frontend (built with Bun), embedded by the server
Dockerfile          multi-stage build (Bun + Go) producing the runtime image
docker-compose.yml  app + Postgres stack for the game library
Makefile            common build/run/test/docker tasks
testdata            a sample PGN (Anderssen's "Immortal Game")
```

The core CLI has **no external Go dependencies** — the PGN parser, ABC writer,
MIDI writer and audio synthesiser are all self-contained. The optional game
library uses the `pgx` PostgreSQL driver.
