#!/bin/bash
# 启动桌面 App 并截一张整屏图，用来核对原生窗口里的实际观感。
#   ./scripts/desktop-shot.sh [输出路径]
set -uo pipefail
cd "$(dirname "$0")/.."

OUT="${1:-/tmp/desktop-shot.png}"
APP="desktop/build/纸团.app"

if [ ! -d "$APP" ]; then
  echo "找不到 $APP，先跑 ./desktop/build.sh"
  exit 1
fi

pkill -f "$APP/Contents/MacOS/" 2>/dev/null
sleep 1

"$APP/Contents/MacOS/纸团" > /tmp/zhituan-app.log 2>&1 &
APP_PID=$!
echo "App 已启动（pid ${APP_PID}），等后端就绪…"

for _ in $(seq 1 60); do
  sleep 0.5
  if curl -sf -o /dev/null http://127.0.0.1:8080/healthz; then
    echo "✓ 后端就绪"
    break
  fi
done

# 给 WKWebView 渲染与字体加载留时间
sleep 4

if screencapture -x "$OUT" 2>/dev/null; then
  echo "✓ 截图：$OUT ($(du -h "$OUT" | cut -f1))"
else
  echo "✗ screencapture 不可用（需要图形会话与录屏权限）"
fi

echo "--- App 日志 ---"
head -12 /tmp/zhituan-app.log
