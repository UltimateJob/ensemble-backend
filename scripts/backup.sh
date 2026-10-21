#!/usr/bin/env bash
# 备份 data/ 到 backups/。
#
#   ./scripts/backup.sh          # 打一个包
#   ./scripts/backup.sh 30       # 打完只保留最近 30 个
#
# data/ 是唯一的数据源，没有第二份。这个脚本是最后一道保险。
set -e
cd "$(dirname "$0")/.."

DATA_DIR="${DATA_DIR:-data}"
BACKUP_DIR="${BACKUP_DIR:-backups}"
KEEP="${1:-20}"

if [ ! -d "$DATA_DIR" ]; then
  echo "没有 $DATA_DIR 目录，没什么可备份的"
  exit 1
fi

mkdir -p "$BACKUP_DIR"

STAMP=$(date +%Y%m%d-%H%M%S)
OUT="$BACKUP_DIR/ensemble-$STAMP.tar.gz"

# 打包时跳过运行时的锁文件——它不是数据
tar -czf "$OUT" \
  --exclude='.lock' \
  -C "$(dirname "$DATA_DIR")" "$(basename "$DATA_DIR")" 2>/dev/null \
  || tar -czf "$OUT" -C . "$DATA_DIR"

SIZE=$(du -h "$OUT" | awk '{print $1}')
COUNT=$(ls -1 "$BACKUP_DIR"/ensemble-*.tar.gz 2>/dev/null | wc -l | tr -d ' ')
echo "→ $OUT  [$SIZE, total $COUNT]"

# 只保留最近 N 份
if [ "$COUNT" -gt "$KEEP" ]; then
  ls -1t "$BACKUP_DIR"/ensemble-*.tar.gz | tail -n +$((KEEP + 1)) | while read -r old; do
    rm -f "$old"
    echo "  清理旧备份 $(basename "$old")"
  done
fi

echo ""
echo "  恢复：  ./scripts/restore.sh $OUT"
echo "  定时：  crontab -e  加一行"
echo "          0 3 * * * cd $(pwd) && ./scripts/backup.sh >> backups/backup.log 2>&1"
echo ""
