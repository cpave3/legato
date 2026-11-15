---
title: Home Assistant over MQTT
description: Use stable agent counts and Attention indicators in Home Assistant automations.
sidebar:
  order: 65
---

Legato can publish ten read-only aggregate entities through Home Assistant MQTT discovery. Use these entities for dashboards, lights, or phone notifications. The integration is off by default. It accepts no commands from Home Assistant.

## Set up the broker

1. Configure the MQTT integration in Home Assistant for your broker. Enable discovery with the default `homeassistant` prefix, or use the same prefix in both applications.
2. Create a broker login for Legato. Give it permission to publish its state root and discovery configurations. Allow subscriptions only to `homeassistant/status` (or your discovery prefix followed by `/status`).
3. Store the login through [Legato's credential store](/legato/security/credential-broker/). The secret must be a JSON object with `username` and `password` strings. Both strings must be nonempty. Do not put the password in `config.yaml` or the broker URL.
4. Add the credential and publisher configuration:

   ```yaml
   credentials:
     - name: mqtt
       profile: mqtt          # host-only: agents cannot request this login
       type: userpass         # required for env/file/op-backed login values
       secret: legato://mqtt-login

   integrations:
     mqtt:
       enabled: true
       broker: mqtts://mqtt.example.net:8883
       credential: mqtt
       discovery_prefix: homeassistant
       base_topic: legato/workstation
       device_name: Legato workstation
       include_reeve: false
       attention_details: false
   ```

   Use your broker's address. `mqtt://` uses TCP without encryption; `mqtts://` verifies the server certificate and hostname against the system trust store. Default ports are 1883 and 8883. WebSockets, client certificates, and insecure TLS are not supported. An empty `credential` explicitly selects anonymous access.

   To store the login, supply the JSON through standard input to `legato secret add mqtt-login --type userpass`. Use a secure input source; do not put the login in shell history. For an existing `env://`, `file://`, or `op://` secret, set `type: userpass` explicitly.

5. Restart Legato when you choose. All eight MQTT settings take effect at the next restart. Configuration loading does not read the credential.
6. Open Home Assistant's MQTT integration. Find the device named **Legato workstation**, or your configured device name.

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

The device becomes available only after Legato publishes a usable snapshot. On graceful shutdown, Legato publishes `offline`. After an unexpected loss, the broker publishes the retained Last Will. The keepalive is 15 seconds; loss detection is not instantaneous.

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
