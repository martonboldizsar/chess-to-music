<script>
    import { Chess } from "chess.js";
    import Board from "./Board.svelte";

    // Practice receives the live sound settings so the notes the player hears
    // match exactly what a real game would render. soundConfig is { tempo,
    // baseOctave, scale, key, fileInstruments, rhythms }.
    let {
        soundConfig,
        pieces = [],
        prettyPiece = (p) => p,
        prettyFile = (f) => f,
        prettyInstrument = (i) => i,
        prettyRhythm = (r) => r,
        fileInstruments = {},
        pieceRhythms = {},
        boardThemes = [],
    } = $props();

    const files = ["a", "b", "c", "d", "e", "f", "g", "h"];

    // chess.js uses single letters; map them to/from the API's piece names.
    const charToName = {
        p: "pawn",
        n: "knight",
        b: "bishop",
        r: "rook",
        q: "queen",
        k: "king",
    };
    const nameToChar = {
        pawn: "p",
        knight: "n",
        bishop: "b",
        rook: "r",
        queen: "q",
        king: "k",
    };

    let mode = $state("listen"); // "listen" (random game) | "piece" (one piece)
    let chosenPiece = $state("knight");
    let chosenColor = $state("white");
    let theme = $state("lichess"); // board colours: "lichess" | "chesscom"

    let started = $state(false);
    let position = $state({}); // square -> { type, color }
    let target = $state(null); // { square, pieceName, color, char }
    let selected = $state("");
    let legalTargets = $state([]);
    let lastFrom = $state("");
    let lastTo = $state("");
    let wrongSquare = $state("");
    let feedback = $state("");
    let feedbackKind = $state(""); // "good" | "bad" | ""
    let revealed = $state(false);
    let hintOn = $state(false);
    let showKey = $state(false);

    let correct = $state(0);
    let total = $state(0);
    const accuracy = $derived(
        total === 0 ? 0 : Math.round((correct / total) * 100),
    );

    // chess.js game for the "listen" (random position) mode.
    let game = null;

    // A single reusable <audio> element; cache rendered notes by request key so
    // replaying a note is instant and doesn't re-hit the server.
    let noteAudio;
    const noteCache = new Map();
    let loadingNote = $state(false);
    let audioError = $state("");

    const hintTargets = $derived(
        target && (hintOn || revealed) ? [target.square] : [],
    );

    function sq(fileIdx, rank) {
        return `${files[fileIdx]}${rank}`;
    }
    function fileIdxOf(square) {
        return square.charCodeAt(0) - 97;
    }
    function rankOf(square) {
        return Number(square[1]);
    }

    // --- Audio ---------------------------------------------------------------

    async function playNoteFor(square, pieceName, color) {
        audioError = "";
        const body = {
            file: square[0],
            rank: rankOf(square),
            piece: pieceName,
            color,
            tempo: soundConfig.tempo,
            baseOctave: soundConfig.baseOctave,
            scale: soundConfig.scale,
            key: soundConfig.key,
            fileInstruments: soundConfig.fileInstruments,
            rhythms: soundConfig.rhythms,
        };
        const cacheKey = JSON.stringify(body);
        if (!noteAudio) noteAudio = new Audio();
        try {
            let url = noteCache.get(cacheKey);
            if (!url) {
                loadingNote = true;
                const res = await fetch("/api/note", {
                    method: "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify(body),
                });
                if (!res.ok)
                    throw new Error(`note request failed (${res.status})`);
                const blob = await res.blob();
                url = URL.createObjectURL(blob);
                noteCache.set(cacheKey, url);
            }
            noteAudio.src = url;
            await noteAudio.play();
        } catch (e) {
            // Rapidly starting a new note interrupts the previous play() with an
            // AbortError; that's expected, not a real failure to surface.
            if (e.name !== "AbortError") {
                audioError = `Could not play the note: ${e.message}`;
            }
        } finally {
            loadingNote = false;
        }
    }

    function replay() {
        if (target) playNoteFor(target.square, target.pieceName, target.color);
    }

    // --- Lone-piece move generation (mode "piece", empty board) --------------

    function pieceTargets(char, color, square) {
        const f = fileIdxOf(square);
        const r = rankOf(square);
        const out = [];
        const add = (nf, nr) => {
            if (nf >= 0 && nf < 8 && nr >= 1 && nr <= 8) out.push(sq(nf, nr));
        };
        const ray = (df, dr) => {
            let nf = f + df,
                nr = r + dr;
            while (nf >= 0 && nf < 8 && nr >= 1 && nr <= 8) {
                out.push(sq(nf, nr));
                nf += df;
                nr += dr;
            }
        };
        switch (char) {
            case "n":
                [
                    [1, 2],
                    [2, 1],
                    [2, -1],
                    [1, -2],
                    [-1, -2],
                    [-2, -1],
                    [-2, 1],
                    [-1, 2],
                ].forEach(([df, dr]) => add(f + df, r + dr));
                break;
            case "b":
                ray(1, 1);
                ray(1, -1);
                ray(-1, 1);
                ray(-1, -1);
                break;
            case "r":
                ray(1, 0);
                ray(-1, 0);
                ray(0, 1);
                ray(0, -1);
                break;
            case "q":
                ray(1, 0);
                ray(-1, 0);
                ray(0, 1);
                ray(0, -1);
                ray(1, 1);
                ray(1, -1);
                ray(-1, 1);
                ray(-1, -1);
                break;
            case "k":
                [
                    [1, 0],
                    [-1, 0],
                    [0, 1],
                    [0, -1],
                    [1, 1],
                    [1, -1],
                    [-1, 1],
                    [-1, -1],
                ].forEach(([df, dr]) => add(f + df, r + dr));
                break;
            case "p": {
                const dir = color === "w" ? 1 : -1;
                const startRank = color === "w" ? 2 : 7;
                add(f, r + dir);
                if (r === startRank) add(f, r + 2 * dir);
                break;
            }
        }
        return out;
    }

    function randomFrom(arr) {
        return arr[Math.floor(Math.random() * arr.length)];
    }

    // --- Round setup ---------------------------------------------------------

    function syncPositionFromGame() {
        const next = {};
        for (const row of game.board()) {
            for (const cell of row) {
                if (cell)
                    next[cell.square] = { type: cell.type, color: cell.color };
            }
        }
        position = next;
    }

    function newListenPosition() {
        game = new Chess();
        const plies = 8 + Math.floor(Math.random() * 16);
        for (let i = 0; i < plies; i++) {
            const moves = game.moves({ verbose: true });
            if (!moves.length || game.isGameOver()) break;
            game.move(randomFrom(moves));
        }
        syncPositionFromGame();
    }

    function nextListenTarget() {
        if (!game || game.isGameOver() || game.moves().length === 0) {
            newListenPosition();
        }
        const moves = game.moves({ verbose: true });
        if (!moves.length) {
            newListenPosition();
            return nextListenTarget();
        }
        const m = randomFrom(moves);
        target = {
            square: m.to,
            pieceName: charToName[m.piece],
            color: m.color === "w" ? "white" : "black",
            char: m.piece,
        };
    }

    function placePracticePiece() {
        const char = nameToChar[chosenPiece];
        const color = chosenColor === "white" ? "w" : "b";
        let square;
        // Keep trying until the piece has at least one legal target.
        let guard = 0;
        do {
            const f = Math.floor(Math.random() * 8);
            let r;
            if (char === "p") {
                // Pawns need room ahead; keep them off the back ranks.
                r =
                    color === "w"
                        ? 2 + Math.floor(Math.random() * 5)
                        : 3 + Math.floor(Math.random() * 5);
            } else {
                r = 1 + Math.floor(Math.random() * 8);
            }
            square = sq(f, r);
            guard++;
        } while (pieceTargets(char, color, square).length === 0 && guard < 50);
        position = { [square]: { type: char, color } };
        return square;
    }

    function nextPieceTarget(fromSquare) {
        const char = nameToChar[chosenPiece];
        const color = chosenColor === "white" ? "w" : "b";
        let targets = pieceTargets(char, color, fromSquare);
        if (!targets.length) {
            fromSquare = placePracticePiece();
            targets = pieceTargets(char, color, fromSquare);
        }
        const dest = randomFrom(targets);
        target = {
            square: dest,
            pieceName: chosenPiece,
            color: chosenColor,
            char,
        };
    }

    function startRound() {
        started = true;
        correct = 0;
        total = 0;
        beginNext(true);
    }

    // Reset the running score without disturbing the current round.
    function resetScore() {
        correct = 0;
        total = 0;
    }

    function beginNext(fresh = false) {
        selected = "";
        legalTargets = [];
        lastFrom = "";
        lastTo = "";
        wrongSquare = "";
        feedback = "";
        feedbackKind = "";
        revealed = false;
        if (mode === "listen") {
            if (fresh) newListenPosition();
            nextListenTarget();
        } else {
            if (fresh || Object.keys(position).length === 0) {
                const from = placePracticePiece();
                nextPieceTarget(from);
            } else {
                const from = Object.keys(position)[0];
                nextPieceTarget(from);
            }
        }
        replay();
    }

    function skip() {
        beginNext(mode === "piece");
    }

    function reveal() {
        revealed = true;
        feedback = `${prettyPiece(target.pieceName)} → ${target.square.toUpperCase()}`;
        feedbackKind = "";
    }

    // --- Interaction ---------------------------------------------------------

    function selectablePieceAt(square) {
        const p = position[square];
        if (!p) return false;
        if (mode === "listen") return p.color === game.turn();
        return true; // only one piece exists in piece mode
    }

    function targetsForSelection(square) {
        if (mode === "listen") {
            return game.moves({ square, verbose: true }).map((m) => m.to);
        }
        const p = position[square];
        return pieceTargets(p.type, p.color, square);
    }

    function onSquareClick(square) {
        if (!started || !target) return;
        if (selected && square === selected) {
            selected = "";
            legalTargets = [];
            return;
        }
        if (selected && legalTargets.includes(square)) {
            attemptMove(selected, square);
            return;
        }
        if (selectablePieceAt(square)) {
            selected = square;
            legalTargets = targetsForSelection(square);
        } else {
            selected = "";
            legalTargets = [];
        }
    }

    function flashWrong(square) {
        wrongSquare = square;
        setTimeout(() => {
            if (wrongSquare === square) wrongSquare = "";
        }, 450);
    }

    function attemptMove(from, to) {
        if (!target) return;
        selected = "";
        legalTargets = [];

        if (mode === "listen") {
            const legal = game
                .moves({ square: from, verbose: true })
                .find((m) => m.to === to);
            total += 1;
            if (!legal) {
                feedback = "Not a legal move there.";
                feedbackKind = "bad";
                flashWrong(to);
                return;
            }
            const matches =
                legal.piece === target.char && legal.to === target.square;
            if (matches) {
                game.move({ from, to, promotion: "q" });
                syncPositionFromGame();
                correct += 1;
                lastFrom = from;
                lastTo = to;
                feedback = "Correct!";
                feedbackKind = "good";
                nextListenTargetAfterDelay();
            } else {
                feedback = "Right move type? Listen again.";
                feedbackKind = "bad";
                flashWrong(to);
            }
            return;
        }

        // piece mode
        const char = nameToChar[chosenPiece];
        const color = chosenColor === "white" ? "w" : "b";
        const targets = pieceTargets(char, color, from);
        total += 1;
        if (!targets.includes(to)) {
            feedback = "That piece can't go there.";
            feedbackKind = "bad";
            flashWrong(to);
            return;
        }
        if (to === target.square) {
            correct += 1;
            position = { [to]: { type: char, color } };
            lastFrom = from;
            lastTo = to;
            feedback = "Correct!";
            feedbackKind = "good";
            nextPieceTargetAfterDelay(to);
        } else {
            feedback = "Not the square the note named.";
            feedbackKind = "bad";
            flashWrong(to);
        }
    }

    function nextListenTargetAfterDelay() {
        setTimeout(() => {
            selected = "";
            legalTargets = [];
            wrongSquare = "";
            revealed = false;
            lastFrom = "";
            lastTo = "";
            feedback = "";
            feedbackKind = "";
            nextListenTarget();
            replay();
        }, 650);
    }

    function nextPieceTargetAfterDelay(from) {
        setTimeout(() => {
            selected = "";
            legalTargets = [];
            wrongSquare = "";
            revealed = false;
            lastFrom = "";
            lastTo = "";
            feedback = "";
            feedbackKind = "";
            nextPieceTarget(from);
            replay();
        }, 650);
    }

    function onMove(from, to) {
        if (!started || !target) return;
        attemptMove(from, to);
    }

    function switchMode(next) {
        if (mode === next) return;
        mode = next;
        started = false;
        target = null;
        position = {};
        selected = "";
        legalTargets = [];
        feedback = "";
        feedbackKind = "";
        // The target-square hint only exists in listen mode; clear it so it
        // can't linger and auto-reveal the answer after switching to piece mode.
        hintOn = false;
        revealed = false;
    }

    // Changing the piece or side mid-practice restarts the round with the new
    // choice so the board immediately reflects the selection.
    function onPickerChange() {
        if (mode === "piece" && started) beginNext(true);
    }
