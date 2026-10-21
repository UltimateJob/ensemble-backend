#!/usr/bin/env bash
# 装一份可以直接看的演示数据：三个群，都是真实 DeepSeek 跑出来的对话。
#
#   ./scripts/demo.sh          # 装数据并启动
#   ./scripts/demo.sh --only   # 只装数据，不启动
set -e
cd "$(dirname "$0")/.."

if [ ! -d seed/demo ]; then
  echo "seed/demo 不存在"
  exit 1
fi

if [ -d data ] && [ -n "$(ls -A data 2>/dev/null)" ]; then
  echo "→ 备份现有数据到 data.bak"
  rm -rf data.bak
  mv data data.bak
fi

echo "→ 装演示数据（3 个群，真实对话）"
mkdir -p data
cp -r seed/demo/* data/

# 从种子数据里读第一个群的 id，拼一个直达链接
FIRST_ID=""
if command -v python3 >/dev/null 2>&1; then
  FIRST_ID=$(python3 -c "
import json
try:
    c = json.load(open('data/conversations.json', encoding='utf-8'))
    print(c[0]['id'] if c else '')
except Exception:
    print('')
" 2>/dev/null || true)
fi

echo ""
echo "  演示账号   13800000000（验证码随便填）"
echo "  群         华东医院数据中心投标 / 深夜茶话会 / 楼下那几个"
if [ -n "$FIRST_ID" ]; then
  echo "  直达       http://localhost${ADDR:-:8080}/app/?c=${FIRST_ID}"
fi
echo ""

if [ "$1" = "--only" ]; then
  echo "  只装了数据。启动：make run"
  exit 0
fi

exec ./scripts/dev.sh
