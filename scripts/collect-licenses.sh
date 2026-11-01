#!/usr/bin/env bash
# Mengumpulkan lisensi pihak ketiga dari dependensi Go dan npm, lalu
# menggabungkannya menjadi THIRD_PARTY_NOTICES.md di root repo.
#
# Dijalankan oleh workflow Release sebelum GoReleaser membangun arsip.
# Membutuhkan go, node, dan npx tersedia di PATH.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/THIRD_PARTY_NOTICES.md"
TMP_GO="$(mktemp)"
TMP_NPM="$(mktemp)"
trap 'rm -f "$TMP_GO" "$TMP_NPM"' EXIT

# --- Go ---
# go-licenses menghasilkan CSV: modul,URL lisensi,tipe lisensi.
# save_path menyimpan salinan berkas lisensi tapi kita hanya pakai CSV.
echo "Collecting Go licenses..."
go install github.com/google/go-licenses@latest
go-licenses csv "$ROOT/cmd/app" 2>/dev/null | sort > "$TMP_GO"

# --- npm (web/) ---
echo "Collecting npm licenses..."
npx --yes license-checker --production --start "$ROOT/web" --csv \
  --out "$TMP_NPM" 2>/dev/null

# --- Gabungkan ---
{
  cat <<'HEADER'
# Third-Party Notices

This file lists third-party software used by YouTube to MP3 & MP4 and their
licenses. It is generated automatically during the release build.

## Go dependencies

| Module | License |
| --- | --- |
HEADER

  while IFS=, read -r mod _url license; do
    # Abaikan modul sendiri.
    [[ "$mod" == github.com/irfanadwifangga/yt-to-mp3* ]] && continue
    printf '| `%s` | %s |\n' "$mod" "$license"
  done < "$TMP_GO"

  cat <<'MID'

## npm dependencies (embedded UI)

| Package | License |
| --- | --- |
MID

  # license-checker CSV: "module@version","license","repository"
  # Baris pertama adalah header, lewati.
  tail -n +2 "$TMP_NPM" | while IFS=, read -r pkg license _repo; do
    # Hapus kutip ganda.
    pkg="${pkg//\"/}"
    license="${license//\"/}"
    [[ -z "$pkg" ]] && continue
    printf '| `%s` | %s |\n' "$pkg" "$license"
  done
} > "$OUT"

echo "Written to $OUT"
