package storage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// LogEvent represents a single captured log line.
type LogEvent struct {
	ID        int64
	SessionID int64
	Timestamp int64  // unix ms
	Stream    string // "stdout" or "stderr"
	Level     string
	Message   string
	Service   string
}

// SessionInfo represents a tracked process session.
type SessionInfo struct {
	ID        int64
	Service   string
	PID       int
	Command   string
	StartedAt int64
	EndedAt   *int64 // nil if still active
	Hostname  string
}

// Store wraps a SQLite database connection for all storage operations.
type Store struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at dbPath, enables WAL mode and
// synchronous=NORMAL, and ensures the schema tables exist.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// WAL mode for concurrent readers.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// NORMAL sync is a good trade-off between safety and speed.
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set synchronous mode: %w", err)
	}

	for _, stmt := range schemaStatements {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("execute schema statement: %w", err)
		}
	}

	return &Store{db: db}, nil
}

// Close shuts down the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// CreateSession inserts a new session and returns its ID.
func (s *Store) CreateSession(service string, pid int, command string, hostname string) (int64, error) {
	startedAt := time.Now().UnixMilli()
	result, err := s.db.Exec(
		`INSERT INTO sessions (service, pid, command, started_at, hostname) VALUES (?, ?, ?, ?, ?)`,
		service, pid, command, startedAt, hostname,
	)
	if err != nil {
		return 0, fmt.Errorf("insert session: %w", err)
	}
	return result.LastInsertId()
}

// EndSession sets the ended_at timestamp on a session.
func (s *Store) EndSession(id int64, endedAt int64) error {
	_, err := s.db.Exec(`UPDATE sessions SET ended_at = ? WHERE id = ?`, endedAt, id)
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	return nil
}

// InsertLogEvents batch-inserts log events inside a single transaction.
func (s *Store) InsertLogEvents(events []LogEvent) error {
	if len(events) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT INTO log_events (session_id, timestamp, stream, level, message, service) VALUES (?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range events {
		if _, err := stmt.Exec(e.SessionID, e.Timestamp, e.Stream, e.Level, e.Message, e.Service); err != nil {
			return fmt.Errorf("insert log event: %w", err)
		}
	}

	return tx.Commit()
}

// QueryRecentLogs returns the most recent N logs for a service, ordered by
// timestamp descending.
func (s *Store) QueryRecentLogs(service string, limit int) ([]LogEvent, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, timestamp, stream, level, message, service
		 FROM log_events
		 WHERE service = ?
		 ORDER BY timestamp DESC
		 LIMIT ?`,
		service, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent logs: %w", err)
	}
	defer rows.Close()

	return scanLogEvents(rows)
}

// SearchLogs searches log messages with LIKE, optionally filtered by a since
// timestamp (unix ms). Pass since=0 to skip the time filter.
func (s *Store) SearchLogs(service string, query string, since int64) ([]LogEvent, error) {
	likePattern := "%" + query + "%"

	var rows *sql.Rows
	var err error

	if since > 0 {
		rows, err = s.db.Query(
			`SELECT id, session_id, timestamp, stream, level, message, service
			 FROM log_events
			 WHERE service = ? AND message LIKE ? AND timestamp >= ?
			 ORDER BY timestamp DESC`,
			service, likePattern, since,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, session_id, timestamp, stream, level, message, service
			 FROM log_events
			 WHERE service = ? AND message LIKE ?
			 ORDER BY timestamp DESC`,
			service, likePattern,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("search logs: %w", err)
	}
	defer rows.Close()

	return scanLogEvents(rows)
}

// QueryErrors returns stderr lines and lines where level contains "error"
// (case insensitive), filtered by since timestamp. Pass since=0 to skip
// the time filter.
func (s *Store) QueryErrors(service string, since int64) ([]LogEvent, error) {
	var rows *sql.Rows
	var err error

	if since > 0 {
		rows, err = s.db.Query(
			`SELECT id, session_id, timestamp, stream, level, message, service
			 FROM log_events
			 WHERE service = ?
			   AND (stream = 'stderr' OR LOWER(level) LIKE '%error%')
			   AND timestamp >= ?
			 ORDER BY timestamp DESC`,
			service, since,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, session_id, timestamp, stream, level, message, service
			 FROM log_events
			 WHERE service = ?
			   AND (stream = 'stderr' OR LOWER(level) LIKE '%error%')
			 ORDER BY timestamp DESC`,
			service,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query errors: %w", err)
	}
	defer rows.Close()

	return scanLogEvents(rows)
}

// QuerySessions lists sessions, optionally filtered by service. Pass an empty
// string to return all sessions.
func (s *Store) QuerySessions(service string) ([]SessionInfo, error) {
	var rows *sql.Rows
	var err error

	if service != "" {
		rows, err = s.db.Query(
			`SELECT id, service, pid, command, started_at, ended_at, hostname
			 FROM sessions
			 WHERE service = ?
			 ORDER BY started_at DESC`,
			service,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, service, pid, command, started_at, ended_at, hostname
			 FROM sessions
			 ORDER BY started_at DESC`,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]SessionInfo, 0)
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.ID, &si.Service, &si.PID, &si.Command, &si.StartedAt, &si.EndedAt, &si.Hostname); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, si)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return sessions, nil
}

// QueryServices returns distinct service names from the sessions table.
func (s *Store) QueryServices() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT service FROM sessions ORDER BY service`)
	if err != nil {
		return nil, fmt.Errorf("query services: %w", err)
	}
	defer rows.Close()

	services := make([]string, 0)
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			return nil, fmt.Errorf("scan service: %w", err)
		}
		services = append(services, svc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate services: %w", err)
	}
	return services, nil
}

// PruneByAge deletes log events and closed sessions older than maxAgeDays.
// Returns the total number of deleted rows.
func (s *Store) PruneByAge(maxAgeDays int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -maxAgeDays).UnixMilli()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin prune transaction: %w", err)
	}
	defer tx.Rollback()

	res1, err := tx.Exec(`DELETE FROM log_events WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune log events: %w", err)
	}
	eventsDeleted, _ := res1.RowsAffected()

	res2, err := tx.Exec(`DELETE FROM sessions WHERE ended_at IS NOT NULL AND ended_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune sessions: %w", err)
	}
	sessionsDeleted, _ := res2.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit prune: %w", err)
	}

	return eventsDeleted + sessionsDeleted, nil
}

// Checkpoint forces a WAL checkpoint to reclaim disk space.
func (s *Store) Checkpoint() error {
	_, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// scanLogEvents extracts LogEvent rows from a query result set.
func scanLogEvents(rows *sql.Rows) ([]LogEvent, error) {
	events := make([]LogEvent, 0)
	for rows.Next() {
		var e LogEvent
		var level sql.NullString
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Timestamp, &e.Stream, &level, &e.Message, &e.Service); err != nil {
			return nil, fmt.Errorf("scan log event: %w", err)
		}
		if level.Valid {
			e.Level = level.String
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate log events: %w", err)
	}
	return events, nil
}

