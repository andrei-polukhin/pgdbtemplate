package pgdbtemplate

import (
	"context"
	"database/sql"

	_ "github.com/lib/pq"
)

// StandardPgDatabaseConnection wraps a standard database/sql connection.
type StandardPgDatabaseConnection struct {
	*sql.DB
}

func (c *StandardPgDatabaseConnection) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.DB.ExecContext(ctx, query, args...)
}

func (c *StandardPgDatabaseConnection) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.DB.QueryRowContext(ctx, query, args...)
}

func (c *StandardPgDatabaseConnection) Close() error {
	return c.DB.Close()
}

// StandardPgConnectionProvider provides PostgreSQL connections.
type StandardPgConnectionProvider struct {
	connStringFunc func(databaseName string) string
}

// NewStandardPgConnectionProvider creates a new StandardPgConnectionProvider.
func NewStandardPgConnectionProvider(connStringFunc func(databaseName string) string) *StandardPgConnectionProvider {
	return &StandardPgConnectionProvider{
		connStringFunc: connStringFunc,
	}
}

// Connect creates a connection to the specified database.
func (p *StandardPgConnectionProvider) Connect(ctx context.Context, databaseName string) (PgDatabaseConnection, error) {
	connString := p.connStringFunc(databaseName)
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &StandardPgDatabaseConnection{DB: db}, nil
}

// PgGetConnectionString returns the connection string for a database.
func (p *StandardPgConnectionProvider) GetConnectionString(databaseName string) string {
	return p.connStringFunc(databaseName)
}
