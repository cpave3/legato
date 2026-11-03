---
title: Manticore token usage
description: Report the tokens your Claude Code agents use to a Manticore instance, and see today's total in the TUI.
sidebar:
  order: 70
---

Manticore records the tokens that its clients use on language models. Claude Code sends its requests straight to Anthropic, so Manticore never sees them. With a `manticore` block in your config, Legato adds a small plugin to each Claude Code session it starts, and the plugin reports each request's token counts to Manticore after the fact. The TUI then shows the tokens used today.

## Set it up

1. In Manticore, make a client for Legato and copy its key.
2. Add the instance to `config.yaml`:

   ```yaml
   manticore:
     url: http://192.168.1.220:3456
     api_key: ${LEGATO_MANTICORE_KEY}   # the Manticore client's key
   ```

3. Restart Legato. Agents you start from then on report their usage.

`manticore.url` must be an `http` or `https` address. Set both settings or neither: Legato refuses to start with only one.

Reporting works for Claude Code sessions that Legato starts. It does not need [Claude Code's native bridge](/legato/agent-tools/claude-code/), so it works with `adapters.claude-code.native_bridge: false` too.

## What is reported

The plugin sends one record for each model request that got an answer, the requests of subagents included:

- the model that answered;
- the prompt tokens, with the tokens read from and written to the cache counted apart;
- the output tokens;
- how long the request took, and how long until its first token;
- why the model stopped.

It groups a task's requests by its Legato task ID. It never reads or sends a prompt, a reply, a tool's input or a file. It does not change the request.

The plugin sends in the background, so a slow or unreachable Manticore never delays the agent. When Manticore can't be reached, the plugin keeps the records and sends them with the next ones. It keeps up to 2,000 records in memory and drops the oldest past that.

Some requests are not reported: requests that failed or were stopped before the first answer, the request Claude Code makes to compact a conversation, and requests made outside a turn. Manticore records tokens, not money.

## Tokens used today

With the `manticore` block set, the TUI's agents view shows the tokens used today, from local midnight: the total, and under it what went in and what came out. The ensemble shows the same in its header, and draws the tokens of each of the last 24 hours. Legato reads the numbers when it starts and every minute after.

:::note
The numbers are for the whole Manticore instance: every client it records, not only this Legato. The input count includes the part of each prompt read from a cache.
:::
