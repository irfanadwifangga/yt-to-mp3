#!/usr/bin/env bash
#
# Memperbarui internal/infrastructure/tools/manifest.json ke rilis terbaru
# dan mengisi checksum SHA-256.
#
# Manifest bersifat fail-closed (ADR-033): entri tanpa checksum menolak
# instalasi. Script ini satu-satunya cara resmi mengisinya, dan hasilnya
# harus di-commit sebagai perubahan tersendiri supaya bisa direview dan
# mudah di-bisect ketika update tool menyebabkan regresi.
#
# Butuh: bash, curl, jq, sha256sum (atau shasum).
#
# Pemakaian:
#   scripts/update-tool-manifest.sh            # yt-dlp saja (cepat)
#   scripts/update-tool-manifest.sh --ffmpeg   # termasuk FFmpeg (unduh besar)

set -euo pipefail

MANIFEST="internal/infrastructure/tools/manifest.json"
WITH_FFMPEG=0
[[ "${1:-}" == "--ffmpeg" ]] && WITH_FFMPEG=1

for cmd in curl jq; do
  command -v "$cmd" >/dev/null || { echo "butuh $cmd" >&2; exit 1; }
done

if command -v sha256sum >/dev/null; then
  hash_of() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null; then
  hash_of() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  echo "butuh sha256sum atau shasum" >&2
  exit 1
fi

[[ -f "$MANIFEST" ]] || { echo "manifest tidak ditemukan: $MANIFEST" >&2; exit 1; }

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# set_field <tool> <platform> <field> <value>
set_field() {
  jq --arg t "$1" --arg p "$2" --arg f "$3" --arg v "$4" \
    '.tools[$t].builds[$p][$f] = $v' "$MANIFEST" > "$tmpdir/m.json"
  mv "$tmpdir/m.json" "$MANIFEST"
}

set_version() {
  jq --arg t "$1" --arg v "$2" '.tools[$t].version = $v' "$MANIFEST" > "$tmpdir/m.json"
  mv "$tmpdir/m.json" "$MANIFEST"
}

echo "==> yt-dlp"
YTDLP_TAG=$(curl -fsSL https://api.github.com/repos/yt-dlp/yt-dlp/releases/latest | jq -r .tag_name)
echo "    rilis terbaru: $YTDLP_TAG"

# yt-dlp menerbitkan checksum resmi, jadi tidak perlu mengunduh binary-nya.
SUMS="$tmpdir/SHA2-256SUMS"
curl -fsSL -o "$SUMS" \
  "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_TAG}/SHA2-256SUMS"

declare -A YTDLP_ASSET=(
  ["windows/amd64"]="yt-dlp.exe"
  ["linux/amd64"]="yt-dlp_linux"
  ["linux/arm64"]="yt-dlp_linux_aarch64"
  ["darwin/amd64"]="yt-dlp_macos"
  ["darwin/arm64"]="yt-dlp_macos"
)

for platform in "${!YTDLP_ASSET[@]}"; do
  asset="${YTDLP_ASSET[$platform]}"
  sum=$(awk -v a="$asset" '$2 == a {print $1}' "$SUMS" | head -1)
  if [[ -z "$sum" ]]; then
    echo "    LEWAT $platform: $asset tidak ada di SHA2-256SUMS" >&2
    continue
  fi
  url="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_TAG}/${asset}"
  set_field "yt-dlp" "$platform" "url" "$url"
  set_field "yt-dlp" "$platform" "sha256" "$sum"
  echo "    $platform -> ${sum:0:12}..."
done
set_version "yt-dlp" "$YTDLP_TAG"

if [[ "$WITH_FFMPEG" -eq 0 ]]; then
  echo
  echo "FFmpeg dilewati. Jalankan ulang dengan --ffmpeg untuk ikut memperbaruinya."
  echo "Selesai. Review diff pada $MANIFEST lalu commit."
  exit 0
fi

echo "==> FFmpeg (BtbN)"
# BtbN tidak menerbitkan checksum per-asset, jadi arsipnya harus diunduh dan
# dihitung sendiri. Ini yang membuat langkah ini mahal dan opsional.
FFMPEG_TAG=$(curl -fsSL https://api.github.com/repos/BtbN/FFmpeg-Builds/releases/latest | jq -r .tag_name)
echo "    rilis terbaru: $FFMPEG_TAG"

declare -A FFMPEG_ASSET=(
  ["windows/amd64"]="ffmpeg-master-latest-win64-gpl.zip"
  ["linux/amd64"]="ffmpeg-master-latest-linux64-gpl.tar.xz"
  ["linux/arm64"]="ffmpeg-master-latest-linuxarm64-gpl.tar.xz"
)

for platform in "${!FFMPEG_ASSET[@]}"; do
  asset="${FFMPEG_ASSET[$platform]}"
  url="https://github.com/BtbN/FFmpeg-Builds/releases/download/${FFMPEG_TAG}/${asset}"
  echo "    mengunduh $asset ..."
  if ! curl -fsSL -o "$tmpdir/$asset" "$url"; then
    echo "    LEWAT $platform: unduhan gagal" >&2
    continue
  fi
  sum=$(hash_of "$tmpdir/$asset")
  set_field "ffmpeg" "$platform" "url" "$url"
  set_field "ffmpeg" "$platform" "sha256" "$sum"
  rm -f "$tmpdir/$asset"
  echo "    $platform -> ${sum:0:12}..."
done
set_version "ffmpeg" "$FFMPEG_TAG"

echo
echo "macOS belum ditangani otomatis; lihat catatan pada entri darwin/* di manifest."
echo "Selesai. Review diff pada $MANIFEST lalu commit."