</script>

<div class="practice">
    <div class="mode-switch">
        <button
            type="button"
            class:active={mode === "listen"}
            onclick={() => switchMode("listen")}
        >
            Listen &amp; move
        </button>
        <button
            type="button"
            class:active={mode === "piece"}
            onclick={() => switchMode("piece")}
        >
            Single piece
        </button>
    </div>

    {#if mode === "listen"}
        <p class="hint">
            A random position appears and a note plays. Decode it — the
            <strong>pitch</strong> is the rank, the <strong>instrument</strong>
            the file, the <strong>rhythm</strong> the piece — then move a matching
            piece to the named square.
        </p>
    {:else}
        <p class="hint">
            One piece roams an empty board. Each note names a square it can
            legally reach — move it there, then the next note plays from its new
            home.
        </p>
        <div class="piece-pickers">
            <div class="field">
                <label for="practice-piece">Piece</label>
                <select
                    id="practice-piece"
                    bind:value={chosenPiece}
                    onchange={onPickerChange}
                >
                    {#each pieces as p (p)}
                        <option value={p}>{prettyPiece(p)}</option>
                    {/each}
                </select>
            </div>
            <div class="field">
                <label for="practice-color">Side</label>
                <select
                    id="practice-color"
                    bind:value={chosenColor}
                    onchange={onPickerChange}
                >
                    <option value="white">White</option>
                    <option value="black">Black</option>
                </select>
            </div>
        </div>
    {/if}

    <div class="board-style">
        <label for="practice-theme">Board style</label>
        <select id="practice-theme" bind:value={theme}>
            {#if boardThemes.length}
                {#each boardThemes as t (t.name)}
                    <option value={t.name}>{t.label}</option>
                {/each}
            {:else}
                <option value="lichess">Lichess</option>
                <option value="chesscom">Chess.com</option>
            {/if}
        </select>
    </div>

    <div class="scoreboard">
        <span class="score-num"
            >{correct}<span class="score-den">/{total}</span></span
        >
        <span class="score-acc">{accuracy}% accurate</span>
        <button
            type="button"
            class="score-reset"
            onclick={resetScore}
            disabled={total === 0}
            title="Reset the score"
        >
            Reset
        </button>
    </div>

    <Board
        {position}
        {selected}
        {legalTargets}
        {hintTargets}
        {lastFrom}
        {lastTo}
        {wrongSquare}
        {theme}
        orientation={mode === "piece" && chosenColor === "black"
            ? "black"
            : "white"}
        interactive={started}
        {onSquareClick}
        {onMove}
    />

    {#if feedback}
        <p class="feedback {feedbackKind}">{feedback}</p>
    {/if}
    {#if audioError}
        <p class="error" style="margin-top:0.6rem">{audioError}</p>
    {/if}

    <div class="practice-actions">
        {#if !started}
            <button class="btn-primary" onclick={startRound}
                >Start practising</button
            >
        {:else}
            <button class="btn-ghost" onclick={replay} disabled={loadingNote}>
                {#if loadingNote}<span class="spinner"></span>{/if}▶ Replay
                note
            </button>
            <button class="btn-ghost" onclick={reveal}>Show answer</button>
            <button class="btn-ghost" onclick={skip}>Skip</button>
            {#if mode === "listen"}
                <label class="hint-toggle">
                    <input type="checkbox" bind:checked={hintOn} />
                    Highlight target square
                </label>
            {/if}
        {/if}
    </div>

    <button
        type="button"
        class="key-toggle"
        onclick={() => (showKey = !showKey)}
    >
        {showKey ? "▾" : "▸"} Sound key
    </button>
    {#if showKey}
        <div class="sound-key">
            <div>
                <h4>Files → instruments</h4>
                <ul>
                    {#each files as f (f)}
                        <li>
                            <span class="k-file">{f.toUpperCase()}</span>
                            {prettyInstrument(fileInstruments[f] ?? "")}
                        </li>
                    {/each}
                </ul>
            </div>
            <div>
                <h4>Pieces → rhythms</h4>
                <ul>
                    {#each pieces as p (p)}
                        <li>
                            <span class="k-piece">{prettyPiece(p)}</span>
                            {prettyRhythm(pieceRhythms[p] ?? "")}
                        </li>
                    {/each}
                </ul>
            </div>
        </div>
    {/if}
</div>
