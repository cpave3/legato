## Why

When working with AI agents, a common flow is: create a local task to start working immediately, then later realize it maps to an existing Jira ticket. Currently there's no way to associate a remote ticket with an existing local task — you'd have to delete the local task (losing agent history, state durations, PR links) and import the remote one fresh. This is disruptive, especially mid-session with a running agent.

## What Changes

- Add a "bind remote ticket" action from the detail view (`i` key) that searches remote tickets and attaches one to the current local task
- `store.BindRemoteTicket(taskID, provider, remoteID, remoteMeta)` updates provider fields on an existing task **without changing its ID**
- Sync service gains `BindRemoteTicket(ctx, taskID, remoteTicketID)` that fetches the remote ticket, validates no duplicate binding, and updates the local task's provider fields
- After binding, the task participates in pull/push sync like any imported task, but retains its original local ID (e.g., `a1b2c3d4` instead of `REX-123`)
- The task's display key on cards shows the remote ID (e.g., `REX-123`) while the internal ID remains the local one
- All existing references (agent sessions, tmux sessions `legato-<localid>`, state intervals, PR meta, sync logs) remain valid — nothing is renamed or migrated

## Capabilities

### New Capabilities
- `remote-binding`: Binding a remote ticket to an existing local task without changing the task's internal ID. Covers store updates, sync participation, detail view trigger, and card display.

### Modified Capabilities
- `jira-sync`: Pull/push sync must handle tasks where `id != remote_id` (local ID retained, remote_id stored separately). Currently `ImportRemoteTask` uses `rt.ID` as the task ID — binding creates a task where these differ.
- `local-task-management`: Detail view gains `i` keybinding for local tasks to trigger remote binding. After binding, description/title editing restrictions apply (task is now remote-tracked).

## Impact

- **Store**: New `BindRemoteTicket` method; `ImportRemoteTask` duplicate check must also look at `remote_id` field (not just task `id`)
- **Sync service**: Pull sync currently looks up tasks by `id` matching `rt.ID` — must also match by `remote_id` for bound tasks. Push sync already uses `remote_id` for transitions, so should work
- **TUI detail view**: `i` keybinding added for local tasks (currently `i` is import overlay on board only)
- **Board rendering**: Card key display logic — show `remote_id` when present, fall back to `id`
- **Agent sessions / tmux**: No impact — task ID unchanged
- **State intervals**: No impact — FK references unchanged local ID
- **PR tracking**: No impact — tracks by local task ID
