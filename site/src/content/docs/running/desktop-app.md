---
title: The desktop app
description: Every Legato server in one window, each paired by four words, with approvals signed by a touch of a security key or, on a Mac, Touch ID.
sidebar:
  order: 35
---

The Legato desktop app keeps all your Legato servers in one window. A dock down the left shows each server as a tile in its tint; click one and its web UI fills the rest of the window. Each server's page stays open, so switching is instant, and a server that goes down leaves the others alone. The app also signs approvals with a touch of a security key, such as a YubiKey, or on a Mac with Touch ID, and gathers the approvals that wait for one from every server.

:::caution
The desktop app is new. The AppImage for Linux is the main build. The Mac build is an early version (see [On a Mac](#on-a-mac)), and there is no Windows build yet.
:::

## Install it

Download `legato-desktop-x86_64.AppImage` from the [latest release that has one](https://github.com/cpave3/legato/releases), make it executable, and run it:

```bash
chmod +x legato-desktop-x86_64.AppImage
./legato-desktop-x86_64.AppImage
```

The AppImage needs a Linux with glibc 2.39 or later, such as Ubuntu 24.04, Fedora 40 or Debian 13. Not every Legato release carries a desktop build: one is made only when the app has changed.

## Pair a server

The app pairs with a server by four words, the same way the Legato app for Android does, so you never copy a token.

1. On the server, run `legato pair`, or open Settings › **Pair a device** in its web UI. It shows four words.
2. In the app, click **+** in the dock. Servers announcing themselves on the network are listed under **On this network**; click one, or type the address.
3. Type the four words and click **Pair**.

The words are never sent. The app and the server each prove they know them, and the server then sends its token and its certificate, sealed so only the app can read them. From then on the app trusts that certificate for that server and nothing else, so a server with Legato's own certificate works without installing anything, and something else on the network cannot answer in its place. The token is kept in your desktop's keyring, not in a file.

A phrase lasts five minutes and five tries. Servers are found on the network as described in [Your phone and other devices](/legato/running/devices/#found-on-the-network); some networks block that, and then you type the address.

## Sign approvals with a security key

A [signed approval](/legato/security/credential-broker/#signed-approvals) needs a signature from a device you enrolled. In the app that device is a security key: you touch it to sign, and no agent can do that for you.

1. Click the key icon at the foot of the dock to open **Servers & keys**.
2. Under **Security keys**, click **Add a key**, give it a name, and touch the key when it blinks. Leave only that key plugged in. Some keys ask for their PIN once here, to make the key's credential; signing later is a touch alone.

The app enrolls each key on every server it has paired with, and on each server you pair later. A server that already has an enrolled device asks for the new key to be approved with a signature from one it knows, as it does for any new device.

When an approval waits for a signature, the title bar shows **to sign**. Click it: a sheet shows what you are about to sign, drawn from the exact request the key signs. Choose an option and touch the key. If no key the server knows is plugged in, the sheet waits for one and carries on when you plug it in. Each decision records whether it was signed by a touch alone or with a PIN or fingerprint. A Legato older than touch-only signing refuses a touch alone; the app says so, and updating the server fixes it.

A server's own page hands such an approval to the app too: in its approvals tray, **◈ Approve and sign** opens the same sheet (an older server says **Approve on this phone**). The page only names the approval; the app fetches what it shows and signs itself, so the page cannot change it.

### Every server's approvals

The button above the key icon, at the foot of the dock, counts the approvals waiting on every server, and each server's tile counts its own. Click it for the list. An approval that needs a signature opens the sheet; any other opens its server's page on it, where you answer it as in the web UI: a question, a plan, or a deny with a note for the agent.

To revoke a key, click **Revoke** beside it. Every server is asked to remove it; each removal is itself a signed approval, which you sign with another key (or that one, while you still have it).

### Touch ID on a Mac

On a Mac with Touch ID, **Servers & keys** also offers **Add Touch ID**. The Mac makes a key in its Secure Enclave, the chip that holds Touch ID, and the app enrolls it on every server as it does a security key. Nothing is asked when it is added. The key never leaves that chip, and it signs only after your fingerprint, so an agent cannot sign with it.

When a server knows Touch ID, the sheet asks for your fingerprint instead of a touch of your key, and the Mac's own dialog names the approval and the server. A decision signed this way is recorded as verified.

- If you add or remove a fingerprint in System Settings, the Mac retires the key: remove Touch ID in the app and add it again. Keep a security key enrolled as well, to sign that change and as a spare.
- With the lid closed and a keyboard without Touch ID, sign with a security key.

## The window

- The title bar is the app's own: drag it to move the window, double-click it to maximize. It names the server in front and has a button to reload its page.
- When the server in front has a [tint](/legato/using/web-ui/), the title bar, the dock and the window's edges take its colour.
- To reorder the servers, press and hold a tile and drag it up or down; the order is kept until you change it again.
- The dock's dot on each tile is green while the server answers and red while it does not.
- The app keeps no offline copy of a server's web UI. While a server is down, the app shows its own offline screen for it, with the address and the reason it could not be reached. It keeps checking every few seconds (**Try again** checks at once), and the server's page comes back by itself when the server answers.
- Dictation works in every server's page, over HTTP too: the app records for the page with `parecord` (or `arecord` when there is no `parecord`), and the server transcribes it as usual. Linux only for now.

### The voice widget

A small capsule at the edge of the screen shows the Reeve, whether your main can listen, and the key. It stays above your other windows and never takes the keyboard from the app you are in.

- Click the capsule to talk, as with the key.
- While you talk, it grows into a card: your words, the pause bar, what the Reeve is doing, and what it says. Its buttons are **Send now**, **Cancel**, **Stop** and **Reply**. **Stop** stops the Reeve and keeps the card, so you can talk again or close it.
- If the main cannot listen, the capsule turns amber and says why. **Open settings** opens that setting on the main.
- A long message does not grow the card off the screen: it shows your latest words, after an ellipsis. The whole message still goes to the Reeve.
- Drag the capsule to put it anywhere. It stays where you drop it, also after a restart: it keeps to the nearest corner, or to the middle of the nearest edge, as far from that edge as you dropped it. A click without a drag still talks.
- Right-click the capsule to hide it, put it in a corner or the middle of an edge (**Place**), or open the app's settings. **Servers & keys** has the same choice. Hidden, it still shows while you talk, and for a few seconds after to say how the conversation ended. **Servers & keys** › **Show the voice widget** brings it back.

On KDE Plasma and other Wayland desktops (when the app runs on Wayland, not through XWayland), the AppImage brings its own `gtk-layer-shell`, so the widget stays where you put it and above other windows with nothing to install. On a desktop without layer shell (GNOME), the widget is an ordinary small window: your desktop decides where it goes, and a drag moves it only until the app restarts. On a Mac it is an ordinary window for now.

## Talk to the Reeve

The app has a key that starts talking to the [Reeve](/legato/using/reeve/) from any app. On Linux with Wayland, such as KDE Plasma, the desktop asks the first time which key to use, and offers **Meta+Alt+Space**. It keeps your choice, and also gives a second key to stop talking. On X11 and Windows the key is **Meta+Alt+Space**, and on a Mac **Ctrl+Shift+Space**. These have no key to stop.

- Open **Servers & keys** › **Talking to the Reeve** to see the key. **Change the key…** opens the desktop's own shortcut settings, where the desktop has them.
- Press the key to start, and again to send. Select **Hold the key to talk** to talk only while you hold it.
- On a desktop where the key does not work, make a shortcut in the desktop's own settings that runs `xdg-open legato-desktop://voice` or `legato-desktop --voice`. It does what the key does. `legato-desktop://voice?action=cancel` or `legato-desktop --voice=cancel` stops talking.

The app talks to the Reeve of your main server. When you have several servers and none of them is the only main, choose one under **The Reeve to talk to** in **Servers & keys**. The app remembers the server, also when its address changes.

When you press the key, the app first asks the main whether it can listen. If it cannot, the app says why in one line and records nothing: for example, the Reeve's voice is off on the main, or this computer has no `parecord` or `arecord`. Otherwise it streams your voice to the main, which ends your turn when you pause. The app plays the Reeve's answer with `pw-play` (or `paplay`). It does not send your voice while it plays, so the Reeve does not hear itself. After the Reeve has spoken, you can just talk again to reply. If you say nothing, the conversation ends.

- Press the key while the Reeve speaks to stop it and reply.
- Press the key while you speak to send at once, without waiting for the pause.

Linux only for now.

## Add a worker from another server

When one of your servers should hand tasks to another ([handoff](/legato/using/handoff/)), open the main's Settings › Handoff › Workers › **Add a worker** in the app. **From this app** lists your other servers. Choose the worker, and the app asks in a dialog of its own, outside the page: "Make vm-1 a worker of workstation?" On **Make it a worker**, it opens the worker's pairing request with the token it holds for that server, and the main pairs with it. You type no address or words, and the main's page never sees the worker's token. See [Main and worker instances](/legato/running/instances/#pair-a-main-instance-with-a-worker).

## Import tickets from the browser

The [browser extension](/legato/integrations/browser-extension/#import-into-the-desktop-app) can hand the tickets you import from Jira or Burnlite to the app. Select **Open ticket imports in the Legato desktop app** in the extension's options. When you import a ticket, the app comes to the front and asks which server to import it into. It then shows that server's board with the new card picked out.

Select **Don't ask again** to always import into the server in front. To be asked again, open **Servers & keys** and select **Ask which server to import a ticket into**.

The app tells your system that it opens `legato-desktop://` links each time it starts. An AppImage can move, so open the app once after you move it.

## Updates

The app keeps itself current. It checks GitHub for a newer release with a desktop build when it starts and every six hours, and when you click **Check for updates** under **This app** in **Servers & keys**. When there is one, the title bar shows **Update to vX.Y.Z**: click it, and the app downloads the new version, checks it against the release's checksums, and puts it in place of the old one, which it keeps beside it (the AppImage as `.prev`; on a Mac, a hidden `.Legato.app.prev`). Then click **Restart to update**.

A copy built from source says it is `dev` and never updates itself.

## On a Mac

There is a disk image for Apple Silicon Macs, `legato-desktop-macos-arm64.dmg`. Drag Legato to Applications. It is not notarized, so the first time macOS refuses to open it: open System Settings › Privacy & Security and click **Open Anyway** (on macOS 14 and earlier, right-click the app and choose **Open**). For now it has limits the Linux build does not:

- A server's page loads only when the Mac trusts the server's certificate: a certificate from a public authority, or Legato's own CA. When it does not, the app says so in place of the blank page and offers the CA it pinned when it paired. **Trust it on this Mac** asks for your password and trusts that CA for that server's address only. **Save the certificate** puts it in Downloads, with the steps to trust it in Keychain Access yourself, which trusts it for every site. Pairing, keys and approvals work either way.
- It updates itself only from the Applications folder. Opened from the disk image or from Downloads, macOS runs it from a read-only copy, and the app says to move it.
- The first update may be stopped by macOS: if it says Legato was prevented from modifying apps, allow Legato in System Settings › Privacy & Security › App Management, and update again.
- Touch ID is new and has had little use on a Mac.
- The app does not record for dictation; a server's page has a microphone only over HTTPS.

## Build it yourself

See [Building Legato](/legato/contributing/build/) and the app's README on GitHub: [desktop/README.md](https://github.com/cpave3/legato/blob/main/desktop/README.md). The internals are in [docs/ai/desktop.md](https://github.com/cpave3/legato/blob/main/docs/ai/desktop.md).
