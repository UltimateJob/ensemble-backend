#!/bin/bash
# 验证连接器动作真的进了 Agent 的工具列表。
#
#   ./scripts/check-connector-tools.sh [port] [phone]
set -uo pipefail

PORT="${1:-18200}"
PHONE="${2:-13900002222}"
BASE="http://127.0.0.1:$PORT"

RC=$(curl -s -X POST "$BASE/v1/auth/send-code" -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\"}")
CODE=$(echo "$RC" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["devCode"])' 2>/dev/null)
TOK=$(curl -s -X POST "$BASE/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\",\"code\":\"$CODE\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["token"])' 2>/dev/null)
[ -z "$TOK" ] && { echo "✗ 登录失败"; exit 1; }

echo "=== 1. 建一个 GitHub 连接器 ==="
CID=$(curl -s -X POST "$BASE/v1/connectors" -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"github","name":"测试仓库","scope":"team","config":{"repo":"UltimateJob/ensemble-backend"},"secrets":{"token":"ghp_fake_for_shape"}}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])' 2>/dev/null)
echo "  连接器 id: $CID"

echo "=== 2. 建工作区 ==="
rm -rf /tmp/conn-ws && mkdir -p /tmp/conn-ws && echo "test" > /tmp/conn-ws/a.txt
WS=$(curl -s -X POST "$BASE/v1/workspaces" -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' -d '{"root":"/tmp/conn-ws"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])' 2>/dev/null)
echo "  工作区 id: $WS"

echo "=== 3. 跑一个需要外部上下文的任务 ==="
RUN=$(curl -s -X POST "$BASE/v1/workspaces/$WS/runs" -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"task":"用 github_search_code 搜一下仓库里有没有 panic 关键字，把结果写进 notes.md","permission":"readonly","maxSteps":4}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])' 2>/dev/null)
echo "  run id: $RUN"

sleep 18
echo "=== 4. 检查这次 Run 用了哪些工具 ==="
curl -s "$BASE/v1/runs/$RUN" -H "Authorization: Bearer $TOK" | python3 -c '
import sys, json
d = json.load(sys.stdin).get("data", {})
print("  状态:", d.get("status"))
tools = [t.get("tool") for t in (d.get("toolCalls") or [])]
print("  工具调用:", tools or "（无）")
conn = [t for t in tools if t and "__" in t]
if conn:
    print("  ✓ Agent 用到了连接器工具:", conn)
else:
    print("  ⚠ 没用连接器工具（可能模型选择了别的方式，或工具名没被识别）")
errs = [t.get("output","") for t in (d.get("toolCalls") or []) if t.get("status")=="error"]
if errs:
    print("  错误输出:", errs[0][:200])
'
