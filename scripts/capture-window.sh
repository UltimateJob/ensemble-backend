#!/bin/bash
# 截取「纸团」窗口的内容（按窗口 id 精确截图，不依赖整屏）。
set -uo pipefail
cd "$(dirname "$0")/.."

OUT="${1:-/tmp/zhituan-window.png}"

WID=$(osascript -e 'tell application "System Events" to tell process "纸团" to get id of window 1' 2>/dev/null)
if [ -z "${WID}" ]; then
  echo "找不到「纸团」窗口。先运行 ./scripts/run-shell.sh"
  echo "当前进程："
  pgrep -fl 'MacOS/纸团' | head -3
  exit 1
fi

echo "窗口 id：${WID}"
if screencapture -l "${WID}" -x "${OUT}" 2>/dev/null; then
  echo "✓ ${OUT} ($(du -h "${OUT}" | cut -f1))"
else
  echo "✗ screencapture 失败（可能需要屏幕录制权限）"
  exit 1
fi
