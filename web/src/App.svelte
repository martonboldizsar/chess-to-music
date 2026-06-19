<script>
    // Chess-to-Music web UI (Svelte 5, runes mode).
    //
    // The user pastes or uploads a PGN game, optionally remaps which piece plays
    // which instrument, then generates audio they can play here or download.

    const SAMPLE_PGN = `[Event "Immortal Game"]
[White "Adolf Anderssen"]
[Black "Lionel Kieseritzky"]
[Result "1-0"]

1. e4 e5 2. f4 exf4 3. Bc4 Qh4+ 4. Kf1 b5 5. Bxb5 Nf6 6. Nf3 Qh6
7. d3 Nh5 8. Nh4 Qg5 9. Nf5 c6 10. g4 Nf6 11. Rg1 cxb5 12. h4 Qg6
13. h5 Qg5 14. Qf3 Ng8 15. Bxf4 Qf6 16. Nc3 Bc5 17. Nd5 Qxb2 18. Bd6 Bxg1
19. e5 Qxa1+ 20. Ke2 Na6 21. Nxg7+ Kd8 22. Qf6+ Nxf6 23. Be7# 1-0`;

    // Reactive state (Svelte 5 runes).
    let pgn = $state(SAMPLE_PGN);
    let tempo = $state(120);
    let baseOctave = $state(4);

    let pieces = $state([]);
    let instruments = $state([]);
    let mapping = $state({}); // piece -> instrument
    let hasMp3 = $state(true);

    let loading = $state(false);
    let error = $state("");
    let audioUrl = $state("");
    let audioType = $state("audio/mpeg");
    let fileInput;

    // Game library (PostgreSQL-backed).
    let games = $state([]); // list of saved games (summaries)
    let selectedGameId = $state(""); // currently picked library game
    let libraryAvailable = $state(true);
    let saveTitle = $state("");
    let saving = $state(false);
    let saveMessage = $state("");

    const prettyPiece = (p) => p.charAt(0).toUpperCase() + p.slice(1);
    const prettyInstrument = (i) => i.charAt(0).toUpperCase() + i.slice(1);

    const gameLabel = (g) => {
        const players =
            g.white || g.black ? `${g.white || "?"} vs ${g.black || "?"}` : "";
        return players && players !== g.title
            ? `${g.title} — ${players}`
            : g.title;
    };

    // Load the available pieces/instruments and default mapping from the API.
    async function loadOptions() {
        try {
            const res = await fetch("/api/options");
            if (!res.ok)
                throw new Error(`options request failed (${res.status})`);
            const data = await res.json();
            pieces = data.pieces;
            instruments = data.instruments;
            mapping = { ...data.defaults };
            hasMp3 = data.hasMp3;
        } catch (e) {
            error = `Could not load options: ${e.message}`;
        }
    }

    // Load the library of saved games for the dropdown.
    async function loadGames() {
        try {
            const res = await fetch("/api/games");
            if (!res.ok)
                throw new Error(`games request failed (${res.status})`);
            games = await res.json();
            libraryAvailable = true;
        } catch (e) {
            libraryAvailable = false;
        }
    }

    loadOptions();
    loadGames();

    // Load a chosen library game's PGN into the editor.
    async function onSelectGame(event) {
        const id = event.target.value;
        selectedGameId = id;
        if (!id) return;
        error = "";
        try {
            const res = await fetch(`/api/games/${id}`);
            if (!res.ok) throw new Error(`could not load game (${res.status})`);
            const game = await res.json();
            pgn = game.pgn;
        } catch (e) {
            error = e.message;
        }
    }

    // Save the current PGN to the library.
    async function saveGame() {
        error = "";
        saveMessage = "";
        saving = true;
        try {
            const res = await fetch("/api/games", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ title: saveTitle, pgn }),
            });
            if (!res.ok) {
                let msg = `Save failed (${res.status})`;
                try {
                    const body = await res.json();
                    if (body?.error) msg = body.error;
                } catch {
                    /* non-JSON error body */
                }
                throw new Error(msg);
            }
            const saved = await res.json();
            saveTitle = "";
            saveMessage = `Saved “${saved.title}” to the library.`;
            await loadGames();
            selectedGameId = String(saved.id);
        } catch (e) {
            error = e.message;
        } finally {
            saving = false;
        }
    }

    // Read an uploaded .pgn file into the textarea.
    function onFile(event) {
        const file = event.target.files?.[0];
        if (!file) return;
        const reader = new FileReader();
        reader.onload = () => {
            pgn = String(reader.result ?? "");
        };
        reader.readAsText(file);
    }

    async function generate() {
        error = "";
        loading = true;
        if (audioUrl) {
            URL.revokeObjectURL(audioUrl);
            audioUrl = "";
        }
        try {
            const res = await fetch("/api/generate", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    pgn,
                    tempo: Number(tempo),
                    baseOctave: Number(baseOctave),
                    instruments: mapping,
                }),
            });
            if (!res.ok) {
                let msg = `Generation failed (${res.status})`;
                try {
                    const body = await res.json();
                    if (body?.error) msg = body.error;
                } catch {
                    /* non-JSON error body */
                }
                throw new Error(msg);
            }
            const blob = await res.blob();
            audioType = blob.type || "audio/mpeg";
            audioUrl = URL.createObjectURL(blob);
        } catch (e) {
            error = e.message;
        } finally {
            loading = false;
        }
    }

    const downloadName = $derived(
        audioType.includes("wav") ? "chess-music.wav" : "chess-music.mp3",
    );
