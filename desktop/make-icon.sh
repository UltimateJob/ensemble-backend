#!/bin/bash
# 由纸团原图生成 AppIcon.icns。
# 原图是 1024 方形；macOS 图标规范要求四周留边，这里按 82% 缩放后居中。
set -euo pipefail
cd "$(dirname "$0")/.."

SRC=desktop/AppIcon-source.png
OUT=desktop/AppIcon.icns
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

if [ ! -f "$SRC" ]; then
  echo "找不到 $SRC，先跑 ./scripts/make-logo-assets.sh"
  exit 1
fi

mkdir -p "$TMP/AppIcon.iconset"

# 1024 母版：缩小到 840 并画到 1024 的透明画布中央（macOS 图标四周留白）
sips -Z 840 "$SRC" --out "$TMP/base.png" >/dev/null
python3 - "$TMP/base.png" "$TMP/padded.png" <<'PY'
import sys
try:
    from PIL import Image
except ImportError:
    # 没有 Pillow 就跳过留白，直接用原图
    import shutil
    shutil.copy(sys.argv[1], sys.argv[2])
    sys.exit(0)
base = Image.open(sys.argv[1]).convert('RGBA')
canvas = Image.new('RGBA', (1024, 1024), (0, 0, 0, 0))
off = (1024 - base.width) // 2
canvas.paste(base, (off, off), base)
canvas.save(sys.argv[2])
PY

for size in 16 32 128 256 512; do
  double=$((size * 2))
  sips -Z "$size" "$TMP/padded.png" --out "$TMP/icon_${size}.png" >/dev/null
  sips -Z "$double" "$TMP/padded.png" --out "$TMP/icon_${double}.png" >/dev/null
  cp "$TMP/icon_${size}.png" "$TMP/AppIcon.iconset/icon_${size}x${size}.png"
  cp "$TMP/icon_${double}.png" "$TMP/AppIcon.iconset/icon_${size}x${size}@2x.png"
done

iconutil -c icns "$TMP/AppIcon.iconset" -o "$OUT"
echo "已生成 $OUT"
