#!/bin/bash
# 直接运行桌面壳（绕过 open），并打印壳与后端的日志。
set -uo pipefail
cd "$(dirname "$0")/.."

APP="desktop/build/纸团.app"
EXE="$APP/Contents/MacOS/纸团"

pkill -f "$APP/Contents/MacOS/" 2>/dev/null
pkill -f 'bin/ensemble' 2>/dev/null
sleep 1

echo "启动：$EXE"
"$EXE" > /tmp/zhituan-shell.log 2>&1 &
SHELL_PID=$!
echo "壳 pid=${SHELL_PID}"

for i in $(seq 1 40); do
  sleep 0.5
  PORT=$(lsof -iTCP -sTCP:LISTEN -n -P 2>/dev/null \
    | grep -E ':(80[89][0-9])' | grep -v Python \
    | grep -oE '80[89][0-9]' | head -1)
  if [ -n "${PORT}" ]; then
    echo "后端监听端口：${PORT}"
    break
  fi
done

sleep 3
echo ""
echo "== 壳日志 =="
cat /tmp/zhituan-shell.log 2>/dev/null || echo "(空)"
echo ""
echo "== 后端日志（最后 8 行）=="
tail -8 "$HOME/Library/Application Support/纸团/ensemble.log" 2>/dev/null || echo "(无)"
echo ""
echo "== 进程 =="
pgrep -fl 'MacOS/纸团|MacOS/ensemble' | head -5
