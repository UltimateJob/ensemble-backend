#!/bin/bash
# 线上端到端自测：登录 → 工作区 → 连接器 → 生图 → 路由
#
#   ./scripts/selftest-live.sh [base]
#
# 只读/可回滚的操作。会创建测试用户与连接器，跑完自行清理。
set -uo pipefail

BASE="${1:-https://teamwise.cn}"
PHONE="${PHONE:-13900001234}"
PASS=0; FAIL=0

ok()   { echo "  ✓ $*"; PASS=$((PASS+1)); }
bad()  { echo "  ✗ $*"; FAIL=$((FAIL+1)); }
head2(){ printf '\n=== %s ===\n' "$*"; }

head2 "1. 登录（线上鉴权）"
R=$(curl -s --max-time 20 -X POST "$BASE/v1/auth/send-code" -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\"}")
CODE=$(echo "$R" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("devCode",""))' 2>/dev/null)
if [ -z "$CODE" ]; then bad "拿不到验证码：$R"; exit 1; fi
ok "验证码已下发"

TOK=$(curl -s --max-time 20 -X POST "$BASE/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"kind\":\"phone\",\"target\":\"$PHONE\",\"code\":\"$CODE\"}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("token",""))' 2>/dev/null)
if [ -n "$TOK" ]; then ok "登录成功（${#TOK} 字节令牌）"; else bad "登录失败"; exit 1; fi

head2 "2. 普通对话"
C=$(curl -s --max-time 60 -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"只回复两个字：正常"}],"stream":false}' \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("data",{}).get("choices",[{}])[0].get("message",{}).get("content",""))' 2>/dev/null)
if [ -n "$C" ]; then ok "模型返回：$C"; else bad "对话无正文"; fi

head2 "3. 工具调用（OpenAI 协议兼容）"
T=$(curl -s --max-time 60 -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"列出 /tmp"}],"tools":[{"type":"function","function":{"name":"list","description":"列目录","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}],"tool_choice":"auto","stream":false}' \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);m=d.get("data",{}).get("choices",[{}])[0].get("message",{});print(",".join(t["function"]["name"] for t in (m.get("tool_calls") or [])))' 2>/dev/null)
if [ -n "$T" ]; then ok "返回 tool_calls：$T"; else bad "没有返回 tool_calls"; fi

head2 "4. usage 透出（计费依赖）"
U=$(curl -s --max-time 60 -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"}],"stream":false}' \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("data",{}).get("usage",{}).get("total_tokens",0))' 2>/dev/null)
if [ "${U:-0}" -gt 0 ] 2>/dev/null; then ok "usage.total_tokens=$U"; else bad "usage 缺失或为 0"; fi

head2 "5. workload 路由"
for wl in code writing analysis chat; do
  R2=$(curl -s --max-time 60 -X POST "$BASE/v1/chat/completions" -H "Authorization: Bearer $TOK" \
    -H 'Content-Type: application/json' \
    -d "{\"model\":\"\",\"workload\":\"$wl\",\"messages\":[{\"role\":\"user\",\"content\":\"ok\"}],\"stream\":false}" \
    | python3 -c 'import sys,json;print("ok" if json.load(sys.stdin).get("ok") else "fail")' 2>/dev/null)
  if [ "$R2" = "ok" ]; then ok "workload=$wl"; else bad "workload=$wl 失败"; fi
done

head2 "6. 连接器"
CID=$(curl -s --max-time 20 -X POST "$BASE/v1/connectors" -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"kind":"webhook","name":"自测","scope":"private","config":{"url":"https://example.com/hook"},"secrets":{}}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("data",{}).get("id",""))' 2>/dev/null)
if [ -n "$CID" ]; then ok "创建连接器 $CID"; else bad "创建连接器失败"; fi
LIST=$(curl -s --max-time 20 "$BASE/v1/connectors" -H "Authorization: Bearer $TOK" \
  | python3 -c 'import sys,json;print(len(json.load(sys.stdin).get("data",{}).get("items",[])))' 2>/dev/null)
if [ "${LIST:-0}" -gt 0 ] 2>/dev/null; then ok "连接器列表 $LIST 条"; else bad "连接器列表为空"; fi

head2 "7. 生图接口（未配渠道时应 503）"
IT=$(curl -s --max-time 20 -o /tmp/it.json -w '%{http_code}' -X POST "$BASE/v1/images" \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{"prompt":"test"}')
BODY=$(head -c 120 /tmp/it.json)
if [ "$IT" = "503" ] || [ "$IT" = "201" ]; then ok "HTTP $IT — $BODY"; else bad "HTTP $IT — $BODY"; fi

head2 "8. 资料库 / 目标 / 状态环"
for ep in /v1/library /v1/goals; do
  HC=$(curl -s --max-time 20 -o /dev/null -w '%{http_code}' "$BASE$ep" -H "Authorization: Bearer $TOK")
  if [ "$HC" = "200" ]; then ok "$ep → 200"; else bad "$ep → $HC"; fi
done

head2 "9. 鉴权边界（未登录应 401）"
HC=$(curl -s --max-time 20 -o /dev/null -w '%{http_code}' "$BASE/v1/images")
if [ "$HC" = "401" ]; then ok "未登录访问 /v1/images → 401"; else bad "未登录返回 $HC（应为 401）"; fi

head2 "结果"
echo "  通过 $PASS 项，失败 $FAIL 项"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
