#!/bin/bash
# 由 logo.svg 生成 AppIcon.icns。
# 优先 rsvg-convert（质量最好），没有就退回 qlmanage（系统自带）。
set -euo pipefail
cd "$(dirname "$0")/.."

SRC=web/site/brand/logo.svg
OUT=desktop/AppIcon.icns
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/AppIcon.iconset"

for size in 16 32 128 256 512; do
  double=$((size * 2))
  if command -v rsvg-convert >/dev/null 2>&1; then
    rsvg-convert -w "$size" -h "$size" "$SRC" -o "$TMP/icon_${size}.png"
    rsvg-convert -w "$double" -h "$double" "$SRC" -o "$TMP/icon_${double}.png"
  else
    # qlmanage 一次只出一种尺寸，且输出文件名固定
    qlmanage -t -s "$size" -o "$TMP" "$SRC" >/dev/null 2>&1
    mv "$TMP/logo.svg.png" "$TMP/icon_${size}.png"
    qlmanage -t -s "$double" -o "$TMP" "$SRC" >/dev/null 2>&1
    mv "$TMP/logo.svg.png" "$TMP/icon_${double}.png"
  fi
  cp "$TMP/icon_${size}.png" "$TMP/AppIcon.iconset/icon_${size}x${size}.png"
  cp "$TMP/icon_${double}.png" "$TMP/AppIcon.iconset/icon_${size}x${size}@2x.png"
done

iconutil -c icns "$TMP/AppIcon.iconset" -o "$OUT"
echo "已生成 $OUT"
