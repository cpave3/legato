---
title: Web access and tokens
description: How Legato's web server checks who may use it, how browsers and phones get the token, and how to keep the port safe.
sidebar:
  order: 40
---

Legato's web server lets whoever holds its token do anything you can do in the web UI: start and stop agents, type into their terminals, and allow what they ask. One token, kept on the machine Legato runs on, protects all of it. Treat it like a password.

## The token

Legato makes the token the first time its web server starts: 32 random bytes, written as hex to the file `auth-token` in the data folder. That is `~/.local/share/legato/auth-token` unless you set `data_dir`. Only your user can read the file.

Print it:

```bash
legato auth token
```

Replace it, for example after a device is lost or the token leaked:

```bash
legato auth regenerate
```

Every browser and phone must then sign in again. A Legato that is running reads the token when it starts, so restart it to use the new one.

## How a browser or phone gets it

- **A browser** asks for the token the first time it opens a server. Paste the output of `legato auth token`. The browser keeps it for that server, in that browser only.
- **The Legato app** gets it by pairing: you run `legato pair`, or choose Settings › Connection › Pair a phone › **Pair a device** in the web UI, and type the four words it shows into the app. The words are never sent: the phone and the server each prove they know them, and the server sends the token sealed under a key only the two of them have. See [Your phone and other devices](/legato/running/devices/).
- **A main instance** never gets it. When it pairs with this Legato as a worker, it gets a token of its own that reaches only the routes a main uses to hand tasks off, and the worker can revoke it under Settings › Handoff › Main instances. See [Main and worker instances](/legato/running/instances/).
- **A script** sends the token in the `Authorization` header:

  ```bash
  curl -H "Authorization: Bearer $(legato auth token)" http://localhost:3080/api/agents
  ```

## What needs no token

A few things answer without the token:

- `GET /health`, which says the server is up and its version, for monitoring.
- `GET /api/meta`, which says the version, the server's tint, whether it pairs by phrase, and whether a main instance can pair with it, so the app or a main can check a server before it pairs.
- `GET /api/ca-cert`, the server's CA certificate, which is public, so a device can install it before it has the token.
- The web UI's own files, so the page can load and ask for the token. The page shows nothing of yours until it has one.
- The two steps of pairing by phrase. The phrase is their check: it lasts five minutes and allows five tries.
- The image files agents, plans and reviews show. Each address is the hash of the image's content, so it cannot be guessed.

Everything else under `/api/`, and the live connection the page uses for updates, needs the token.

## Integration tokens

A service such as Argus gets a token of its own, limited to the integration routes and to the scopes you give it:

```bash
legato integration token create argus --scope owner:read,delegate,attention
```

Such a token starts with `lgi_` and reaches nothing else. `legato integration token revoke argus` stops it at once. See [Argus](/legato/integrations/argus/).

## Keep the port safe

:::danger
Never open Legato's port to the internet. Anyone who reaches it and has the token can run commands on your machine through your agents.
:::

- **Use Tailscale** to reach Legato away from home. Only your own devices can connect, and Tailscale gives the machine a certificate browsers trust. See [Your phone and other devices](/legato/running/devices/#from-anywhere-with-tailscale).
- **Turn on TLS** on any network you share. By default the web server speaks plain HTTP, and other devices on the network can read the token as it passes. Set `web.tls.enabled: true` for a certificate Legato makes itself, or `web.tls.cert` and `web.tls.key` for your own. See [HTTPS with Legato's own certificate](/legato/running/devices/#https-with-legatos-own-certificate).
- **Mind your agents.** An agent outside the [sandbox](/legato/security/sandbox/) runs as you and can read the token file. [Signed approvals](/legato/security/credential-broker/#signed-approvals) keep the decisions that matter most for a person at an enrolled phone or passkey, which a token alone cannot make.
