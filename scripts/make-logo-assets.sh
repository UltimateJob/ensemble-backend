#!/bin/bash
# 从选定的纸团原图生成各尺寸品牌资源。
# 用法：./scripts/make-logo-assets.sh [源图]
set -euo pipefail
cd "$(dirname "$0")/.."

SRC="${1:-docs/brand/zhituan/logo-b.png}"
OUT=web/site/brand

if [ ! -f "$SRC" ]; then
  echo "找不到源图 $SRC"
  exit 1
fi

# 应用图标：裁成方形，圆角交给系统（macOS 会自己加圆角，这里只保证满幅）
cp "$SRC" "$OUT/logo.png"
sips -Z 512 "$OUT/logo.png" >/dev/null

# favicon 用同一张缩到 64
cp "$SRC" "$OUT/favicon.png"
sips -Z 64 "$OUT/favicon.png" >/dev/null

# 桌面图标母版
cp "$SRC" desktop/AppIcon-source.png
sips -Z 1024 desktop/AppIcon-source.png >/dev/null

echo "已生成："
echo "  $OUT/logo.png     (512)"
echo "  $OUT/favicon.png  (64)"
echo "  desktop/AppIcon-source.png (1024)"
