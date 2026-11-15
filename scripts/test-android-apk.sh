#!/usr/bin/env bash
# Real Gradle packaging test with a disposable signer; never installs or uploads.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
: "${JAVA_HOME:?set JAVA_HOME to JDK 17-24}"
: "${ANDROID_HOME:?set ANDROID_HOME to SDK36}"
scratch=$(mktemp -d)
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1])" "$scratch"' EXIT
export LEGATO_ANDROID_RELEASE_TAG=v0.0.1
unset LEGATO_ANDROID_KEYSTORE LEGATO_ANDROID_STORE_PASSWORD LEGATO_ANDROID_KEY_ALIAS LEGATO_ANDROID_KEY_PASSWORD
if mobile/gradlew -p mobile --no-daemon --no-configuration-cache help > "$scratch/missing.log" 2>&1; then
  echo 'Official build accepted missing signer' >&2
  exit 1
fi
grep -q 'LEGATO_ANDROID_KEYSTORE must be nonempty' "$scratch/missing.log"
export LEGATO_ANDROID_KEYSTORE="$scratch/test.jks"
export LEGATO_ANDROID_STORE_PASSWORD=test-only-password
export LEGATO_ANDROID_KEY_PASSWORD=test-only-password
export LEGATO_ANDROID_KEY_ALIAS=test
"$JAVA_HOME/bin/keytool" -genkeypair -keystore "$LEGATO_ANDROID_KEYSTORE" \
  -storepass "$LEGATO_ANDROID_STORE_PASSWORD" -keypass "$LEGATO_ANDROID_KEY_PASSWORD" \
  -alias test -keyalg RSA -keysize 2048 -validity 2 -dname 'CN=Disposable APK Test' >/dev/null 2>&1
export LEGATO_ANDROID_CERT_SHA256
LEGATO_ANDROID_CERT_SHA256=$("$JAVA_HOME/bin/keytool" -list -v -keystore "$LEGATO_ANDROID_KEYSTORE" \
  -storepass "$LEGATO_ANDROID_STORE_PASSWORD" | sed -n 's/.*SHA256: //p' | tr -d ':')
for tag in v0.0.1 v0.1.0; do
  export LEGATO_ANDROID_RELEASE_TAG="$tag"
  mobile/gradlew -p mobile --no-daemon --no-configuration-cache --max-workers=2 \
    :app:testReleaseUnitTest :app:assembleRelease
  apk=mobile/app/build/outputs/apk/release/app-release.apk
  python3 scripts/android-release.py verify "$tag" "$apk"
  unzip -p "$apk" assets/palettes.json > "$scratch/palettes.json"
  cmp themes/palettes.json "$scratch/palettes.json"
done
echo 'Two real APKs verified: same signer, increasing version codes, packaged palette. Device installation/data retention NOT tested.'
