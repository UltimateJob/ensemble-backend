#!/bin/bash
# 桌面端冒烟：从干净目录启动打包好的 .app，确认后端自启 + 页面可访问。
#
# 用法：./scripts/desktop-check.sh [app路径]
# 默认用 desktop/build/智团.app。
set -uo pipefail

SRC="${1:-desktop/build/智团.app}"
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

# 用 ASCII 名字的副本启动：中文路径不影响壳本身，但方便脚本引用。
"$APP/Contents/MacOS/智团" > "$TMP/app.log" 2>&1 &
APP_PID=$!

PORT=""
for i in $(seq 1 40); do
  sleep 0.3
  PORT=$(lsof -iTCP -sTCP:LISTEN -n -P -a -p "$APP_PID" 2>/dev/null | grep -oE ':(80[89][0-9])' | head -1 | tr -d ':')
  if [ -z "$PORT" ]; then
    # 后端是子进程，不在 app pid 下找，改为扫端口
    PORT=$(lsof -iTCP -sTCP:LISTEN -n -P 2>/dev/null | grep -oE ':(80[89][0-9])' | head -1 | tr -d ':')
  fi
  if [ -n "$PORT" ]; then
    if curl -sf -o /dev/null "http://127.0.0.1:${PORT}/healthz"; then break; fi
  fi
  PORT=""
done

if [ -z "$PORT" ]; then
  echo "✗ 后端没有起来"
  echo "--- app.log ---"
  cat "$TMP/app.log" 2>/dev/null | head -20
  LOGDIR="$HOME/Library/Application Support/智团/ensemble.log"
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

code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/site/brand/logo.svg")
echo "  /site/brand/logo.svg → $code"
[ "$code" = "200" ] || { echo "✗ 品牌资源没打进包"; exit 1; }

if grep -q "静态资源目录未找到" "$HOME/Library/Application Support/智团/ensemble.log" 2>/dev/null; then
  echo "✗ 日志里仍有「静态资源目录未找到」"
  exit 1
fi

echo "✓ 桌面端冒烟通过"
