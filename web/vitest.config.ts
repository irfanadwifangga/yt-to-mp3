import { defineConfig, mergeConfig } from "vitest/config";
import viteConfig from "./vite.config";

// Test memakai konfigurasi Vite yang sama dengan build, supaya transform
// JSX dan resolusi modul tidak menyimpang dari yang dikirim ke pengguna.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: "jsdom",
      include: ["src/**/*.test.{ts,tsx}"],
      setupFiles: ["./src/test/setup.ts"],
    },
  }),
);
