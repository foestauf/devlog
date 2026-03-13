package storage

const createSessionsTable = `
CREATE TABLE IF NOT EXISTS sessions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    service     TEXT NOT NULL,
    pid         INTEGER NOT NULL,
    command     TEXT NOT NULL,
    started_at  INTEGER NOT NULL,
    ended_at    INTEGER,
    hostname    TEXT
);`

const createLogEventsTable = `
CREATE TABLE IF NOT EXISTS log_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  INTEGER NOT NULL REFERENCES sessions(id),
    timestamp   INTEGER NOT NULL,
    stream      TEXT NOT NULL,
    level       TEXT,
    message     TEXT NOT NULL,
    service     TEXT NOT NULL
);`

const createIndexLogEventsServiceTS = `
CREATE INDEX IF NOT EXISTS idx_log_events_service_ts ON log_events(service, timestamp);`

const createIndexLogEventsSession = `
CREATE INDEX IF NOT EXISTS idx_log_events_session ON log_events(session_id);`

const createIndexSessionsService = `
CREATE INDEX IF NOT EXISTS idx_sessions_service ON sessions(service);`

var schemaStatements = []string{
	createSessionsTable,
	createLogEventsTable,
	createIndexLogEventsServiceTS,
	createIndexLogEventsSession,
	createIndexSessionsService,
}
