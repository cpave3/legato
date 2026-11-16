-- Whether each Legato-managed skill is given to agents, keyed by the skill's
-- folder name under the skills folder (skills.dir). A skill with no row is
-- on. The skills themselves are files on disk, never in the database. 114
-- and 115 are taken by open branches (LEG-214, LEG-199).
CREATE TABLE IF NOT EXISTS skill_states (
    name       TEXT PRIMARY KEY,
    enabled    INTEGER NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
