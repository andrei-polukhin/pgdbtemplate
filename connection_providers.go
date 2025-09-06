package pgdbtemplate

import (
	"context"
	"database/sql"

	_ "github.com/lib/pq"
)

// StandardDatabaseConnection wraps a standard database/sql connection.
type StandardDatabaseConnection struct {
	*sql.DB
}

func (c *StandardDatabaseConnection) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.DB.ExecContext(ctx, query, args...)
}

func (c *StandardDatabaseConnection) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.DB.QueryRowContext(ctx, query, args...)
}

func (c *StandardDatabaseConnection) PingContext(ctx context.Context) error {
	return c.DB.PingContext(ctx)
}

func (c *StandardDatabaseConnection) Close() error {
	return c.DB.Close()
}

// StandardConnectionProvider provides PostgreSQL connections.
type StandardConnectionProvider struct {
	connStringFunc func(databaseName string) string
}

// NewStandardConnectionProvider creates a new StandardConnectionProvider.
func NewStandardConnectionProvider(connStringFunc func(databaseName string) string) *StandardConnectionProvider {
	return &StandardConnectionProvider{
		connStringFunc: connStringFunc,
	}
}

// Connect creates a connection to the specified database.
func (p *StandardConnectionProvider) Connect(ctx context.Context, databaseName string) (DatabaseConnection, error) {
	connString := p.connStringFunc(databaseName)
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return &StandardDatabaseConnection{DB: db}, nil
}

// GetConnectionString returns the connection string for a database.
func (p *StandardConnectionProvider) GetConnectionString(databaseName string) string {
	return p.connStringFunc(databaseName)
}
