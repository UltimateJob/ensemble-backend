#!/usr/bin/env bash
# 一键本地启动：校验环境 → 编译 → 起服务 → 打印地址
set -e
cd "$(dirname "$0")/.."

if [ -f .env ]; then
  set -a; . ./.env; set +a
fi

if ! command -v go >/dev/null 2>&1; then
  echo "需要 Go 1.22+：https://go.dev/dl/"
  exit 1
fi

echo "→ 编译"
go build -o bin/ensemble ./cmd/server

if [ -z "$DEEPSEEK_API_KEY" ]; then
  echo "→ 未设置 DEEPSEEK_API_KEY，使用 mock 上游（输出为演示文本）"
else
  echo "→ 上游：DeepSeek (${DEEPSEEK_MODEL:-deepseek-chat})"
fi

ADDR_VAL="${ADDR:-:8080}"

# 端口被占用时给一句人话，而不是让用户盯着 "address already in use"
PORT_NUM="${ADDR_VAL##*:}"
if command -v lsof >/dev/null 2>&1; then
  BUSY=$(lsof -ti:"$PORT_NUM" 2>/dev/null | head -1 || true)
  if [ -n "$BUSY" ]; then
    echo ""
    echo "  端口 ${PORT_NUM} 已被占用（PID ${BUSY}）。"
    echo "  要么先停掉它：  kill ${BUSY}"
    echo "  要么换个端口：  ADDR=:8081 ./scripts/dev.sh"
    echo ""
    exit 1
  fi
fi

echo ""
echo "  客户端   http://localhost${ADDR_VAL}/app/"
echo "  运维台   http://localhost${ADDR_VAL}/admin/   token: ${ADMIN_TOKEN:-admin-dev-token}"
echo "  健康检查 http://localhost${ADDR_VAL}/healthz"
echo ""

exec ./bin/ensemble
