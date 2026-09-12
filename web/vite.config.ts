import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// Port dev server Go. Harus sama dengan target `make dev-api`, karena port
// acak produksi tidak bisa ditebak oleh proxy.
const GO_DEV_PORT = 8799;

/**
 * Menulis ulang web/dist/.gitkeep setelah build.
 *
 * File itu satu-satunya isi dist yang di-commit, dan keberadaannya wajib:
 * tanpa dist, `//go:embed all:dist` gagal compile pada clone yang masih
 * bersih. Karena `emptyOutDir` mengosongkan direktori setiap build, penanda
 * ini harus dipulihkan setelahnya.
 */
function keepDistTracked(): Plugin {
  return {
    name: "keep-dist-tracked",
    apply: "build",
    closeBundle() {
      writeFileSync(resolve(import.meta.dirname, "dist/.gitkeep"), "");
    },
  };
}

export default defineConfig({
  plugins: [react(), keepDistTracked()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: `http://127.0.0.1:${GO_DEV_PORT}`,
        changeOrigin: false,
      },
    },
  },
});
