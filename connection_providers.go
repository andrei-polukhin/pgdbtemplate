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

// DatabaseConnection implements DatabaseConnection.ExecContext.
func (c *StandardDatabaseConnection) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.DB.ExecContext(ctx, query, args...)
}

// QueryRowContext implements DatabaseConnection.QueryRowContext.
func (c *StandardDatabaseConnection) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.DB.QueryRowContext(ctx, query, args...)
}

// Close implements DatabaseConnection.Close.
func (c *StandardDatabaseConnection) Close() error {
	return c.DB.Close()
}

// StandardConnectionProvider provides PostgreSQL connections
// with configurable options.
type StandardConnectionProvider struct {
	connStringFunc func(databaseName string) string
	options        []StandardDatabaseConnectionOption
}

// NewStandardConnectionProvider creates a new StandardConnectionProvider.
func NewStandardConnectionProvider(connStringFunc func(databaseName string) string, options ...StandardDatabaseConnectionOption) *StandardConnectionProvider {
	return &StandardConnectionProvider{
		connStringFunc: connStringFunc,
		options:        options,
	}
}

// Connect creates a connection to the specified database.
func (p *StandardConnectionProvider) Connect(ctx context.Context, databaseName string) (DatabaseConnection, error) {
	connString := p.connStringFunc(databaseName)
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}

	// Apply connection options.
	for _, option := range p.options {
		option(db)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close() // #nosec G104 -- Close error in error path is not critical.
		return nil, err
	}
	return &StandardDatabaseConnection{DB: db}, nil
}

// GetConnectionString returns the connection string for a database.
func (p *StandardConnectionProvider) GetConnectionString(databaseName string) string {
	return p.connStringFunc(databaseName)
}
