#!/usr/bin/env bash
# Generates the Android project files (if missing) and applies ChamaChain's settings.
# Run from the mobile folder. Safe to run more than once.
set -euo pipefail
cd "$(dirname "$0")/.."

if [[ ! -d android ]]; then
  flutter create --project-name chamachain_member --org com.chamachain --platforms android .
fi

MANIFEST=android/app/src/main/AndroidManifest.xml
[[ -f "$MANIFEST" ]] || { echo "Missing $MANIFEST"; exit 1; }

# Release builds need the internet permission (Flutter only adds it to debug builds)
if ! grep -q 'android.permission.INTERNET' "$MANIFEST"; then
  sed -i 's|<manifest xmlns:android="http://schemas.android.com/apk/res/android"[^>]*>|&\n    <uses-permission android:name="android.permission.INTERNET"/>|' "$MANIFEST"
fi

# App name shown under the icon
sed -i 's|android:label="[^"]*"|android:label="ChamaChain"|' "$MANIFEST"

# Allow plain http only so members can test against a computer on their own Wi-Fi.
# The app itself refuses http for any address outside the local network (see lib/format.dart).
if ! grep -q 'usesCleartextTraffic' "$MANIFEST"; then
  sed -i 's|<application|<application android:usesCleartextTraffic="true"|' "$MANIFEST"
fi

grep -q 'android.permission.INTERNET' "$MANIFEST" || { echo "Could not add INTERNET permission"; exit 1; }
echo "Android project ready."
