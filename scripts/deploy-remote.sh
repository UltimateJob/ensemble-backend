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

say "1. 确认在 main 上编译"
BRANCH=$(git rev-parse --abbrev-ref HEAD)
REV=$(git rev-parse --short HEAD)
if [ "$BRANCH" != "main" ]; then
  echo "  ✗ 当前分支是 ${BRANCH}，线上版本必须基于 main 构建"
  echo "    先合并再部署：git checkout main && git merge <分支>"
  exit 1
fi
if [ -n "$(git status --porcelain -- cmd internal web)" ]; then
  echo "  ✗ 工作区有未提交改动（cmd/internal/web），构建产物会与 main 不一致"
  git status --short -- cmd internal web | head -5
  exit 1
fi
echo "  分支 main @ ${REV}"

say "2. 编译 linux/amd64"
# 显式写入构建标记。在 worktree 里编译时 go 拿不到主仓库的 git 元数据，
# 二进制里就没有 vcs.revision，线上跑的是哪个提交事后无法追溯。
# 用 ldflags 塞一个 buildinfo 变量，比依赖 vcs 自动注入可靠。
BUILD_REV="${REV}"
BUILD_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-X main.buildRev=${BUILD_REV} -X main.buildAt=${BUILD_AT}" \
  -o /tmp/ensemble-new ./cmd/server
ls -lh /tmp/ensemble-new | awk '{print "  " $5, $9}'
echo "  构建标记：${BUILD_REV} @ ${BUILD_AT}"

if [ "$DRY" = "1" ]; then
  say "dry-run：不实际上传"
  exit 0
fi

say "3. 备份线上二进制"
bash scripts/ssh-run.sh "cp -a $REMOTE_DIR/ensemble $REMOTE_DIR/ensemble.bak-\$(date +%Y%m%d-%H%M%S) && ls -1t $REMOTE_DIR/ensemble.bak-* | head -3"

say "4. 上传"
scp -q /tmp/ensemble-new root@64.90.17.217:/tmp/ensemble-new
bash scripts/ssh-run.sh "install -m 755 /tmp/ensemble-new $REMOTE_DIR/ensemble && rm -f /tmp/ensemble-new && ls -la $REMOTE_DIR/ensemble"

say "5. 同步前端资源"
rsync -az --delete web/app/ root@64.90.17.217:$REMOTE_DIR/web/app/
rsync -az --delete web/site/ root@64.90.17.217:$REMOTE_DIR/web/site/
echo "  web/app、web/site 已同步"

# 手机端 Web 构建产物（Expo 导出）。目录不存在就跳过——
# 有些部署不需要手机端，硬要求会让整个部署失败。
if [ -d web/m ] && [ -f web/m/index.html ]; then
  rsync -az --delete web/m/ root@64.90.17.217:$REMOTE_DIR/web/m/
  echo "  web/m 已同步（$(du -sh web/m | cut -f1)）"
else
  echo "  跳过 web/m：没有构建产物"
  echo "    要部署手机端先跑：cd mobile && npx expo export --platform web --output-dir ../web/m"
fi

say "6. 重启并等待健康"
bash scripts/ssh-run.sh "systemctl restart $SERVICE && sleep 3 && systemctl is-active $SERVICE"
for i in $(seq 1 20); do
  if curl -sf -o /dev/null --max-time 3 https://teamwise.cn/healthz; then
    echo "  ✓ 线上已恢复"
    break
  fi
  sleep 1
  [ "$i" = "20" ] && { echo "  ✗ 健康检查失败，回滚"; bash scripts/ssh-run.sh "cp -a \$(ls -1t $REMOTE_DIR/ensemble.bak-* | head -1) $REMOTE_DIR/ensemble && systemctl restart $SERVICE"; exit 1; }
done

say "7. 端到端验证"
bash scripts/check-remote.sh
