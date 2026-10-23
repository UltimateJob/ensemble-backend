#!/bin/bash
# 把头像 PNG 转成 JPEG 并重建 manifest。
#
# 为什么转 JPEG：头像不需要透明通道，PNG 比 JPEG 大 6 倍
# （99KB → 15KB）。101 张从 11MB 降到约 2MB，可以随仓库分发，
# 新克隆下来就自带完整形象，不用再跑生成脚本。
set -euo pipefail
cd "$(dirname "$0")/.."

DIR=web/app/avatars
CHAR=$DIR/characters
COVER=$DIR/covers
QUALITY="${1:-82}"

echo "==> 转换角色头像"
n=0
for f in "$CHAR"/*.png; do
  [ -e "$f" ] || continue
  out="${f%.png}.jpg"
  sips -s format jpeg -s formatOptions "$QUALITY" "$f" --out "$out" >/dev/null 2>&1
  rm -f "$f"
  n=$((n + 1))
done
echo "   $n 张"

echo "==> 转换团队封面"
m=0
for f in "$COVER"/*.png; do
  [ -e "$f" ] || continue
  out="${f%.png}.jpg"
  sips -s format jpeg -s formatOptions "$QUALITY" "$f" --out "$out" >/dev/null 2>&1
  rm -f "$f"
  m=$((m + 1))
done
echo "   $m 张"

echo "==> 重建 manifest（改扩展名）"
python3 - <<'PY'
import json, os

path = 'web/app/avatars/manifest.json'
m = json.load(open(path, encoding='utf-8'))

for kind in ('characters', 'covers'):
    new = {}
    for name, rel in m.get(kind, {}).items():
        jpg = rel[:-4] + '.jpg' if rel.endswith('.png') else rel
        if os.path.exists('web/app/avatars/' + jpg):
            new[name] = jpg
        elif os.path.exists('web/app/avatars/' + rel):
            new[name] = rel
    m[kind] = new

json.dump(m, open(path, 'w', encoding='utf-8'), ensure_ascii=False, indent=1)
print(f"   角色 {len(m.get('characters', {}))}，封面 {len(m.get('covers', {}))}")
PY

echo ""
echo "总体积：$(du -sh "$DIR" | cut -f1)"
