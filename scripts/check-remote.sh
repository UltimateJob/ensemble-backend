#!/bin/bash
# 检查线上后端是否可用：登录 → 直接调 /v1/chat/completions → 看有没有正文。
#
#   ./scripts/check-remote.sh [base]
set -uo pipefail

BASE="${1:-https://teamwise.cn}"
PHONE="${PHONE:-13800000000}"

say() { echo ""; echo "=== $* ==="; }

say "1. 健康检查"
curl -s -o /dev/null -w "  /healthz → %{http_code}\n" --max-time 15 "$BASE/healthz"

say "2. 取验证码"
RESP=$(curl -s --max-time 20 -X POST "$BASE/v1/auth/send-code" \
  -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\"}")
echo "  $RESP"
CODE=$(echo "$RESP" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("devCode",""))' 2>/dev/null)
if [ -z "$CODE" ]; then
  echo "  ✗ 没有拿到 devCode（线上可能已关 DEV_BYPASS，这是好事）"
  exit 1
fi

say "3. 登录"
TOKEN=$(curl -s --max-time 20 -X POST "$BASE/v1/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\",\"code\":\"$CODE\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("token",""))' 2>/dev/null)
if [ -z "$TOKEN" ]; then
  echo "  ✗ 登录失败"
  exit 1
fi
echo "  ✓ 拿到令牌（${TOKEN:0:16}…）"

say "4. 直接调线上 /v1/chat/completions"
OUT=$(curl -s --max-time 60 -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"只回复两个字：你好"}],"stream":false}')
echo "  $OUT" | head -c 600
echo ""

CONTENT=$(echo "$OUT" | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
    c = d.get("data", {}).get("choices", [{}])[0].get("message", {}).get("content", "")
    print(c)
except Exception:
    print("")
' 2>/dev/null)

say "结论"
if [ -n "$CONTENT" ]; then
  echo "  ✓ 线上模型可用，返回：$CONTENT"
else
  echo "  ✗ 线上返回了 200 但正文为空"
  echo "    这意味着线上后端连上了上游，但没把内容透传出来。"
  echo "    需要检查线上版本的 /v1/chat/completions 实现。"
fi
