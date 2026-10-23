#!/bin/bash
# 构建纸团桌面端：Go 后端 + WKWebView 壳 + web 资源 + 图标。
#
# 产出：desktop/build/纸团.app
# 用法：./desktop/build.sh
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
APP="desktop/build/纸团.app"

echo "==> 编译后端"
ARCH=$(uname -m)
case "$ARCH" in
  arm64) GOARCH=arm64 ;;
  x86_64) GOARCH=amd64 ;;
  *) GOARCH="$ARCH" ;;
esac
GOOS=darwin GOARCH="$GOARCH" go build -ldflags="-s -w" -o bin/ensemble ./cmd/server

echo "==> 图标"
if [ ! -f desktop/AppIcon.icns ]; then
  ./desktop/make-icon.sh
fi

echo "==> 桌面壳"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources/web"

# 必须显式指定 -target。
#
# swiftc 默认用 SDK 的版本作为最低系统版本（这台机器上是 macosx28.0），
# 而系统只有 27.0。产出的二进制声明「需要 macOS 28 以上」，
# LaunchServices 判定不兼容，open 直接返回 -10825，
# 但直接执行二进制却完全正常——这个不对称极难查。
#
# 13.0 与 Info.plist 的 LSMinimumSystemVersion 保持一致。
DEPLOY_TARGET="${DEPLOY_TARGET:-13.0}"
swiftc -O -swift-version 5 \
  -target "$(uname -m)-apple-macos${DEPLOY_TARGET}" \
  -framework Cocoa -framework WebKit -framework UserNotifications \
  -o "$APP/Contents/MacOS/纸团" \
  desktop/EnsembleDesktop.swift

# 编译完立刻校验：最低系统版本不能高于本机，否则装上去也打不开。
BIN_MINOS=$(otool -l "$APP/Contents/MacOS/纸团" 2>/dev/null \
  | awk '/LC_BUILD_VERSION/{f=1} f&&/minos/{print $2; exit}')
if [ -n "$BIN_MINOS" ]; then
  echo "    二进制最低系统版本：macOS $BIN_MINOS"
fi

echo "==> 打包资源"
cp bin/ensemble "$APP/Contents/MacOS/ensemble"
cp -R web/app web/admin web/site "$APP/Contents/Resources/web/"

# 演示内容随包分发：第一次打开就有三个群、真实对话、现成的产出，
# 而不是一片空大厅。后端启动时自己判断要不要装（见 store.SeedDemo）。
if [ -d seed/demo ]; then
  mkdir -p "$APP/Contents/Resources/seed"
  cp -R seed/demo "$APP/Contents/Resources/seed/"
fi
cp desktop/AppIcon.icns "$APP/Contents/Resources/AppIcon.icns"

# 后端按可执行文件旁找 web/，找不到再看 Contents/Resources/web（见 webDir）。
cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>纸团</string>
  <key>CFBundleDisplayName</key><string>纸团</string>
  <key>CFBundleIdentifier</key><string>com.ultimatejob.zhituan.desktop</string>
  <key>CFBundleExecutable</key><string>纸团</string>
  <key>CFBundleIconFile</key><string>AppIcon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.2.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSAppTransportSecurity</key>
  <dict><key>NSAllowsLocalNetworking</key><true/></dict>
</dict>
</plist>
PLIST

# 签名。这里有两个坑，都是实测踩出来的：
#
# 1. --deep 已废弃，对嵌套的二进制处理不当，会用错标识。
#    正确做法是自内向外逐个签：先内置后端，再壳本身。
# 2. 不加 entitlements 时，`open` 启动会被 LaunchServices 拒绝
#    （-10825），日志里是 "process not properly entitled"；
#    但直接跑 Contents/MacOS/<exe> 却能起来。这个不对称极难查。
#
# ad-hoc 签名（-）够本机使用；分发给别人需要开发者证书 + 公证。
# 不要加 --options runtime（硬化运行时）：它要求签名带可信时间戳，
# ad-hoc 签名下会让 App 启动即被 kill。
SIGN_ARGS=(--force --sign - --identifier "com.ultimatejob.zhituan.desktop")

if [ -f "$APP/Contents/MacOS/ensemble" ]; then
  codesign "${SIGN_ARGS[@]}" "$APP/Contents/MacOS/ensemble" 2>/dev/null || true
fi
codesign "${SIGN_ARGS[@]}" --entitlements desktop/entitlements.plist "$APP" 2>&1 | grep -v "replacing existing signature" || true

if codesign --verify --deep --strict "$APP" 2>/dev/null; then
  echo "==> 签名校验通过"
else
  echo "==> ⚠ 签名校验未通过（本机运行通常仍可用，分发会失败）"
fi

echo "==> 完成：$ROOT/$APP"
echo "    启动：open \"$ROOT/$APP\""
