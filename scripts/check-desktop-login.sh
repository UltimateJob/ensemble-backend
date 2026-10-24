#!/bin/bash
# 验证桌面端「代理登录 → 调模型」整条链。
#
#   ./scripts/check-desktop-login.sh [port] [phone]
#
# 假设本地后端已经用 REMOTE_BASE 启动。
set -uo pipefail

PORT="${1:-18215}"
PHONE="${2:-13800009999}"
BASE="http://127.0.0.1:$PORT"

say() { printf '\n=== %s ===\n' "$*"; }

say "1. 发验证码（本地后端代理到线上）"
R=$(curl -s -X POST "$BASE/v1/auth/send-code" -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\"}")
echo "  $R"
CODE=$(echo "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("devCode",""))' 2>/dev/null)
if [ -z "$CODE" ]; then echo "  ✗ 没拿到验证码（线上限流或不可用）"; exit 1; fi

say "2. 登录（线上校验，本地签发镜像会话）"
R=$(curl -s -X POST "$BASE/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\",\"code\":\"$CODE\"}")
LT=$(echo "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("token",""))' 2>/dev/null)
RT=$(echo "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("remoteToken",""))' 2>/dev/null)
if [ -z "$LT" ]; then echo "  ✗ 本地会话没签发：$R"; exit 1; fi
if [ -z "$RT" ]; then echo "  ✗ 没拿到线上令牌：$R"; exit 1; fi
echo "  ✓ 本地令牌 ${LT:0:12}…  线上令牌 ${RT:0:12}…"

say "3. 把线上令牌交给本地后端"
curl -s -X POST "$BASE/v1/remote/token" -H "Authorization: Bearer $LT" \
  -H 'Content-Type: application/json' -d "{\"token\":\"$RT\"}" | head -c 200
echo ""

say "4. 调模型（本地 → 线上 → 上游）"
OUT=$(curl -s --max-time 90 -X POST "$BASE/v1/chat/completions" \
  -H "Authorization: Bearer $LT" -H 'Content-Type: application/json' \
  -d '{"model":"teamwise-chat","messages":[{"role":"user","content":"只回复两个字：你好"}],"stream":false}')
echo "  $(echo "$OUT" | head -c 400)"

CONTENT=$(echo "$OUT" | python3 -c '
import sys, json
try:
    print(json.load(sys.stdin).get("data",{}).get("choices",[{}])[0].get("message",{}).get("content",""))
except Exception:
    print("")
' 2>/dev/null)

say "结论"
if [ -n "$CONTENT" ]; then
  echo "  ✓ 桌面端链路畅通，模型返回：$CONTENT"
else
  echo "  ✗ 模型没返回正文"
  exit 1
fi
