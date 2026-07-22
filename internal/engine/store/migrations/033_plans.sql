CREATE TABLE plans (
    id              TEXT PRIMARY KEY,
    task_id         TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    title           TEXT NOT NULL,
    summary         TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'proposed',
    latest_revision INTEGER NOT NULL DEFAULT 0,
    approved_at     DATETIME,
    rejected_at     DATETIME,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    UNIQUE (task_id, name)
);
CREATE INDEX idx_plans_status ON plans(status, updated_at DESC);

CREATE TABLE plan_revisions (
    id            TEXT PRIMARY KEY,
    plan_id       TEXT NOT NULL,
    revision      INTEGER NOT NULL,
    markdown      TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
    UNIQUE (plan_id, revision)
);
CREATE INDEX idx_plan_revisions_plan ON plan_revisions(plan_id, revision DESC);

CREATE TABLE plan_questions (
    id                  TEXT PRIMARY KEY,
    plan_id             TEXT NOT NULL,
    revision_id         TEXT NOT NULL,
    question_key        TEXT NOT NULL,
    kind                TEXT NOT NULL,
    prompt              TEXT NOT NULL,
    rationale           TEXT NOT NULL DEFAULT '',
    required            INTEGER NOT NULL DEFAULT 0,
    options_json        TEXT NOT NULL DEFAULT '[]',
    recommended_json    TEXT NOT NULL DEFAULT '[]',
    created_at          DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
    FOREIGN KEY (revision_id) REFERENCES plan_revisions(id) ON DELETE CASCADE,
    UNIQUE (revision_id, question_key)
);
CREATE INDEX idx_plan_questions_revision ON plan_questions(revision_id);

CREATE TABLE plan_responses (
    id            TEXT PRIMARY KEY,
    plan_id       TEXT NOT NULL,
    revision_id   TEXT NOT NULL,
    question_id   TEXT NOT NULL,
    values_json   TEXT NOT NULL DEFAULT '[]',
    text          TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
    FOREIGN KEY (revision_id) REFERENCES plan_revisions(id) ON DELETE CASCADE,
    FOREIGN KEY (question_id) REFERENCES plan_questions(id) ON DELETE CASCADE,
    UNIQUE (question_id)
);

CREATE TABLE plan_comments (
    id              TEXT PRIMARY KEY,
    plan_id         TEXT NOT NULL,
    revision_id     TEXT NOT NULL,
    body            TEXT NOT NULL,
    selection_start INTEGER,
    selection_end   INTEGER,
    selected_text   TEXT NOT NULL DEFAULT '',
    prefix          TEXT NOT NULL DEFAULT '',
    suffix          TEXT NOT NULL DEFAULT '',
    submitted_at    DATETIME,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
    FOREIGN KEY (revision_id) REFERENCES plan_revisions(id) ON DELETE CASCADE
);
CREATE INDEX idx_plan_comments_revision ON plan_comments(revision_id, created_at);

CREATE TABLE plan_transcript (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    plan_id      TEXT NOT NULL,
    revision_id  TEXT NOT NULL,
    thread_id    TEXT NOT NULL,
    kind         TEXT NOT NULL,
    author       TEXT NOT NULL,
    body         TEXT NOT NULL,
    delivered_at DATETIME,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE,
    FOREIGN KEY (revision_id) REFERENCES plan_revisions(id) ON DELETE CASCADE
);
CREATE INDEX idx_plan_transcript_plan ON plan_transcript(plan_id, id);
