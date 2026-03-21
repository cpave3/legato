-- Create workspaces table
CREATE TABLE workspaces (
    id         INTEGER PRIMARY KEY,
    name       TEXT UNIQUE NOT NULL,
    color      TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0
);

-- Add workspace_id foreign key to tasks
ALTER TABLE tasks ADD COLUMN workspace_id INTEGER REFERENCES workspaces(id);

CREATE INDEX idx_tasks_workspace_id ON tasks(workspace_id);
