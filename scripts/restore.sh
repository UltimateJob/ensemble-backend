#!/usr/bin/env bash
# 从备份恢复 data/。
#
#   ./scripts/restore.sh backups/ensemble-20260101-030000.tar.gz
#
# 会先把现有的 data/ 挪到 data.before-restore，不会直接覆盖。
set -e
cd "$(dirname "$0")/.."

SRC="$1"
DATA_DIR="${DATA_DIR:-data}"

if [ -z "$SRC" ] || [ ! -f "$SRC" ]; then
  echo "用法：./scripts/restore.sh backups/ensemble-YYYYmmdd-HHMMSS.tar.gz"
  echo ""
  echo "现有备份："
  ls -1t backups/ensemble-*.tar.gz 2>/dev/null | head -10 || echo "  （没有）"
  exit 1
fi

if lsof -ti:${ADDR##*:} >/dev/null 2>&1; then
  echo "服务还在跑，先停掉它（同目录只能有一个写入者）"
  exit 1
fi

if [ -d "$DATA_DIR" ] && [ -n "$(ls -A "$DATA_DIR" 2>/dev/null)" ]; then
  echo "→ 把现有数据挪到 $DATA_DIR.before-restore"
  rm -rf "$DATA_DIR.before-restore"
  mv "$DATA_DIR" "$DATA_DIR.before-restore"
fi

echo "→ 解包 $SRC"
# 包里的路径永远是 data/ 开头（不管源目录叫什么都统一），
# 所以 DATA_DIR 不是 data 时要先解到临时目录再搬
if [ "$DATA_DIR" = "data" ]; then
  tar -xzf "$SRC"
else
  TMPX=$(mktemp -d)
  tar -xzf "$SRC" -C "$TMPX"
  mkdir -p "$DATA_DIR"
  cp -r "$TMPX"/data/* "$DATA_DIR"/ 2>/dev/null || true
  rm -rf "$TMPX"
fi

# 备份里可能带着旧锁文件，删掉
rm -f "$DATA_DIR/.lock"

echo ""
echo "  恢复完成。启动：make run"
echo "  不满意就换回来：rm -rf $DATA_DIR && mv $DATA_DIR.before-restore $DATA_DIR"
echo ""
