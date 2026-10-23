#!/bin/bash
# 桌面端冒烟：从干净目录启动打包好的 .app，确认后端自启 + 页面可访问。
#
# 用法：./scripts/desktop-check.sh [app路径]
# 默认用 desktop/build/纸团.app。
set -uo pipefail

SRC="${1:-desktop/build/纸团.app}"
TMP=$(mktemp -d /tmp/zt-check-XXXXXX)
APP="$TMP/Zhituan.app"

cleanup() {
  pkill -f "$APP/Contents/MacOS/" 2>/dev/null
  rm -rf "$TMP"
}
trap cleanup EXIT

if [ ! -d "$SRC" ]; then
  echo "找不到 $SRC，先跑 ./desktop/build.sh"
  exit 1
fi

cp -R "$SRC" "$APP"

# 端口上有别的后端在跑时，壳会直接复用、不写新日志，
# 脚本就会误判「没起来」。先要求端口干净，让测试结果可信。
if lsof -iTCP:8080 -sTCP:LISTEN -n -P 2>/dev/null | grep -q LISTEN; then
  echo "✗ 8080 已被占用，冒烟会连到别人的后端。先停掉它："
  lsof -iTCP:8080 -sTCP:LISTEN -n -P 2>/dev/null | tail -1
  exit 1
fi

# 后端日志落在应用支持目录，重启会追加。先把已有内容备份掉，
# 只从本次启动后新增的行里找端口——否则会读到上一次运行的残留，
# 拿着一个早已不监听的端口去 curl，结果误报「后端没起来」。
LOG="$HOME/Library/Application Support/纸团/ensemble.log"
mkdir -p "$(dirname "$LOG")"
if [ -f "$LOG" ]; then
  mv "$LOG" "$LOG.prev" 2>/dev/null || true
fi
: > "$LOG"

# 用 ASCII 名字的副本启动：中文路径不影响壳本身，但方便脚本引用。
"$APP/Contents/MacOS/纸团" > "$TMP/app.log" 2>&1 &
APP_PID=$!

# 端口不写死：壳会自己探 8080–8099，从启动日志的 "addr" 字段读实际值。
PORT=""
for i in $(seq 1 60); do
  sleep 0.4
  CAND=$(grep -F "\"msg\":\"纸团后端已启动\"" "$LOG" 2>/dev/null \
    | tail -1 | grep -oE '"addr":":80[89][0-9]"' | grep -oE '80[89][0-9]')
  if [ -n "$CAND" ] && curl -sf -o /dev/null "http://127.0.0.1:${CAND}/healthz"; then
    PORT="$CAND"
    break
  fi
done

if [ -z "$PORT" ]; then
  echo "✗ 后端没有起来"
  echo "--- app.log ---"
  cat "$TMP/app.log" 2>/dev/null | head -20
  LOGDIR="$HOME/Library/Application Support/纸团/ensemble.log"
  [ -f "$LOGDIR" ] && { echo "--- ensemble.log ---"; tail -20 "$LOGDIR"; }
  exit 1
fi
echo "✓ 后端已自启（端口 ${PORT}）"

code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/app/")
echo "  /app/ → $code"
[ "$code" = "200" ] || { echo "✗ 页面不可访问"; exit 1; }

code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/app/css/tokens.css")
echo "  /app/css/tokens.css → $code"
[ "$code" = "200" ] || { echo "✗ 静态资源没打进包"; exit 1; }

code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/site/brand/logo-mark.png")
echo "  /site/brand/logo-mark.png → $code"
[ "$code" = "200" ] || { echo "✗ 品牌资源没打进包"; exit 1; }

if grep -q "静态资源目录未找到" "$HOME/Library/Application Support/纸团/ensemble.log" 2>/dev/null; then
  echo "✗ 日志里仍有「静态资源目录未找到」"
  exit 1
fi

echo "✓ 桌面端冒烟通过"
