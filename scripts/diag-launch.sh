#!/bin/bash
# 严格对照：逐步加复杂度，定位 open 到底卡在哪一步。
#
#   A. 最小 Swift app，无签名
#   B. 最小 Swift app，adhoc 签名
#   C. 最小 Swift app + 额外一个 Mach-O 放在 Contents/MacOS/（模拟我们的结构）
#   D. 我们的 App（去掉额外的 ensemble 二进制）
set -uo pipefail

ROOT=/tmp/launch-diag
rm -rf "$ROOT"
mkdir -p "$ROOT"

say() { echo ""; echo "=== $* ==="; }

make_app() {
  local dir="$1" name="$2" extra_macho="${3:-}"
  mkdir -p "$dir/Contents/MacOS"
  cat > "$ROOT/main.swift" <<'SWIFT'
import Cocoa
let app = NSApplication.shared
app.setActivationPolicy(.regular)
let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 420, height: 260),
                 styleMask: [.titled, .closable], backing: .buffered, defer: false)
w.title = "LaunchTest"
w.makeKeyAndOrderFront(nil)
app.activate(ignoringOtherApps: true)
app.run()
SWIFT
  swiftc -O -framework Cocoa -o "$dir/Contents/MacOS/$name" "$ROOT/main.swift" 2>/dev/null
  if [ -n "$extra_macho" ]; then
    cp "$extra_macho" "$dir/Contents/MacOS/helper"
  fi
  cat > "$dir/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>${name}</string>
  <key>CFBundleExecutable</key><string>${name}</string>
  <key>CFBundleIdentifier</key><string>com.test.$(echo "$name" | tr 'A-Z' 'a-z')</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>CFBundleVersion</key><string>1</string>
</dict>
</plist>
PLIST
}

try_open() {
  local app="$1" label="$2"
  pkill -f "LaunchTest" 2>/dev/null
  sleep 0.4
  local out
  out=$(open "$app" 2>&1)
  local rc=$?
  sleep 1.5
  if pgrep -f "MacOS/LaunchTest" >/dev/null 2>&1; then
    echo "  ✓ $label 能打开"
    pkill -f "MacOS/LaunchTest" 2>/dev/null
    return 0
  else
    echo "  ✗ ${label} 打不开（exit=${rc}）${out}"
    return 1
  fi
}

say "A. 最小 app，不签名，放在 ~/Applications"
A="$HOME/Applications/LaunchTestA.app"
rm -rf "$A"
make_app "$A" LaunchTest
try_open "$A" "A"

say "B. 最小 app，adhoc 签名"
B="$HOME/Applications/LaunchTestB.app"
rm -rf "$B"
make_app "$B" LaunchTest
codesign --force --sign - "$B" 2>/dev/null
try_open "$B" "B"

say "C. 最小 app + 额外 Mach-O 在 Contents/MacOS/（模拟我们的结构）"
C="$HOME/Applications/LaunchTestC.app"
rm -rf "$C"
HELPER="/Users/mac/DevelopWorkspace/ensemble-backend/bin/ensemble"
[ -x "$HELPER" ] || HELPER="$(command -v ls)"
make_app "$C" LaunchTest "$HELPER"
codesign --force --sign - "$C/Contents/MacOS/helper" 2>/dev/null
codesign --force --sign - "$C" 2>/dev/null
try_open "$C" "C"

say "D. 我们的 App（原样）"
D="/Users/mac/DevelopWorkspace/ensemble-backend/desktop/build/纸团.app"
if [ -d "$D" ]; then
  pkill -f "MacOS/纸团" 2>/dev/null
  sleep 0.4
  out=$(open "$D" 2>&1); rc=$?
  sleep 3
  if pgrep -f "MacOS/纸团" >/dev/null 2>&1; then
    echo "  ✓ D 能打开"
    pkill -f "MacOS/纸团" 2>/dev/null
  else
    echo "  ✗ D 打不开（exit=${rc}）${out}"
  fi
else
  echo "  跳过（$D 不存在）"
fi

say "E. 我们的 App 去掉 ensemble 二进制后"
E="$HOME/Applications/LaunchTestE.app"
rm -rf "$E"
if [ -d "$D" ]; then
  cp -R "$D" "$E"
  rm -f "$E/Contents/MacOS/ensemble"
  codesign --force --sign - "$E" 2>/dev/null
  pkill -f "MacOS/纸团" 2>/dev/null
  sleep 0.4
  out=$(open "$E" 2>&1); rc=$?
  sleep 3
  if pgrep -f "MacOS/纸团" >/dev/null 2>&1; then
    echo "  ✓ E 能打开（说明是额外二进制的问题）"
    pkill -f "MacOS/纸团" 2>/dev/null
  else
    echo "  ✗ E 打不开（exit=${rc}）${out}"
  fi
fi

say "清理"
rm -rf "$HOME/Applications/LaunchTestA.app" "$HOME/Applications/LaunchTestB.app" \
       "$HOME/Applications/LaunchTestC.app" "$HOME/Applications/LaunchTestE.app"
echo "  已清理测试 App"
