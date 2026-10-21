CREATE TABLE IF NOT EXISTS maintainers (
    id INTEGER PRIMARY KEY CHECK (id > 0),
    name TEXT NOT NULL,
    email TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS applications (
    id INTEGER PRIMARY KEY CHECK (id > 0),
    title TEXT NOT NULL,
    version TEXT NOT NULL,
    company TEXT NOT NULL DEFAULT '',
    website TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    license TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    maintainer_id INTEGER NOT NULL REFERENCES maintainers(id) ON DELETE RESTRICT
) STRICT;

CREATE INDEX IF NOT EXISTS applications_maintainer_id_idx
    ON applications (maintainer_id);
