#!/bin/sh
# Builds, tests or installs the Legato Android app (mobile/).
#   apk      build the release APK and print its path
#   install  build it, install it over the app on the connected device, start it
#   test     run the app's unit tests
set -eu

mode="${1:-apk}"
root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mobile_dir="$root_dir/mobile"

# Gradle 8.14 runs on Java 17 to 24. Use the first JDK in that range.
java_major() {
  "$1/bin/java" -version 2>&1 | sed -n 's/.*version "\([0-9]*\).*/\1/p' | head -1
}
usable_jdk() {
  [ -x "$1/bin/java" ] || return 1
  major=$(java_major "$1")
  [ -n "$major" ] && [ "$major" -ge 17 ] && [ "$major" -le 24 ]
}
path_jdk=""
if command -v java >/dev/null 2>&1; then
  path_jdk=$(dirname "$(dirname "$(readlink -f "$(command -v java)")")")
fi
for candidate in \
  "${JAVA_HOME:-}" \
  "$path_jdk" \
  /home/linuxbrew/.linuxbrew/opt/openjdk@17/libexec \
  /home/linuxbrew/.linuxbrew/opt/openjdk@21/libexec \
  /opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home \
  /opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home \
  "/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
  /opt/android-studio/jbr \
  "$HOME/android-studio/jbr"; do
  if [ -n "$candidate" ] && usable_jdk "$candidate"; then
    export JAVA_HOME="$candidate"
    break
  fi
done
if [ -z "${JAVA_HOME:-}" ] || ! usable_jdk "$JAVA_HOME"; then
  echo "missing prerequisite: a JDK from 17 to 24 (set JAVA_HOME)" >&2
  exit 1
fi

android_sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
for candidate in "$HOME/Library/Android/sdk" "$HOME/Android/Sdk"; do
  if [ -z "$android_sdk" ] && [ -d "$candidate" ]; then android_sdk="$candidate"; fi
done
if [ -z "$android_sdk" ]; then
  echo "ANDROID_HOME or ANDROID_SDK_ROOT must point to the Android SDK" >&2
  exit 1
fi
export ANDROID_HOME="$android_sdk"

gradle() {
  "$mobile_dir/gradlew" -p "$mobile_dir" --console=plain "$@"
}

case "$mode" in
  test)
    gradle :app:testDebugUnitTest
    exit 0
    ;;
  apk|install)
    gradle :app:assembleRelease
    ;;
  *)
    echo "usage: $0 apk|install|test" >&2
    exit 2
    ;;
esac

apk_path="$mobile_dir/app/build/outputs/apk/release/app-release.apk"
test -f "$apk_path"
echo "$apk_path"

if [ "$mode" = "install" ]; then
  adb="$ANDROID_HOME/platform-tools/adb"
  if [ ! -x "$adb" ]; then
    echo "adb not found at $adb" >&2
    exit 1
  fi
  if ! "$adb" get-state >/dev/null 2>&1; then
    echo "no single authorized Android device connected (set ANDROID_SERIAL to pick one)" >&2
    "$adb" devices -l >&2
    exit 1
  fi
  "$adb" install -r "$apk_path"
  "$adb" shell am force-stop dev.legato.mobile
  "$adb" shell am start -n dev.legato.mobile/.MainActivity
fi
