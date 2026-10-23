#!/bin/bash
# 起一个测试用的后端，进度可预测、不会因为 shell 退出被带走。
#   ./scripts/serve-test.sh start [port] [datadir]
#   ./scripts/serve-test.sh stop
#   ./scripts/serve-test.sh status
set -uo pipefail
cd "$(dirname "$0")/.."

PIDFILE=/tmp/zhituan-test.pid
PORTFILE=/tmp/zhituan-test.port

case "${1:-start}" in
  start)
    PORT="${2:-18200}"
    DATA="${3:-/tmp/zhituan-test-data}"
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      echo "已在运行：$(cat "$PIDFILE") 端口 $(cat "$PORTFILE" 2>/dev/null)"
      exit 0
    fi
    rm -rf "$DATA"
    # macOS 没有 setsid。用 sh -c 包一层再 nohup，让后端成为独立会话，
    # 不会被当前 shell 退出时按进程组带走。
    nohup sh -c "exec env ADDR=':$PORT' DATA_DIR='$DATA' ./bin/ensemble" > /tmp/zhituan-test.log 2>&1 &
    BGPID=$!
    disown "$BGPID" 2>/dev/null || true
    echo "$BGPID" > "$PIDFILE"
    echo "$PORT" > "$PORTFILE"
    for _ in $(seq 1 40); do
      sleep 0.2
      if curl -sf -o /dev/null "http://127.0.0.1:$PORT/healthz"; then
        echo "✓ 已启动：端口 ${PORT}（pid $(cat "$PIDFILE")）"
        exit 0
      fi
    done
    echo "✗ 启动失败，看 /tmp/zhituan-test.log"
    tail -10 /tmp/zhituan-test.log
    exit 1
    ;;
  stop)
    if [ -f "$PIDFILE" ]; then
      kill "$(cat "$PIDFILE")" 2>/dev/null
      rm -f "$PIDFILE" "$PORTFILE"
    fi
    # 兜底：清掉所有测试实例
    pkill -f 'DATA_DIR=/tmp/zhituan-test-data' 2>/dev/null
    echo "已停止"
    ;;
  status)
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      echo "运行中：pid $(cat "$PIDFILE") 端口 $(cat "$PORTFILE")"
    else
      echo "未运行"
    fi
    ;;
esac
