#!/usr/bin/env bash
set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_DIR="$DIR/src-tauri/target/release/bundle/macos"
DMG_DIR="$DIR/src-tauri/target/release/bundle/dmg"
APP_NAME="opentermius.app"
DMG_NAME="OpenTermius_0.7.3_aarch64.dmg"
STAGING_DIR="/tmp/opentermius-dmg-staging"
RW_DMG="/tmp/opentermius-rw.dmg"

if [ -z "${TAURI_SIGNING_PRIVATE_KEY:-}" ] && [ -f "$HOME/.tauri/opentermius.key" ]; then
  export TAURI_SIGNING_PRIVATE_KEY="$HOME/.tauri/opentermius.key"
fi
export TAURI_SIGNING_PRIVATE_KEY_PASSWORD="${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}"

echo "1. Building release binary..."
cd "$DIR"
npx tauri build --bundles app

echo "1b. Re-signing macOS app & updater archive with bound CFBundleIdentifier..."
xattr -cr "$APP_DIR/$APP_NAME" || true
codesign --force --deep --identifier "com.opentermius.client" --sign - "$APP_DIR/$APP_NAME"
codesign --verify --deep --strict "$APP_DIR/$APP_NAME"
rm -f "$APP_DIR/$APP_NAME.tar.gz" "$APP_DIR/$APP_NAME.tar.gz.sig"
COPYFILE_DISABLE=1 tar -czf "$APP_DIR/$APP_NAME.tar.gz" -C "$APP_DIR" "$APP_NAME"
if [ -n "${TAURI_SIGNING_PRIVATE_KEY:-}" ]; then
  KEY_PATH="$TAURI_SIGNING_PRIVATE_KEY"
  (
    unset TAURI_SIGNING_PRIVATE_KEY
    if [ -f "$KEY_PATH" ]; then
      npx tauri signer sign -f "$KEY_PATH" -p "${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}" "$APP_DIR/$APP_NAME.tar.gz"
    else
      npx tauri signer sign -k "$KEY_PATH" -p "${TAURI_SIGNING_PRIVATE_KEY_PASSWORD:-}" "$APP_DIR/$APP_NAME.tar.gz"
    fi
  )
fi

echo "2. Preparing clean staging & ad-hoc signing..."
rm -rf "$STAGING_DIR" "$RW_DMG"
mkdir -p "$STAGING_DIR" "$DMG_DIR"

ditto "$APP_DIR/$APP_NAME" "$STAGING_DIR/OpenTermius.app"
xattr -cr "$STAGING_DIR/OpenTermius.app" || true
codesign --force --deep --identifier "com.opentermius.client" --sign - "$STAGING_DIR/OpenTermius.app"
codesign --verify --deep --strict "$STAGING_DIR/OpenTermius.app"
ln -s /Applications "$STAGING_DIR/Applications"

echo "3. Creating macOS drag-to-Applications DMG installer..."
if [ -d "/Volumes/OpenTermius" ]; then
  hdiutil detach "/Volumes/OpenTermius" -force || true
fi
rm -f "$DMG_DIR/$DMG_NAME"
diskutil image create from --format UDRW --volumeName "OpenTermius" "$STAGING_DIR" "$RW_DMG"

MOUNT_DIR=$(hdiutil attach -readwrite -noverify -noautoopen "$RW_DMG" | grep -E '/Volumes/OpenTermius' | sed 's/.*\(\/Volumes\/.*\)/\1/' | head -n 1)
if [ -n "$MOUNT_DIR" ]; then
  osascript <<EOF || true
tell application "Finder"
  tell disk "OpenTermius"
    open
    set current view of container window to icon view
    set toolbar visible of container window to false
    set statusbar visible of container window to false
    set the bounds of container window to {200, 160, 780, 520}
    set viewOptions to the icon view options of container window
    set arrangement of viewOptions to not arranged
    set icon size of viewOptions to 112
    set position of item "OpenTermius.app" of container window to {160, 175}
    set position of item "Applications" of container window to {420, 175}
    close
    open
    update without registering applications
    delay 2
  end tell
end tell
EOF
  sync
  hdiutil detach "$MOUNT_DIR" -quiet || hdiutil detach "$MOUNT_DIR" -force
fi

hdiutil convert "$RW_DMG" -format UDZO -imagekey zlib-level=9 -o "$DMG_DIR/$DMG_NAME"
rm -rf "$STAGING_DIR" "$RW_DMG"

echo "✓ OpenTermius DMG installer successfully created:"
echo "  $DMG_DIR/$DMG_NAME"
