---
title: Notifications
description: Hear when an agent is waiting for you or needs an approval, on your phone through ntfy or on your desktop.
sidebar:
  order: 60
---

Legato can tell you when an agent stops and waits for you, when it has an error, and when an agent asks for an approval. It sends phone notifications through [ntfy](https://ntfy.sh) and desktop notifications through your operating system. The web UI can also notify you of approvals in the browser.

## Phone notifications with ntfy

ntfy delivers a message posted to a topic to every phone subscribed to that topic. You can use the public server at ntfy.sh or run your own.

1. Install the ntfy app on your phone and subscribe to a topic. Pick a name that is hard to guess, since anyone who knows a topic on a public server can read it.
2. Add the topic to `config.yaml`:

   ```yaml
   notifications:
     ntfy:
       url: https://ntfy.sh        # your ntfy server
       topic: legato-7f3k2q        # the topic to post to
       token: ${NTFY_TOKEN}        # optional, for a protected topic
   ```

3. Restart Legato.

| Setting | What it does | Default |
| --- | --- | --- |
| `notifications.ntfy.url` | The ntfy server. | `https://ntfy.sh` |
| `notifications.ntfy.topic` | The topic to post to. Without it, Legato sends nothing to ntfy. | none |
| `notifications.ntfy.token` | An access token for a protected topic. | none |
| `notifications.approvals` | Send a notification for each new approval. | `true` |
| `notifications.os.enabled` | Show desktop notifications. | `false` |

### Choose which agents notify you

Agent notifications are off for each task until you turn them on:

- In the TUI's agents view, select the agent and press `n`.
- In the web UI, open the agent's menu and choose **Enable notify**.

Both appear only when an ntfy topic is set. With notifications on, you get one when the task's agent goes from working to waiting or idle, and one when it has an error. Legato sends at most one notification for a task every five minutes.

### Approvals

When an agent asks for an approval, such as permission to run a command or to use a credential, Legato sends one ntfy notification. It names the task and the agent tool, never the command. Approval notifications don't depend on the per-task switch. To turn them off, set `notifications.approvals: false`.

Tapping the notification opens the approval in the Legato app on your phone. The link names your Legato server by:

1. `web.public_url`, when you set it.
2. Otherwise `https://<web.tls.hostname>:<port>`, when TLS has a host name.
3. Otherwise the first local network address Legato finds.

Set `web.public_url` if your phone reaches Legato by another address. See [Devices](/legato/running/devices/).

### Away tasks

On a main instance that [hands tasks off](/legato/using/handoff/), Legato sends one ntfy notification when an away task fails or is lost on its worker, when a worker no longer accepts your main, and when a worker that holds your tasks has been unreachable for 15 minutes (once for each outage). Tapping it opens the task, or Settings › Handoff › Workers, in your browser. A task done on the worker, or one whose agent asks you something, sends nothing from your main: the worker's own notifications cover those. To turn them off, set `notifications.handoff: false`.

## Desktop notifications

Set `notifications.os.enabled: true` to show a desktop notification each time any agent starts waiting, goes idle or has an error. Desktop notifications don't use the per-task switch. Legato uses `notify-send` on Linux and `osascript` on macOS, and shows nothing on other systems.

## Browser notifications for approvals

The web UI shows a browser notification for each approval that arrives while its tab is not in front. Clicking it brings the tab forward with the approval open. This needs a secure page (HTTPS or `localhost`), and the Legato app on a phone uses ntfy instead. To turn it off for one browser, go to **Settings › Security** and clear **Notify me of approvals**.