</script>

<div class="wrap">
    <header>
        <h1>♞ Chess to Music</h1>
        <p>
            Turn a chess game into a piece of music. Paste a PGN or upload a
            file, choose your instruments, and generate.
        </p>
    </header>

    <section class="panel">
        <h2>1. The game (PGN)</h2>

        {#if libraryAvailable}
            <div class="field" style="margin-bottom:1rem">
                <label for="library">Pick a saved game</label>
                <select
                    id="library"
                    value={selectedGameId}
                    onchange={onSelectGame}
                >
                    <option value="">— Choose from the library —</option>
                    {#each games as g (g.id)}
                        <option value={String(g.id)}>
                            {g.builtin ? "★ " : ""}{gameLabel(g)}
                        </option>
                    {/each}
                </select>
            </div>
        {/if}

        <label for="pgn">Paste or edit the game below</label>
        <textarea id="pgn" bind:value={pgn} spellcheck="false"></textarea>
        <div class="row file-row">
            <input
                type="file"
                accept=".pgn,text/plain"
                bind:this={fileInput}
                onchange={onFile}
            />
        </div>
        <p class="hint">
            Standard PGN export from chess.com or lichess works. Only the first
            game is rendered.
        </p>

        {#if libraryAvailable}
            <div class="save-row">
                <input
                    type="text"
                    class="save-title"
                    placeholder="Title (optional)"
                    bind:value={saveTitle}
                />
                <button
                    class="btn-ghost"
                    onclick={saveGame}
                    disabled={saving || !pgn.trim()}
                >
                    {#if saving}<span class="spinner"></span>Saving…{:else}Save
                        to library{/if}
                </button>
            </div>
            {#if saveMessage}
                <p class="hint" style="color:var(--accent)">{saveMessage}</p>
            {/if}
        {/if}
    </section>

    <section class="panel">
        <h2>2. Instruments</h2>
        <div class="grid">
            {#each pieces as piece (piece)}
                <div class="field">
                    <label for={`inst-${piece}`}>{prettyPiece(piece)}</label>
                    <select id={`inst-${piece}`} bind:value={mapping[piece]}>
                        {#each instruments as inst (inst)}
                            <option value={inst}
                                >{prettyInstrument(inst)}</option
                            >
                        {/each}
                    </select>
                </div>
            {/each}
        </div>

        <div class="sliders">
            <div class="field">
                <label for="tempo">Tempo: {tempo} BPM</label>
                <input
                    id="tempo"
                    type="range"
                    min="40"
                    max="240"
                    bind:value={tempo}
                />
            </div>
            <div class="field">
                <label for="octave">Base octave: {baseOctave}</label>
                <input
                    id="octave"
                    type="range"
                    min="2"
                    max="6"
                    bind:value={baseOctave}
                />
            </div>
        </div>
    </section>

    <section class="panel">
        <h2>3. Generate</h2>
        <div class="actions">
            <button
                class="btn-primary"
                onclick={generate}
                disabled={loading || !pgn.trim()}
            >
                {#if loading}<span class="spinner"
                    ></span>Generating…{:else}Generate music{/if}
            </button>
            {#if !hasMp3}
                <span class="hint"
                    >ffmpeg not found on the server — output will be WAV.</span
                >
            {/if}
        </div>

        {#if error}
            <p class="error" style="margin-top:1rem">{error}</p>
        {/if}

        {#if audioUrl}
            <div class="result" style="margin-top:1.25rem">
                <audio controls src={audioUrl}></audio>
                <a class="download" href={audioUrl} download={downloadName}>
                    <button class="btn-ghost"
                        >Download {downloadName.endsWith("mp3")
                            ? "MP3"
                            : "WAV"}</button
                    >
                </a>
            </div>
        {/if}
    </section>
</div>
