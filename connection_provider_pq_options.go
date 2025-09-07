package pgdbtemplate

import (
	"database/sql"
	"time"
)

// StandardDatabaseConnectionOption configures *sql.DB connection.
type StandardDatabaseConnectionOption func(*sql.DB)

// WithMaxOpenConns sets the maximum number of open connections.
func WithMaxOpenConns(n int) StandardDatabaseConnectionOption {
	return func(db *sql.DB) {
		db.SetMaxOpenConns(n)
	}
}

// WithMaxIdleConns sets the maximum number of connections.
// in the idle pool.
func WithMaxIdleConns(n int) StandardDatabaseConnectionOption {
	return func(db *sql.DB) {
		db.SetMaxIdleConns(n)
	}
}

// WithConnMaxLifetime sets the maximum time a connection may be reused.
func WithConnMaxLifetime(d time.Duration) StandardDatabaseConnectionOption {
	return func(db *sql.DB) {
		db.SetConnMaxLifetime(d)
	}
}

// WithConnMaxIdleTime sets the maximum time a connection may be idle.
func WithConnMaxIdleTime(d time.Duration) StandardDatabaseConnectionOption {
	return func(db *sql.DB) {
		db.SetConnMaxIdleTime(d)
	}
}
