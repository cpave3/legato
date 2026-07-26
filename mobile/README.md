# Legato Mobile

Android-first Expo shell for managing multiple Legato servers.

## Fast path

Install Node, pnpm, Java 17+, and the Android SDK, then set `ANDROID_HOME`:

```sh
task mobile:apk
```

The command installs the frozen dependencies, generates the Android project,
builds a debug APK, and prints its path. Use `task mobile:device` to install and
launch on a connected phone. `task mobile:eas` creates an internal EAS APK.

The APK is a standalone release build with its JavaScript and assets embedded,
so it runs without Metro or an internet connection. Re-running the task and
installing with `adb install -r` updates it while preserving app data.

For the faster development loop, run `task mobile:dev-apk`, install the debug
APK once, then run `task mobile:dev`. That build connects to Metro over the LAN
and supports Fast Refresh; the standalone APK remains independent of Metro.

The app accepts the QR URI printed by `legato pair`. When machines share a
configuration hostname, advertise each machine's actual reachable address:

```sh
legato pair --host 192.168.1.20
```

The app's editable connection name distinguishes Desktop, Laptop, and other
instances independently of hostname or IP. Tokens are stored in Android secure
storage; connection names and URLs are stored locally outside the WebView.
Legato defaults to HTTP for local-network development. Treat the token and all
traffic as visible to other devices on that network.

For HTTPS, the generated certificate includes current local interface IP
addresses. Expo Go cannot use Android's user-installed CA store, so it cannot connect to
Legato's locally issued HTTPS certificates. Build the Legato APK with
`task mobile:eas` (no local Android SDK required) or `task mobile:apk`. The
Legato build opts into both Android's system and user CA stores; after installing
the CA offered by a connection, restart the app and connect using the exact IP
address advertised by `legato pair --host`.
