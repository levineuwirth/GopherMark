package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
	path string
}

func OpenReadOnly(dbPath string) (*DB, error) {
	uri := fmt.Sprintf("file:%s?mode=ro&nolock=1&immutable=1&_query_only=1&_timeout=5000", dbPath)

	conn, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(10 * time.Second)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{
		conn: conn,
		path: dbPath,
	}, nil
}

func (db *DB) Close() error {
	if db.conn != nil {
		return db.conn.Close()
	}
	return nil
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}
