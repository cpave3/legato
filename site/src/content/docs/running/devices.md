---
title: Your phone and other devices
description: Reach Legato's web UI from a phone, a tablet or another computer, and pair the Legato app.
sidebar:
  order: 30
---

Legato's web server serves the whole web UI, so any device that can reach it can follow and answer your agents: a phone on the sofa, a tablet, or a laptop in another room. On Android, the Legato app adds pairing by four words, a list of servers, dictation and signed approvals.

First turn on the web server: `web.enabled: true` with the TUI, or `legato serve`. See [On your workstation](/legato/running/workstation/) and [Web server only](/legato/running/web-server/).

## On the same network

Open the machine's address and port in the device's browser, for example `http://192.168.1.20:3080`. The page asks for the token, which `legato auth token` prints on the machine.

By default the web server speaks plain HTTP. Other devices on the network can see the token and everything the page sends. Turn on TLS (below) on any network you do not trust.

### Found on the network

Legato announces its web server on the local network over mDNS, so the Legato app lists it under **On this network** when you add a server. It announces only on interfaces a phone can reach, not on loopback, Docker, bridge, VM or VPN interfaces. Some networks, such as guest Wi-Fi, block these announcements; then type the address.

To stop announcing:

```yaml
web:
  discovery: false
```

## Pair the Legato app

Pairing gives the app the server's address, its token and, over HTTPS, the certificate to trust. You type four words; the token never crosses the network in the clear.

1. Start a pairing on the machine:

   ```bash
   legato pair
   ```

   Or, in the web UI, open Settings › Connection › Pair a phone and choose **Pair a device**.
2. Legato shows a QR code and four words, such as `matchstick obnoxious eruption hybrid`.
3. In the app, tap **+** to add a server.
4. Scan the code, or pick the server under **On this network** (or type its address) and type the four words. The first three letters of each word are enough.
5. Tap **Pair**. The app opens the server.

The words work for one phone, for five minutes, and for five tries. `legato pair` waits until a phone pairs, and you can stop it with `ctrl+c`.

When the phone must use an address other than the one Legato picks, for example a Tailscale name or one of several machines that share a hostname, name it:

```bash
legato pair --host 192.168.1.20
```

To pair another Legato as a main instance rather than a phone, use `legato pair --main`; see [Main and worker instances](/legato/running/instances/).

`legato pair` needs a running web server for pairing by words. Without one, it prints a QR code and the token instead; that code carries the token itself, so keep it private.

## The Legato app for Android

The app is a native Android shell around the web UI, for Android 10 and later. On top of the page, it gives you:

- A list of your Legato servers, each with its own name, and pairing by four words or a QR code. The app scans the code with its own camera, so it works without Google Play services, on GrapheneOS for example. It asks for the camera the first time you scan.
- Tokens kept encrypted with a key in the Android Keystore.
- Dictation: the app records for the page, over HTTP too, where a browser has no microphone.
- Talking with the Reeve: the app listens for the page's voice sheet. Music pauses while you talk, and the conversation ends when you leave the app. On a worker's page, the voice button opens the main's page with the sheet open. The app learns which of your servers is the main by asking each one; with a single server, that server is the main.
- The page kept alive when you switch apps, and brought back where it was when Android reclaims it.
- Approval links: an approval notification from ntfy opens the app on that approval. See [Notifications](/legato/integrations/notifications/).
- Signed approvals: the phone holds a key in its secure hardware and signs a decision after your fingerprint or PIN. See [How credentials reach agents](/legato/security/credential-broker/).
- An approvals list across all your servers.
- Adding a worker from another server the app knows: on a main's Settings › Handoff › Workers › **Add a worker**, choose one of the app's other servers and agree in the app's own dialog. The app opens the pairing request on that server with the token it holds, so you type no address or words. See [Main and worker instances](/legato/running/instances/).

There is no download of the app yet. Build it from the repository with the Android SDK and a JDK from 17 to 24:

```bash
task mobile:apk       # build a release APK and print its path
task mobile:install   # build it and install it on the connected phone
```

See [`mobile/README.md`](https://github.com/cpave3/legato/blob/main/mobile/README.md) for signing and how pairing works.

## Install the web app

On any other device, including an iPhone, you can install the web UI as an app from the browser's menu ("Install app" or "Add to Home Screen"). The browser allows this only over HTTPS, or on `localhost`.

## More than one server

Both the web UI and the Legato app keep a list of servers.

- In the web UI, add one in Settings › Connection › Servers with a name and its address, such as `https://legato-vm.example.ts.net:3080`. The page asks for that server's token when you switch to it. Switch between servers with the switcher at the foot of the side rail, or in **More** on a phone.
- In the app, add each server with **+** and pair it.
- The [desktop app](/legato/running/desktop-app/) keeps every server in one window, with a dock to switch between them.

Give each Legato a tint in Settings › Appearance › Tint, so you can tell at a glance which one a page is on. The tint is saved on the server, so every device shows it.

## HTTPS with Legato's own certificate

Turn on TLS and Legato makes its own certificate authority and a server certificate:

```yaml
web:
  tls:
    enabled: true
    hostname: mybox.local   # optional: one more name for the certificate
```

- The files are in the `certs` folder of the data folder: `~/.local/share/legato/certs/` unless you set `data_dir`.
- The server certificate names `localhost`, the loopback addresses, every local network address of the machine, and `web.tls.hostname`. Legato makes a new one when these change or it nears its end, and keeps the same authority.
- The Legato app needs nothing installed: it trusts the certificate it received when it paired.
- A browser needs the authority installed before it trusts the page. Copy `certs/ca.pem` to the device and install it as a CA certificate. On Android: Settings → Security → Install a certificate → CA certificate. On iOS: install the profile, then turn on full trust in Settings → General → About → Certificate Trust Settings.

Settings › Connection › TLS certificate in the web UI repeats these steps.

## HTTPS with a certificate of your own

Give Legato a certificate and its key, and it uses them instead:

```yaml
web:
  tls:
    cert: /home/you/.config/legato/tls.crt
    key: /home/you/.config/legato/tls.key
```

Legato reads the files when it starts. Restart it after you renew them.

## From anywhere, with Tailscale

To reach Legato away from home, put the machine on your [Tailscale](https://tailscale.com) network and use its tailnet name. Nothing is opened to the internet, and only your own devices can connect.

Tailscale also gives the machine a real certificate, so browsers trust the page with nothing installed:

```bash
sudo tailscale cert --cert-file ~/.config/legato/tls.crt --key-file ~/.config/legato/tls.key legato-vm.example.ts.net
sudo chown "$USER": ~/.config/legato/tls.*
chmod 600 ~/.config/legato/tls.key
```

Point `web.tls.cert` and `web.tls.key` at these files. The certificate lasts 90 days, so run the command again from a monthly timer.

:::danger
Do not open Legato's port to the internet. Anyone who reaches it with the token can run commands through your agents.
:::

## The address in notifications

Approval notifications carry a link that opens the server. Tell Legato the address your phone uses:

```yaml
web:
  public_url: https://legato-vm.example.ts.net:3080
```

Without it, Legato uses `https://<web.tls.hostname>:<port>` when TLS has a hostname, and otherwise the first local network address it announces.
