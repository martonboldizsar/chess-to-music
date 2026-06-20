<script>
    // A reusable 8x8 chess board. It renders a position and emits user intent
    // (click a square, or drag a piece from one square to another); all game
    // logic lives in the parent so the board stays a dumb, presentational view.
    //
    // position:     { [square]: { type, color } } where square is "e4",
    //               type is one of p,n,b,r,q,k and color is "w"|"b".
    // selected:     the currently picked-up square ("" when none).
    // legalTargets: squares the selected piece may move to (shown as dots).
    // hintTargets:  squares to softly highlight as a hint.
    // lastFrom/lastTo: the most recent move, lightly highlighted.
    // wrongSquare:  a square to flash when the player guesses wrong.
    let {
        position = {},
        selected = "",
        legalTargets = [],
        hintTargets = [],
        lastFrom = "",
        lastTo = "",
        wrongSquare = "",
        interactive = true,
        theme = "lichess",
        orientation = "white",
        onSquareClick = () => {},
        onMove = () => {},
    } = $props();

    const files = ["a", "b", "c", "d", "e", "f", "g", "h"];
    // Ranks from 8 (top) down to 1 (bottom): White is at the bottom.
    const ranks = [8, 7, 6, 5, 4, 3, 2, 1];

    // Display order respects the viewer's side: from Black's perspective the
    // board is rotated 180° (files h..a left-to-right, ranks 1..8 top-to-bottom).
    const displayFiles = $derived(
        orientation === "black" ? [...files].reverse() : files,
    );
    const displayRanks = $derived(
        orientation === "black" ? [...ranks].reverse() : ranks,
    );

    // Solid chess glyphs are used for BOTH colours; white vs black is conveyed
    // by the fill/outline colours (per theme), matching how the server-side
    // video renderer draws the board.
    const glyphs = { p: "♟", n: "♞", b: "♝", r: "♜", q: "♛", k: "♚" };

    let dragFrom = $state("");
    let dragGhost = null;

    const legalSet = $derived(new Set(legalTargets));
    const hintSet = $derived(new Set(hintTargets));

    function pieceAt(sq) {
        return position[sq] ?? null;
    }

    function isDark(file, rank) {
        // a1 (file 0, rank 1) is dark.
        return (file + rank) % 2 === 0;
    }

    function handleClick(sq) {
        if (!interactive) return;
        onSquareClick(sq);
    }

    function handleDragStart(e, sq) {
        if (!interactive || !pieceAt(sq)) {
            e.preventDefault();
            return;
        }
        dragFrom = sq;
        e.dataTransfer.effectAllowed = "move";
        try {
            e.dataTransfer.setData("text/plain", sq);
            // Build a detached drag image holding ONLY the piece glyph (no
            // square background), so nothing of the board travels with the
            // cursor. Computed styles are copied because the clone, once moved
            // to <body>, loses the board's CSS custom properties.
            const glyph = e.currentTarget.querySelector(".piece");
            if (glyph) {
                const cs = getComputedStyle(glyph);
                const ghost = document.createElement("div");
                ghost.textContent = glyph.textContent;
                ghost.className = "drag-ghost";
                ghost.style.fontSize = cs.fontSize;
                ghost.style.color = cs.color;
                ghost.style.webkitTextStroke = cs.webkitTextStroke;
                document.body.appendChild(ghost);
                dragGhost = ghost;
                const r = glyph.getBoundingClientRect();
                e.dataTransfer.setDragImage(ghost, r.width / 2, r.height / 2);
            }
        } catch {
            /* ignore */
        }
    }

    function clearGhost() {
        if (dragGhost) {
            dragGhost.remove();
            dragGhost = null;
        }
    }

    function handleDragEnd() {
        dragFrom = "";
        clearGhost();
    }

    function handleDrop(e, sq) {
        if (!interactive) return;
        e.preventDefault();
        const from = dragFrom || e.dataTransfer.getData("text/plain");
        dragFrom = "";
        clearGhost();
        if (from && from !== sq) onMove(from, sq);
    }

    function handleDragOver(e) {
        if (!interactive) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
    }
</script>

<div class="board theme-{theme}" class:locked={!interactive}>
    {#each displayRanks as rank, ri (rank)}
        {#each displayFiles as file, fi (file)}
            {@const sq = `${file}${rank}`}
            {@const piece = pieceAt(sq)}
            <button
                type="button"
                class="square"
                class:dark={isDark(files.indexOf(file), rank)}
                class:selected={sq === selected}
                class:target={legalSet.has(sq)}
                class:hint={hintSet.has(sq)}
                class:last={sq === lastFrom || sq === lastTo}
                class:wrong={sq === wrongSquare}
                draggable={interactive && !!piece}
                onclick={() => handleClick(sq)}
                ondragstart={(e) => handleDragStart(e, sq)}
                ondragend={handleDragEnd}
                ondragover={handleDragOver}
                ondrop={(e) => handleDrop(e, sq)}
                aria-label={piece
                    ? `${sq}, ${piece.color === "w" ? "white" : "black"} ${piece.type}`
                    : sq}
            >
                {#if piece}
                    <span class="piece" class:white={piece.color === "w"}>
                        {glyphs[piece.type]}
                    </span>
                {/if}
                {#if legalSet.has(sq) && !piece}
                    <span class="dot" aria-hidden="true"></span>
                {/if}
                {#if fi === 0}
                    <span class="coord rank" aria-hidden="true">{rank}</span>
                {/if}
                {#if ri === displayRanks.length - 1}
                    <span class="coord file" aria-hidden="true">{file}</span>
                {/if}
            </button>
        {/each}
    {/each}
</div>
