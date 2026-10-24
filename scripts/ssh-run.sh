#!/bin/bash
# 在线上服务器执行一条命令。
#
# 优先用 SSH 密钥（已装好，免密）；密钥不可用时退回密码。
# 主机/用户/密码从 docs/local/INFRA.local.md 读，不写死在这里。
#
#   ./scripts/ssh-run.sh "systemctl status ensemble"
#   ./scripts/ssh-run.sh --script <<'EOF'
#   ...多条命令...
#   EOF
set -uo pipefail
cd "$(dirname "$0")/.."

INFRA="docs/local/INFRA.local.md"
if [ ! -f "$INFRA" ]; then
  echo "✗ 找不到 $INFRA（本机环境记录，不入库）"
  exit 1
fi

HOST=$(grep -oE '`[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+`' "$INFRA" | head -1 | tr -d '`')
USER_NAME=$(grep -oE 'SSH 用户 \| `[^`]+`' "$INFRA" | grep -oE '`[^`]+`' | tr -d '`')
PASS=$(grep -oE 'SSH 密码[：:]`[^`]+`' "$INFRA" | grep -oE '`[^`]+`' | tr -d '`')

if [ -z "$HOST" ] || [ -z "$USER_NAME" ]; then
  echo "✗ 无法从 $INFRA 解析出主机/用户"
  exit 1
fi

CMD="$*"
if [ "${1:-}" = "--script" ]; then
  CMD=$(cat)
fi

if [ -z "$CMD" ]; then
  echo "用法：$0 \"命令\"   或   $0 --script <<'EOF' ... EOF"
  exit 1
fi

SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=15)

# 先试密钥。BatchMode 保证没有可用密钥时立刻失败，不去等密码提示。
if ssh -o BatchMode=yes "${SSH_OPTS[@]}" "$USER_NAME@$HOST" true 2>/dev/null; then
  exec ssh "${SSH_OPTS[@]}" "$USER_NAME@$HOST" "$CMD"
fi

if [ -z "$PASS" ]; then
  echo "✗ 密钥登录失败，且 $INFRA 里没有密码可退回"
  exit 1
fi

if command -v sshpass >/dev/null 2>&1; then
  exec sshpass -p "$PASS" ssh "${SSH_OPTS[@]}" "$USER_NAME@$HOST" "$CMD"
fi

# 没有 sshpass 时用系统自带的 expect。
export SSHPASS_HOST="$HOST" SSHPASS_USER="$USER_NAME" SSHPASS_PASS="$PASS" SSHPASS_CMD="$CMD"

expect <<'EXPECT'
set timeout 120
set host $env(SSHPASS_HOST)
set user $env(SSHPASS_USER)
set pass $env(SSHPASS_PASS)
set cmd  $env(SSHPASS_CMD)

log_user 0
spawn ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 "$user@$host" $cmd
expect {
    -re "(?i)password:" { send "$pass\r"; log_user 1; exp_continue }
    -re "(?i)yes/no"    { send "yes\r"; exp_continue }
    eof
}
catch wait result
exit [lindex $result 3]
EXPECT
