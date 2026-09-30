#!/usr/bin/env bash
# =============================================================================
# ocr.sh — OCR LOCAL de screenshot → texto (sem gastar token de visão).
# Use isto em vez de colar a imagem: roda Tesseract (pt+en) e imprime o texto.
#
# Uso:
#   scripts/ocr.sh <imagem.png>            # OCR de um arquivo
#   scripts/ocr.sh                         # OCR da imagem mais recente em ~/Desktop
#   pbpaste -> n/a; para clipboard use: pngpaste /tmp/s.png && scripts/ocr.sh /tmp/s.png
#
# Requer: tesseract + tesseract-lang (brew). Idiomas: por+eng.
# =============================================================================
set -uo pipefail
export PATH="/usr/local/bin:$PATH"

IMG="${1:-}"
if [ -z "$IMG" ]; then
  # imagem mais recente no Desktop (screenshots do macOS caem lá por padrão)
  IMG="$(ls -t "$HOME/Desktop/"*.png 2>/dev/null | head -1)"
  [ -z "$IMG" ] && { echo "uso: scripts/ocr.sh <imagem.png>  (ou tire um print → Desktop)"; exit 1; }
  echo "# OCR da imagem mais recente: $IMG" >&2
fi
[ -f "$IMG" ] || { echo "arquivo não existe: $IMG" >&2; exit 1; }

if ! command -v tesseract >/dev/null 2>&1; then
  echo "tesseract não instalado. Rode: brew install tesseract tesseract-lang" >&2; exit 1
fi

# psm 6 = bloco uniforme de texto (bom p/ tabelas/painel). -l por+eng cobre PT-BR + termos EN.
tesseract "$IMG" stdout -l por+eng --psm 6 2>/dev/null
