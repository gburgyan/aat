import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

export default defineConfig({
  plugins: [svelte()],
  build: {
    // dist/placeholder.txt is tracked for go:embed; the bundle goes beside it.
    outDir: "dist/app",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:9119",
      "/health": "http://localhost:9119",
    },
  },
});
