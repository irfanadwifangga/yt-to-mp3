// Memeriksa kelengkapan terjemahan terhadap sumber kebenaran sebenarnya.
//
// Acceptance criteria menuntut setiap error.code punya terjemahan di SPA.
// Daftar kode itu hidup di kode Go, bukan di frontend, jadi pemeriksaan
// sengaja membaca langsung dari sana: menambah kode baru di backend tanpa
// terjemahan akan menggagalkan CI, bukan menghasilkan teks mentah di layar
// pengguna. Lihat ADR-027.
//
// Yang diperiksa:
//   1. setiap kode error Go punya kunci error.<KODE> di id dan en
//   2. kunci id dan en persis sama
//   3. placeholder {nama} pada kedua bahasa cocok
//   4. setiap pemanggilan t("kunci") literal di komponen punya terjemahan

import { readdirSync, readFileSync } from "node:fs";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const web = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const root = resolve(web, "..");

const load = (p) => JSON.parse(readFileSync(resolve(web, p), "utf8"));
const locales = { id: load("src/locales/id.json"), en: load("src/locales/en.json") };

const problems = [];

// 1. Kode error dari sumber Go.
const goSources = ["internal/domain/errors.go", "internal/api/errors.go"];
const codes = new Set();
for (const src of goSources) {
  const text = readFileSync(resolve(root, src), "utf8");
  for (const m of text.matchAll(/ErrorCode\s*=\s*"([A-Z_]+)"/g)) codes.add(m[1]);
}
// Tanpa penjagaan ini, regex yang usang akan membuat pemeriksaan lolos
// diam-diam karena tidak ada satu kode pun yang diperiksa.
if (codes.size === 0) {
  problems.push("tidak ada kode error terbaca dari sumber Go; pola pencarian mungkin usang");
}
for (const [name, dict] of Object.entries(locales)) {
  for (const code of codes) {
    if (!(`error.${code}` in dict)) problems.push(`${name}: error.${code} belum diterjemahkan`);
  }
}

// 2. Kesetaraan kunci.
const idKeys = Object.keys(locales.id);
const enKeys = Object.keys(locales.en);
for (const k of idKeys) if (!(k in locales.en)) problems.push(`en: kunci ${k} hilang`);
for (const k of enKeys) if (!(k in locales.id)) problems.push(`id: kunci ${k} hilang`);

// 3. Placeholder.
const placeholders = (s) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort().join(",");
for (const k of idKeys) {
  if (k in locales.en && placeholders(locales.id[k]) !== placeholders(locales.en[k])) {
    problems.push(`placeholder berbeda pada ${k}`);
  }
}

// 4. Kunci literal yang dipakai komponen. Kunci dinamis berbentuk template
//    literal tidak terjangkau di sini dan dijaga oleh fallback has().
const srcDir = resolve(web, "src");
const files = readdirSync(srcDir, { recursive: true })
  .map(String)
  .filter((f) => [".ts", ".tsx"].includes(extname(f)));
let usages = 0;
for (const file of files) {
  const text = readFileSync(join(srcDir, file), "utf8");
  for (const m of text.matchAll(/\bt\(\s*"([A-Za-z0-9_.-]+)"/g)) {
    usages++;
    for (const [name, dict] of Object.entries(locales)) {
      if (!(m[1] in dict)) problems.push(`${name}: ${file} memakai kunci ${m[1]} yang tidak ada`);
    }
  }
}

if (problems.length > 0) {
  console.error(`i18n: ${problems.length} masalah`);
  for (const p of problems) console.error(`  - ${p}`);
  process.exit(1);
}

console.log(
  `i18n: ${codes.size} kode error, ${idKeys.length} kunci, ${usages} pemakaian literal; id dan en selaras`,
);
