#!/bin/sh
set -eu

mode="${1:-apk}"
root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mobile_dir="$root_dir/mobile"

for command_name in node pnpm java; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "missing prerequisite: $command_name" >&2
    exit 1
  fi
done
android_sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [ -z "$android_sdk" ] && [ -d "${HOME}/Library/Android/sdk" ]; then
  android_sdk="${HOME}/Library/Android/sdk"
fi
if [ -z "$android_sdk" ] && [ -d "${HOME}/Android/Sdk" ]; then
  android_sdk="${HOME}/Android/Sdk"
fi
if [ -z "$android_sdk" ]; then
  echo "ANDROID_HOME or ANDROID_SDK_ROOT must point to the Android SDK" >&2
  exit 1
fi
export ANDROID_HOME="$android_sdk"

cd "$mobile_dir"
pnpm install --frozen-lockfile
pnpm exec expo prebuild --platform android

if [ "$mode" = "device" ]; then
  pnpm exec expo run:android --device
  exit 0
fi
if [ "$mode" != "apk" ]; then
  echo "usage: $0 apk|device" >&2
  exit 2
fi

cd android
./gradlew assembleDebug
apk_path="$mobile_dir/android/app/build/outputs/apk/debug/app-debug.apk"
test -f "$apk_path"
echo "$apk_path"
