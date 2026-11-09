-- Messages an agent is launched with (its brief kickoff, the person's first
-- message), kept until delivered, so a restart of Legato between the launch
-- and the agent tool being ready does not lose them. owner is the Legato
-- process delivering them; a process that starts later takes them over.
CREATE TABLE IF NOT EXISTS launch_messages (
    execution_id TEXT NOT NULL,
    position     INTEGER NOT NULL,
    task_id      TEXT NOT NULL,
    slot         TEXT NOT NULL DEFAULT '',
    tmux_session TEXT NOT NULL,
    agent_kind   TEXT NOT NULL DEFAULT '',
    prompt_file  TEXT NOT NULL DEFAULT '',
    message      TEXT NOT NULL,
    owner        TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (execution_id, position)
);
