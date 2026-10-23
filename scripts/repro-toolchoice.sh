#!/bin/bash
# 复现线上 /v1/chat/completions 的 tool_choice 问题。
#
#   ./scripts/repro-toolchoice.sh
#
# 关键点：请求里带 tools 且 toolChoice 是个函数名（不是 auto/none/required）时，
# 线上返回 422。不带 tools 时一切正常。
set -uo pipefail

BASE="${1:-https://teamwise.cn}"
PHONE="${PHONE:-13800000000}"

CODE=$(curl -s --max-time 20 -X POST "$BASE/v1/auth/send-code" \
  -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("devCode",""))')
TOKEN=$(curl -s --max-time 20 -X POST "$BASE/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\",\"code\":\"$CODE\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("token",""))')
if [ -z "$TOKEN" ]; then echo "✗ 登录失败"; exit 1; fi

echo "=== 1. 不带 tools ==="
curl -s --max-time 40 -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"只回复两个字：你好"}],"stream":false}' | head -c 300
echo ""

echo "=== 2. 带 tools + toolChoice=auto ==="
curl -s --max-time 40 -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read","description":"d","parameters":{"type":"object","properties":{}}}}],"toolChoice":"auto","stream":false}' | head -c 400
echo ""

echo "=== 3. 带 tools + toolChoice=函数名（强制单个工具） ==="
curl -s --max-time 40 -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"read","description":"d","parameters":{"type":"object","properties":{}}}}],"toolChoice":"read","stream":false}' | head -c 400
echo ""
