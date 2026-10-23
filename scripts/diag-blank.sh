#!/bin/bash
# 诊断桌面壳白屏：清残留进程 → 检查端口 → 直接打接口看返回。
set -uo pipefail
cd "$(dirname "$0")/.."

echo "== 1. 残留的后端进程 =="
pgrep -fl 'bin/ensemble|MacOS/ensemble' | head -10

echo ""
echo "== 2. 监听端口 =="
lsof -iTCP -sTCP:LISTEN -n -P 2>/dev/null | grep -E ':(80[89][0-9])' | head -5

echo ""
echo "== 3. 直接打 8080/8081 =="
for p in 8080 8081; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${p}/app/" 2>/dev/null || echo "conn-fail")
  echo "  127.0.0.1:${p}/app/ → ${code}"
done

echo ""
echo "== 4. 打包里的 web 资源 =="
APP="desktop/build/纸团.app"
ls "$APP/Contents/Resources/web/" 2>/dev/null || echo "  没有 Resources/web"
echo "  index.html: $([ -f "$APP/Contents/Resources/web/app/index.html" ] && echo 存在 || echo 缺失)"
echo "  ensemble:   $([ -x "$APP/Contents/MacOS/ensemble" ] && echo 存在 || echo 缺失)"

echo ""
echo "== 5. 壳的最新日志 =="
tail -6 "$HOME/Library/Application Support/纸团/ensemble.log" 2>/dev/null || echo "  无日志"
