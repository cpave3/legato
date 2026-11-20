---
title: Home Assistant over MQTT
description: Use stable agent counts and Attention indicators in Home Assistant automations.
sidebar:
  order: 65
---

Legato can publish ten read-only aggregate entities through Home Assistant MQTT discovery. Use these entities for dashboards, lights, or phone notifications. The aggregate publisher is off by default. A separate opt-in adds a **Server theme** select that changes the preferred theme for clients using **Follow server**.

## Set up the broker

1. Configure the MQTT integration in Home Assistant for your broker. Enable discovery with the default `homeassistant` prefix, or use the same prefix in both applications.
2. Create a broker login for Legato. Give it permission to publish its state root and discovery configurations. Allow subscriptions only to `homeassistant/status` (or your discovery prefix followed by `/status`).
3. Store the login through [Legato's credential store](/legato/security/credential-broker/). The secret must be a JSON object with nonempty `username` and `password` strings. Ask the Reeve to open a secure secret form, then paste the JSON into that form, not the conversation. Alternatively, supply the JSON through standard input to `legato secret add mqtt-login --type userpass`. Use a secure input source; do not put the login in shell history, `config.yaml`, or the broker URL.
4. Open **Settings › Security › Credentials**, choose **Add a credential**, and name the entry `mqtt`. Set **Secret** to `legato://mqtt-login`, **Profile** to `mqtt` and **Type** to `userpass`. **Edit as YAML** shows the same entry as `config.yaml` has it:

   ```yaml
   name: mqtt
   profile: mqtt
   type: userpass
   secret: legato://mqtt-login
   ```

   Replace the secret reference with the name used in step 3. The `mqtt` profile is host-only: agents cannot request this login. Set `type: userpass` explicitly, including when using the secure form or an existing `env://`, `file://`, or `op://` secret.
5. Open **Settings › Integrations › Home Assistant**. Set **Broker** to your broker's address and **Login** to `mqtt` (the credential entry's name, not its secret reference or password). Empty credential explicitly selects anonymous access; in that case, skip steps 2–4.

   `mqtt://` uses TCP without encryption; `mqtts://` verifies the server certificate and hostname against the system trust store. Default ports are 1883 and 8883. WebSockets, client certificates, and insecure TLS are not supported.
6. Set **MQTT discovery prefix** to match Home Assistant (`homeassistant` by default). **MQTT base topic** and **MQTT device name** are optional. Leave **Include Reeve in MQTT counts** and **MQTT Attention details** off unless you need them. Open **Broker, credentials and privacy** for defaults and retained-data consequences.
7. Turn on **MQTT publisher** after setting the broker and credential. Text fields save on blur or Enter; toggles save when clicked. Save errors appear beside the setting. A lock means `config.yaml` pins the value: edit the file or use **Move to the database (restarts Legato)** when ready for a restart.
8. Restart Legato when you choose from **Settings → Updates**. All nine MQTT settings and credential entries take effect at the next restart; saving does not connect to the broker. Changed values show **applies at restart**. Configuration loading does not read the credential.
9. Open Home Assistant's MQTT integration. Find the device named **Legato <hostname>**, or your configured device name.

### Configure through a file instead

These are the equivalent file settings. File values pin the controls in Settings.

```yaml
credentials:
  - name: mqtt
    profile: mqtt
    type: userpass
    secret: legato://mqtt-login

integrations:
  mqtt:
    enabled: true
    theme_control: false
    broker: mqtts://mqtt.example.net:8883
    credential: mqtt
    discovery_prefix: homeassistant
    base_topic: legato/workstation
    device_name: Legato workstation
    include_reeve: false
    attention_details: false
```

If `base_topic` is empty, Legato uses `legato/<persistent-instance-id>`. The default device name is `Legato <hostname>`. Identity does not change when the device name changes. Independent instances must have different state roots.

## Entities

| Entity | Meaning |
| --- | --- |
| Agents running | All sessions with status `running`, regardless of activity. |
| Agents working | Running sessions with activity `working`. |
| Agents waiting | Running sessions with activity `waiting`. |
| Agents idle | Running sessions with empty activity. |
| Agents background | Running sessions with activity `background`. |
| Agents error | Running sessions with activity `error`. |
| Needs attention | On when the default-policy Attention list has open items. |
| Attention items | The number of open default-policy Attention items. |
| Agent waiting on you | On when an included running agent waits for input. |
| Pending approvals | All pending approvals, including questions and credential requests. |

Agent counts include named slots, swarm agents, and workflow agents. They exclude the Reeve unless `include_reeve` is true. Stopped, dead, and interrupted sessions do not count. Idle does not mean completed. Attention and approvals remain instance-wide, including Reeve items. Their counts can differ from agent counts.

Legato publishes changed values after events, normally within two seconds. It republishes discovery and current state after reconnecting or receiving Home Assistant's `online` birth message. Custom birth topics are not supported.

## Control the server theme

1. Complete broker and credential setup above. Use dedicated broker accounts and `mqtts://` on an untrusted network.
2. Add broker ACLs for theme control. In the table below, `<base>` is your MQTT base topic, `<discovery>` is the discovery prefix, and `<instance-id>` is Legato's persistent identity from its discovery topics. Keep existing aggregate permissions if you use the publisher too.

   | Account | Subscribe/read | Publish/write |
   | --- | --- | --- |
   | Legato | Exact `<base>/theme/set`, `<discovery>/status` | `<discovery>/select/<instance-id>/theme/config`, `<base>/theme/state`, `<base>/theme/result`, `<base>/theme/availability` |
   | Home Assistant | That select discovery path and theme state/result/availability | Exact `<base>/theme/set`, plus its existing `<discovery>/status` birth topic |

