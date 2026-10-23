#!/bin/bash
# 把当前构建部署到线上。
#
#   ./scripts/deploy-remote.sh            # 编译 + 上传 + 重启
#   ./scripts/deploy-remote.sh --dry-run  # 只看会做什么
#
# 前提：本机已能免密 ssh（scripts/ssh-run.sh 用的同一套配置）。
# 安全：先在服务器上备份旧二进制，失败自动回滚。
set -euo pipefail
cd "$(dirname "$0")/.."

REMOTE_DIR="/opt/ensemble"
SERVICE="ensemble"
DRY=0
[ "${1:-}" = "--dry-run" ] && DRY=1

say() { printf '\n=== %s ===\n' "$*"; }

say "1. 编译 linux/amd64"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /tmp/ensemble-new ./cmd/server
ls -lh /tmp/ensemble-new | awk '{print "  " $5, $9}'

if [ "$DRY" = "1" ]; then
  say "dry-run：不实际上传"
  exit 0
fi

say "2. 备份线上二进制"
bash scripts/ssh-run.sh "cp -a $REMOTE_DIR/ensemble $REMOTE_DIR/ensemble.bak-\$(date +%Y%m%d-%H%M%S) && ls -1t $REMOTE_DIR/ensemble.bak-* | head -3"

say "3. 上传"
scp -q /tmp/ensemble-new root@64.90.17.217:/tmp/ensemble-new
bash scripts/ssh-run.sh "install -m 755 /tmp/ensemble-new $REMOTE_DIR/ensemble && rm -f /tmp/ensemble-new && ls -la $REMOTE_DIR/ensemble"

say "4. 同步前端资源"
rsync -az --delete web/app/ root@64.90.17.217:$REMOTE_DIR/web/app/
rsync -az --delete web/site/ root@64.90.17.217:$REMOTE_DIR/web/site/
echo "  web/app、web/site 已同步"

say "5. 重启并等待健康"
bash scripts/ssh-run.sh "systemctl restart $SERVICE && sleep 3 && systemctl is-active $SERVICE"
for i in $(seq 1 20); do
  if curl -sf -o /dev/null --max-time 3 https://teamwise.cn/healthz; then
    echo "  ✓ 线上已恢复"
    break
  fi
  sleep 1
  [ "$i" = "20" ] && { echo "  ✗ 健康检查失败，回滚"; bash scripts/ssh-run.sh "cp -a \$(ls -1t $REMOTE_DIR/ensemble.bak-* | head -1) $REMOTE_DIR/ensemble && systemctl restart $SERVICE"; exit 1; }
done

say "6. 端到端验证"
bash scripts/check-remote.sh
