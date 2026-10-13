#!/bin/bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
[[ $(uname -s) == Darwin ]] || { echo 'Build this on the Apple Silicon Mac or a macOS CI runner.' >&2; exit 1; }
if [[ ${CI:-} == true ]]; then
  TRANSPORT_CHECK=$(mktemp -t t3oxide-transport.XXXXXX)
  trap 'rm -f "$TRANSPORT_CHECK"' EXIT
  xcrun swiftc -swift-version 5 -parse-as-library "$ROOT/mac/Model.swift" "$ROOT/mac/Transport.swift" "$ROOT/tests/TransportTests.swift" -o "$TRANSPORT_CHECK"
  "$TRANSPORT_CHECK"
  rm -f "$TRANSPORT_CHECK"
  trap - EXIT
fi
APP="$ROOT/dist/t3oxide.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
xcrun swiftc -swift-version 5 -parse-as-library -O -whole-module-optimization \
  -target arm64-apple-macosx14.0 -framework AppKit \
  "$ROOT/mac/Model.swift" "$ROOT/mac/Transport.swift" "$ROOT/mac/App.swift" \
  -o "$APP/Contents/MacOS/t3oxide"
cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>t3oxide</string>
<key>CFBundleIdentifier</key><string>io.github.xon333.t3oxide</string>
<key>CFBundleName</key><string>t3oxide</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.1.1</string>
<key>CFBundleVersion</key><string>2</string>
<key>LSMinimumSystemVersion</key><string>14.0</string>
<key>NSHighResolutionCapable</key><true/>
<key>NSPrincipalClass</key><string>NSApplication</string>
</dict></plist>
PLIST
SOURCE_REVISION=$(git -C "$ROOT" rev-parse --verify HEAD)
/usr/libexec/PlistBuddy -c "Add :T3OxideSourceRevision string $SOURCE_REVISION" "$APP/Contents/Info.plist"
cp "$ROOT/NOTICE" "$APP/Contents/Resources/NOTICE"
/usr/bin/codesign --force --sign - --timestamp=none "$APP"
/usr/bin/ditto -c -k --keepParent "$APP" "$ROOT/dist/t3oxide-macos-arm64.zip"
echo "$APP"
