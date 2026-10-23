#!/bin/bash
# 模拟「用户第一次打开 App」：全新数据目录 + 包内后端，
# 验证演示内容自动装载、头像可访问。
set -uo pipefail
cd "$(dirname "$0")/.."

APP="desktop/build/纸团.app"
BACKEND="$APP/Contents/MacOS/ensemble"
DATA=/tmp/zhituan-fresh-$$
PORT=18399

cleanup() {
  pkill -f "DATA_DIR=${DATA}" 2>/dev/null || true
  rm -rf "$DATA"
}
trap cleanup EXIT

if [ ! -x "$BACKEND" ]; then
  echo "✗ 找不到 $BACKEND，先跑 ./desktop/build.sh"
  exit 1
fi

echo "==> 用全新数据目录启动包内后端（${DATA}）"
ADDR=":$PORT" DATA_DIR="$DATA" "$BACKEND" > /tmp/zhituan-fresh.log 2>&1 &
BPID=$!

for _ in $(seq 1 50); do
  sleep 0.2
  curl -sf -o /dev/null "http://127.0.0.1:${PORT}/healthz" && break
done

echo ""
echo "== 演示内容 =="
grep -F '首次启动，已装载演示内容' /tmp/zhituan-fresh.log | tail -1 || echo "  ✗ 没有装载演示内容"

echo ""
echo "== 资源可访问性 =="
for p in /app/ /app/avatars/manifest.json /app/css/tokens.css /site/brand/logo-mark.png; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}${p}")
  printf '  %-36s %s\n' "$p" "$code"
done

echo ""
echo "== 头像抽样 =="
AV=$(python3 - <<'PY'
import json, urllib.request
m = json.load(urllib.request.urlopen('http://127.0.0.1:18399/app/avatars/manifest.json'))
print(m['characters'].get('工头', ''))
PY
)
if [ -n "$AV" ]; then
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/app/avatars/${AV}")
  echo "  工头 → ${AV} (${code})"
else
  echo "  ✗ manifest 里没有工头"
fi

echo ""
echo "== 登录并检查演示群 =="
TOKEN=$(curl -s -X POST "http://127.0.0.1:${PORT}/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"phone","target":"13800000000","code":""}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["token"])' 2>/dev/null)

if [ -z "${TOKEN}" ]; then
  echo "  ✗ 登录失败"
  exit 1
fi
curl -s "http://127.0.0.1:${PORT}/v1/conversations" -H "Authorization: Bearer ${TOKEN}" \
  | python3 -c '
import sys, json
convs = json.load(sys.stdin)["data"]
print("  群数：%d" % len(convs))
for c in convs:
    print("    - " + c["title"])
'
