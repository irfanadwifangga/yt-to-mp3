import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@fontsource-variable/archivo/wdth.css";
import "@fontsource-variable/jetbrains-mono/wght.css";
import { App } from "./App";
import { applyTheme, loadTheme } from "./theme";
import "./index.css";

// Tema dipasang sebelum render pertama supaya halaman tidak berkedip dari
// terang ke gelap.
applyTheme(loadTheme());

const container = document.getElementById("root");
if (!container) throw new Error("elemen #root tidak ditemukan");

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>
);
