import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// Tanpa globals, Testing Library tidak mendaftarkan pembersihan otomatis.
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.resetAllMocks();
});
