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
swiftc -O -swift-version 5 \
  -framework Cocoa -framework WebKit -framework UserNotifications \
  -o "$APP/Contents/MacOS/纸团" \
  desktop/EnsembleDesktop.swift

echo "==> 打包资源"
cp bin/ensemble "$APP/Contents/MacOS/ensemble"
cp -R web/app web/admin web/site "$APP/Contents/Resources/web/"
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

# 本地自签名，避免 Gatekeeper 直接拦。
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "（签名跳过，本机运行不受影响）"

echo "==> 完成：$ROOT/$APP"
echo "    启动：open \"$ROOT/$APP\""
