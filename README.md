# chess-to-music

Turn a chess game into music. `chess-to-music` reads a game in standard **PGN**
(Portable Game Notation) — the export format used by chess.com and lichess — and
maps every move to a musical note, then writes the result in several standard
formats.

> **Try it live:** [chess-to-music.onrender.com](https://chess-to-music.onrender.com)
> (hosted on Render's free tier, so the first load may take a moment to wake up).

Every move becomes a note. A move's **row** sets the pitch within a single
hummable octave, its **column** (file) picks the instrument, and the **piece
type** plays its own rhythmic figure — so a game is easy to learn and recall by
ear. The column instruments and per-piece rhythms are fully customisable.
Annotations (captures, checkmate, castling) add percussion, checks make a note
louder, and White and Black are placed in different registers so you can hear
the two players converse.

The result is shaped to sound like an actual song rather than a random walk:
every note is quantised to a single musical key, moves fall on a steady beat, a
bass line and chords play underneath, and a short hook based on the game's
opening bookends the piece as an intro and a closing chorus — so games (and
openings) become tunes you can actually remember.

It can also render an **animated MP4** in which the pieces slide across the
board in time with the music, in either a **Lichess** or **Chess.com** board
view.

## Outputs

For an input game, the tool writes these files (using `-out <prefix>`):

| File            | Format                              | Purpose                                        |
| --------------- | ----------------------------------- | ---------------------------------------------- |
| `<prefix>.abc`  | [ABC notation](https://abcnotation.com) | Human-readable text listing every musical note |
| `<prefix>.mid`  | Standard MIDI File (format 0)       | Machine-readable music interchange             |
| `<prefix>.wav`  | 16-bit PCM WAV                      | Synthesised audio                              |
| `<prefix>.mp3`  | MP3                                 | Audio as MP3 (requires `ffmpeg`)               |
| `<prefix>.mp4`  | H.264 + AAC video                   | Animated board synced to the music (`-video`, requires `ffmpeg`) |

ABC notation and Standard MIDI Files are the two most widely used standards for
representing notes as text/data, so the rendering can be opened, viewed,
played, or converted in almost any music tool.

## Requirements

- Go 1.24+
- [`ffmpeg`](https://ffmpeg.org) on your `PATH` for MP3 and MP4 output (optional
  for audio — the WAV file is always written and MP3 is skipped with a message if
  `ffmpeg` is missing; required for the animated video).
- [Bun](https://bun.sh) to build the web frontend.
- (Optional) Docker + PostgreSQL for the saved-game library. The server runs
  fine without it — the library features are simply disabled.
- (Optional) A [Render](https://render.com) + [Supabase](https://supabase.com)
  account to publish the app online — see
  [Deploying to the web](#deploying-to-the-web-render--supabase).

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

# Also render an animated Chess.com-style board video:
go run ./cmd/chess2music -in testdata/sample.pgn -out game -video -view chesscom
```

### Flags

| Flag           | Default   | Description                                                    |
| -------------- | --------- | ------------------------------------------------------------- |
| `-in`          | stdin     | Input PGN file path                                           |
| `-out`         | `game`    | Output file prefix                                            |
| `-tempo`       | `120`     | Playback tempo in quarter-note beats per minute               |
| `-base-octave` | `4`       | Base octave for White's pitches (octave 4 contains middle C)  |
| `-scale`       | `major-pentatonic` | Melody scale: `major-pentatonic`, `minor-pentatonic`, `major`, `minor` or `dorian` |
| `-key`         | `C`       | Musical key (tonic): a note name like `C` or `F#`, or `auto` to derive one from the game |
| `-no-audio`    | `false`   | Skip WAV/MP3 rendering (only write `.abc` and `.mid`)         |
| `-video`       | `false`   | Also render an animated board video (`<prefix>.mp4`, needs `ffmpeg`) |
| `-view`        | `lichess` | Board view for the video: `lichess` or `chesscom`             |

Only the first game in a PGN file is rendered.

## Web UI

A small Svelte 5 frontend (built with [Bun](https://bun.sh)) lets you paste or
upload a game, customise the column instruments and per-piece rhythms, and pick
the output: either an **MP3** to play/download, or an **animated MP4** of the
board in a Lichess or Chess.com view, synced to the music.

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

| Endpoint          | Method | Description                                                                                  |
| ----------------- | ------ | -------------------------------------------------------------------------------------------- |
| `/api/options`    | GET    | Lists pieces, instruments, scales, keys, files, rhythm patterns, the default file→instrument and piece→rhythm maps, and the available board views |
| `/api/generate`   | POST   | JSON `{pgn, tempo, baseOctave, scale, key, fileInstruments, rhythms, format, boardTheme}` → MP3/WAV audio or MP4 video |
| `/api/games`      | GET    | Lists saved games (built-in library first), without PGN bodies                               |
| `/api/games/{id}` | GET    | Returns one saved game including its PGN                                                      |
| `/api/games`      | POST   | JSON `{title, pgn, boardTheme}` → saves a game and returns it                                 |

Set `format` to `mp4` (with `boardTheme` of `lichess` or `chesscom`) to get the
animated board video; any other value returns audio.

## Deploying to the web (Render + Supabase)

Running locally with Docker Compose stays the default for development and
testing. To publish the app, host the container on [Render](https://render.com)
and use [Supabase](https://supabase.com) for the Postgres game library. Nothing
in the local workflow changes — both are driven entirely by environment
variables (`DATABASE_URL`, `DB_POOLER`, and Render's injected `PORT`).

**1. Create the Supabase database**

1. Create a new project at [supabase.com](https://supabase.com) and pick a
   database password.
2. In the dashboard go to **Project Settings → Database → Connection string**
   and copy the **Transaction pooler** URI (host ends in
   `…pooler.supabase.com`, port `6543`). It looks like:

   ```
   postgresql://postgres.<ref>:<password>@aws-0-<region>.pooler.supabase.com:6543/postgres?sslmode=require
   ```

   Replace `<password>` with your database password. Keep `sslmode=require`.
3. No manual SQL is needed: the server creates its schema and seeds the
   built-in games automatically on first start.

**2. Deploy the web service on Render**

- **Blueprint (one click):** the repo ships a [`render.yaml`](render.yaml).
  In Render choose **New + → Blueprint**, connect this repository, and Render
  reads the service definition. You'll be prompted for `DATABASE_URL`; paste the
  Supabase pooler URI from step 1.
- **Manual:** create a **New + → Web Service**, connect the repo, and select
  **Docker** as the runtime (the [`Dockerfile`](Dockerfile) bundles ffmpeg and
  the built frontend). Then add the environment variables below.

| Variable       | Value                                              | Notes                                                              |
| -------------- | -------------------------------------------------- | ------------------------------------------------------------------ |
| `DATABASE_URL` | the Supabase **transaction pooler** URI            | Mark it secret; required for the game library                      |
| `DB_POOLER`    | `true`                                             | Disables prepared statements so pgx works through the pooler       |
| `PORT`         | _(set automatically by Render)_                    | The server binds to `$PORT`; no `-addr` flag is needed             |

Render uses `/api/options` as the health check. The free plan works but cold
starts; the container needs ~512 MB to render video comfortably.

> The game library is optional. If you omit `DATABASE_URL` the app still runs and
> generates audio/video — it just hides the saved-games dropdown.

## How moves become music

- **Sound mapping** — every move is spread across pitch, timbre and rhythm so
  it is easy to tell apart by ear:
  - the **row** (1–8) sets an in-scale pitch within about one hummable octave;
  - the **column** (a–h) chooses the instrument — by default eight deliberately
    very different voices (tuba, jaw harp, organ, horn, pizzicato viola, piano,
    bass guitar, xylophone) so an untrained ear can still hear which file a move
    landed on, and each file's instrument can be remapped via the web UI or the
    API;
  - the **piece type** plays a recognisable rhythmic figure inside the bar (by
    default the pawn marches in even quarters, the king strides in two halves,
    the rook/knight/bishop place a long note at the middle/front/back of the
    bar, and the queen leans with an off-beat syncopation). Each piece's rhythm
    can be remapped to any of the named patterns (`march`, `accent-front`,
    `accent-middle`, `accent-back`, `stride`, `syncopated`, `gallop`, `held`).
  Splitting the move across three easy-to-tell channels makes games and openings
  memorable by ear.
- **Key & scale** — pick a `scale` (major/minor pentatonic, major, minor or
  dorian) and a `key`. The default scale is **major pentatonic**, which keeps
  even chaotic games sounding pleasant. The key defaults to **C** so a given
  row always sounds the same across games, which is what makes them
  learnable; set `key` to `auto` to instead derive a per-game key
  deterministically from the players, event and opening.
- **Meter** — moves are laid out on a steady beat, one move per bar of common
  time, and the first beat of each bar is accented, giving the music a clear,
  foot-tapping pulse.
- **Opening hook** — the piece opens with a short motif chosen from the game's
  opening (a curated melody for ~36 well-known openings, or a deterministic one
  derived from the moves otherwise). The same opening always yields the same
  hook, so you can learn to recognise an opening — or a whole game — by its tune.
- **Chorus & form** — that hook returns at the end as a closing chorus and
  resolves on a held tonic, giving the music an ABA song form: intro hook, the
  game, then the hook again.
- **Accompaniment** — a bass line and a sustained chord pad play underneath the
  melody. Each bar's chord follows the melody note on its downbeat, so the
  harmony is always in key and gives the tune a song-like backing.
- **Dynamics** — captures, checks and checkmate get progressively louder
  accents.
- **Effects** — special moves add a sound on top of the note: captures get a
  sharp drumstick strike, checkmate a deep drum hit, and castling a short drum
  roll that builds into the move. (Checks are not given an extra sound — they
  only play louder.)
- **Players** — White plays in the base register; Black plays a fifth higher.
- **Castling** — rendered as an in-key triad (a small "fanfare") under the drum
  roll.
- **Promotion** — the promoted pawn jumps up an octave.

## Animated video

Choosing the **MP4** output (the web UI's *Output* section, or `-video` on the
CLI) replays the game on a real 8×8 board and animates each piece sliding from
its source square to its destination in time with the music. The last move is
highlighted, and you can pick between two board views:

- **Lichess** — the classic brown/cream board.
- **Chess.com** — the green/ivory board.

The move source squares (which PGN/SAN does not state explicitly) are
reconstructed by replaying the game, including disambiguation, pins, castling,
en passant and promotion. Pieces are drawn from the Unicode chess glyphs of an
embedded font, and frames are muxed with the audio by `ffmpeg` (H.264 + AAC).

## Project layout

```
cmd/chess2music     command-line entry point
cmd/server          HTTP server + JSON API for the web UI
internal/pgn        PGN/SAN parser
internal/board      replays a game to reconstruct every board position
internal/music      move-to-note mapping, ABC and MIDI writers
internal/audio      WAV synthesis and MP3 conversion
internal/render     board/piece image rendering (Lichess & Chess.com views)
internal/video      animated MP4 generation (frames + ffmpeg)
internal/db         PostgreSQL game library (pgx) + seed games
web                 Svelte 5 frontend (built with Bun), embedded by the server
Dockerfile          multi-stage build (Bun + Go) producing the runtime image
docker-compose.yml  app + Postgres stack for the game library
Makefile            common build/run/test/docker tasks
testdata            a sample PGN (Anderssen's "Immortal Game")
```

The core CLI has **no external Go dependencies** for the music itself — the PGN
parser, ABC writer, MIDI writer and audio synthesiser are all self-contained.
The board video uses `golang.org/x/image` for font rendering, and the optional
game library uses the `pgx` PostgreSQL driver.
