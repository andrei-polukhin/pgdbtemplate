package pgdbtemplate

import (
	"context"
	"database/sql"
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
	conn           PgDatabaseConnection
	connStringFunc func(databaseName string) string
}

// NewStandardPgConnectionProvider creates a new StandardPgConnectionProvider.
func NewStandardPgConnectionProvider(conn PgDatabaseConnection, connStringFunc func(databaseName string) string) *StandardPgConnectionProvider {
	return &StandardPgConnectionProvider{
		conn:           conn,
		connStringFunc: connStringFunc,
	}
}

// PgConnect creates a connection to the specified database.
func (p *StandardPgConnectionProvider) Connect(ctx context.Context, databaseName string) (PgDatabaseConnection, error) {
	return p.conn, nil
}

// PgGetConnectionString returns the connection string for a database.
func (p *StandardPgConnectionProvider) GetConnectionString(databaseName string) string {
	return p.connStringFunc(databaseName)
}
