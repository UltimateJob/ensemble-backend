#!/usr/bin/env bash
# 备份/恢复的往返测试：备份 → 改数据 → 恢复 → 数据应该和备份时一致
set -e
cd "$(dirname "$0")/.."

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

export DATA_DIR="$TMP/data"
export BACKUP_DIR="$TMP/backups"
mkdir -p "$DATA_DIR/messages"

echo '{"id":"c1","title":"备份前"}' > "$DATA_DIR/conversations.json"
echo '[{"id":"m1"}]' > "$DATA_DIR/messages/c1.json"

./scripts/backup.sh 3 > /dev/null
BUNDLE=$(ls -1 "$BACKUP_DIR"/ensemble-*.tar.gz | head -1)

# 改掉数据，模拟「误删」
echo '{"id":"c1","title":"被改坏了"}' > "$DATA_DIR/conversations.json"
rm -f "$DATA_DIR/messages/c1.json"

./scripts/restore.sh "$BUNDLE" > /dev/null

TITLE=$(python3 -c "import json;print(json.load(open('$DATA_DIR/conversations.json',encoding='utf-8'))['title'])" 2>/dev/null || cat "$DATA_DIR/conversations.json")
if [ "$TITLE" != "备份前" ]; then
  echo "✗ 恢复后标题不对：$TITLE"
  exit 1
fi
if [ ! -f "$DATA_DIR/messages/c1.json" ]; then
  echo "✗ 恢复后消息文件没了"
  exit 1
fi
if [ -f "$DATA_DIR/.lock" ]; then
  echo "✗ 备份里的锁文件应该被删掉"
  exit 1
fi

echo "备份/恢复往返正常"
