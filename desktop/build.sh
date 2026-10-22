#!/bin/bash
# 构建 TeamWise 桌面端（原生 WebKit 壳，无 Electron 依赖）。
#
# 产出：desktop/build/TeamWise.app
# 用法：./desktop/build.sh
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"
APP="desktop/build/TeamWise.app"

echo "==> 编译前端后端"
make build >/dev/null

echo "==> 编译桌面壳"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
swiftc -O -swift-version 5 \
  -framework Cocoa -framework WebKit \
  -o "$APP/Contents/MacOS/TeamWise" \
  desktop/EnsembleDesktop.swift

echo "==> 打包二进制"
cp bin/ensemble "$APP/Contents/MacOS/ensemble"

cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>TeamWise</string>
  <key>CFBundleDisplayName</key><string>智团 TeamWise</string>
  <key>CFBundleIdentifier</key><string>com.ultimatejob.teamwise.desktop</string>
  <key>CFBundleExecutable</key><string>TeamWise</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.0.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>NSAppTransportSecurity</key>
  <dict><key>NSAllowsLocalNetworking</key><true/></dict>
</dict>
</plist>
PLIST

# 本地自签名，避免 Gatekeeper 直接拦
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || echo "（签名跳过，本机运行不受影响）"

echo "==> 完成：$ROOT/$APP"
echo "    启动：open $ROOT/$APP"
