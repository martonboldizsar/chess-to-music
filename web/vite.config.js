import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

// The frontend is served by the Go server in production (it embeds web/dist).
// During development, `bun run dev` starts Vite and proxies API calls to the
// Go server running on :8080.
export default defineConfig({
    plugins: [svelte()],
    build: {
        outDir: "dist",
        emptyOutDir: true,
    },
    server: {
        proxy: {
            "/api": "http://localhost:8080",
        },
    },
});
