package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	conn.Exec("PRAGMA journal_mode = WAL")
	conn.Exec("PRAGMA busy_timeout = 5000")

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	schema := `
CREATE TABLE IF NOT EXISTS sites (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	url TEXT NOT NULL,
	engine_type TEXT NOT NULL,
	selectors TEXT,
	cookie_file TEXT,
	enabled INTEGER NOT NULL DEFAULT 1,
	format_prompt TEXT,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	prompt TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	site_id TEXT NOT NULL,
	content TEXT,
	kept INTEGER NOT NULL DEFAULT 0,
	error TEXT,
	elapsed_ms INTEGER,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (session_id) REFERENCES sessions(id),
	FOREIGN KEY (site_id) REFERENCES sites(id)
);

CREATE TABLE IF NOT EXISTS site_cookies (
	site_id TEXT PRIMARY KEY,
	cookies TEXT,
	local_storage TEXT,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (site_id) REFERENCES sites(id)
);
`
	if _, err := conn.Exec(schema); err != nil {
		return nil, fmt.Errorf("run schema: %w", err)
	}

	migrations := []string{
		"ALTER TABLE messages ADD COLUMN turn INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE messages ADD COLUMN prompt TEXT NOT NULL DEFAULT ''",
	}
	for _, m := range migrations {
		conn.Exec(m)
	}

	return &DB{db: conn}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) Conn() *sql.DB {
	return d.db
}