3. In **Settings › Integrations › Home Assistant**, turn on **Let Home Assistant set the theme** (`integrations.mqtt.theme_control`). This grants theme changes to **every publisher your broker allows on the exact command topic**. Legato cannot identify the sender or revoke an individual publisher. Restrict writers through broker ACLs; avoid anonymous or wildcard write access. No Legato application token is used.
4. Restart Legato when you choose. Saving does not enable control live. **MQTT publisher** can stay off if you want only theme control. Find the automatically discovered **Server theme** select on the Legato device; do not create a manual select.
5. Choose `none`, `legato`, `dracula`, `ocular-light`, `ocular-dark`, `ocular-light-vivid` or `ocular-dark-vivid`. `none` clears the server preference. Each browser or app must separately choose **Follow server** in its app theme picker; existing local choices stay local.

The select waits for saved state rather than assuming a command succeeded. It uses state attributes for the current revision, a connection nonce from discovery, and the command time. Home Assistant must support MQTT select `command_template`, JSON state attributes and `this.attributes` in that template. Keep Home Assistant and Legato clocks synchronized.

### Topics and command limits

| Topic under `<base>/theme/` | Payload | Delivery |
| --- | --- | --- |
| `set` | `{"id":"dracula","session":"<current discovery nonce>","expected_revision":3,"issued_at":1794744000}` | QoS 0, not retained |
| `state` | `{"id":"dracula","revision":4}` (`none` when cleared) | Retained, authoritative |
| `result` | `{"code":"ok"}` | Not retained |
| `availability` | `online` or `offline` | Retained, separate from aggregates |

Custom controllers must replace the example nonce with the current discovery nonce and `issued_at` with integer Unix seconds at send time. Commands must be UTF-8 JSON of at most 2 KiB with exactly those four fields. Commands expire after 30 seconds; Legato allows five seconds of future clock skew. A fresh nonce on every broker connection rejects old-session commands. Revision conflicts reject stale or duplicate writes; every saved change advances the revision. Results report `ok`, `conflict`, `retained`, `invalid`, `session`, `expired`, `unknown_theme` or `unavailable`.

Legato uses clean MQTT v5 sessions, no offline command queue, no retained-command replay, and rejects retained commands sent while connected. It accepts at most five commands per second, burst five, with a five-entry queue; excess traffic can be dropped without a result. Oversized messages that reach the controller return `invalid` without retaining their payload bytes. Reconnect refreshes discovery and current state, not commands. Logs do not print raw command payloads. This grants no other settings or command execution.

To revoke a controller, remove its broker write permission. Turning off theme control in Legato takes effect at your next chosen restart. Old retained select discovery, state and command messages are not automatically deleted: remove only this instance's topics with your broker administration tools. Home Assistant history needs separate cleanup.

## Notify your phone

Home Assistant assigns entity IDs when it discovers the device. Find the **Needs attention** entity, then replace the example entity ID and phone action below:

```yaml
alias: Legato needs attention
triggers:
  - trigger: state
    entity_id: binary_sensor.legato_workstation_needs_attention
    from: "off"
    to: "on"
actions:
  - action: notify.mobile_app_your_phone
    data:
      title: Legato
      message: Legato needs your attention.
```

The explicit `from: "off"` prevents a notification when availability changes from unavailable to on after a restart. This automation notifies once per all-clear-to-attention transition, not for each new Attention item.

## Availability and ownership

The aggregate entities become available only after Legato publishes a usable snapshot. The theme select has separate availability. On graceful shutdown, Legato publishes `offline`. After an unexpected loss, the broker publishes the retained Last Will. The keepalive is 15 seconds; loss detection is not instantaneous.

Availability describes the publisher's source snapshot and broker connection. It does not claim that every agent or external integration is healthy.

The TUI publishes even with its web server disabled. When a TUI and `legato serve` share a data directory, only one process publishes. The other logs that the publisher is inactive. Automatic owner failover is not supported; a non-owner needs an operator-controlled restart after the owner stops.

## Privacy and retained data

By default, Legato publishes only counts and states. `attention_details: true` adds at most 20 generated signal labels to the Attention items attributes. The attributes include `total` and `truncated`; the payload stays below 8 KiB. Legato never exports source titles, task IDs, paths, prompts, transcripts, approval contents, commands, or resource names.

MQTT retained messages and Home Assistant history can outlive Legato. When you turn detail export off and restart, Legato clears the retained attributes and removes their discovery link.

When you disable the integration or change topic roots, retained discovery and old state remain on the broker. Remove only this instance's discovery configurations and state topics with your broker's administration tools. Do not clear another instance's topics. Remove unwanted Home Assistant history separately.

## Troubleshooting

- **No entities:** check that Legato owns the publisher lock, discovery prefixes match, and broker ACLs allow discovery publication and the HA birth subscription.
- **Credential error:** check the configured name, `profile: mqtt`, `type: userpass`, and JSON login format. Logs report the failure without printing the login.
- **TLS error:** install a trusted broker certificate with the correct hostname. There is no insecure TLS switch.
- **Unavailable after a source error:** Legato keeps existing values, marks availability offline, and retries. It does not publish false zero counts.
- **Broker restarted:** Legato reconnects with a fresh credential read and publishes the latest snapshot. It does not replay intermediate states.

See the [configuration reference](/legato/reference/configuration/) for all MQTT settings.
