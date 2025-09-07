package pgdbtemplate

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgxConnectionProvider implements ConnectionProvider using pgx driver with connection pooling.
type PgxConnectionProvider struct {
	connectionStringFunc func(string) string
	poolConfig           *pgxpool.Config

	mu    sync.RWMutex
	pools map[string]*pgxpool.Pool

	options []PgxConnectionOption
}

// NewPgxConnectionProvider creates a new pgx-based connection provider.
func NewPgxConnectionProvider(connectionStringFunc func(string) string, opts ...PgxConnectionOption) *PgxConnectionProvider {
	provider := &PgxConnectionProvider{
		connectionStringFunc: connectionStringFunc,
		pools:                make(map[string]*pgxpool.Pool),
		options:              opts,
	}

	for _, opt := range opts {
		opt(provider)
	}
	return provider
}

// Connect implements ConnectionProvider.Connect using pgx with connection pooling.
func (p *PgxConnectionProvider) Connect(ctx context.Context, databaseName string) (DatabaseConnection, error) {
	connString := p.GetConnectionString(databaseName)

	// Check if we already have a pool for this database.
	p.mu.RLock()
	if pool, exists := p.pools[databaseName]; exists {
		p.mu.RUnlock()
		return &PgxDatabaseConnection{Pool: pool}, nil
	}
	p.mu.RUnlock()

	// Create new pool.
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock.
	if pool, exists := p.pools[databaseName]; exists {
		return &PgxDatabaseConnection{Pool: pool}, nil
	}

	var config *pgxpool.Config
	var err error

	if p.poolConfig != nil {
		// Parse connection string first.
		config, err = pgxpool.ParseConfig(connString)
		if err != nil {
			return nil, fmt.Errorf("failed to parse connection string: %w", err)
		}

		// Apply pool configuration settings from the base config.
		if p.poolConfig.MaxConns != 0 {
			config.MaxConns = p.poolConfig.MaxConns
		}
		if p.poolConfig.MinConns != 0 {
			config.MinConns = p.poolConfig.MinConns
		}
		if p.poolConfig.MaxConnLifetime != 0 {
			config.MaxConnLifetime = p.poolConfig.MaxConnLifetime
		}
		if p.poolConfig.MaxConnIdleTime != 0 {
			config.MaxConnIdleTime = p.poolConfig.MaxConnIdleTime
		}
	} else {
		// Parse connection string directly.
		config, err = pgxpool.ParseConfig(connString)
		if err != nil {
			return nil, fmt.Errorf("failed to parse connection string: %w", err)
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test the connection.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	p.pools[databaseName] = pool
	return &PgxDatabaseConnection{Pool: pool}, nil
}

// GetConnectionString implements ConnectionProvider.GetConnectionString.
func (p *PgxConnectionProvider) GetConnectionString(databaseName string) string {
	return p.connectionStringFunc(databaseName)
}

// Close closes all connection pools.
func (p *PgxConnectionProvider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pool := range p.pools {
		pool.Close()
	}
	p.pools = make(map[string]*pgxpool.Pool)
}

// PgxDatabaseConnection implements DatabaseConnection using pgx.
type PgxDatabaseConnection struct {
	Pool *pgxpool.Pool
}

// ExecContext implements DatabaseConnection.ExecContext.
func (c *PgxDatabaseConnection) ExecContext(ctx context.Context, query string, args ...any) (any, error) {
	return c.Pool.Exec(ctx, query, args...)
}

// QueryRowContext implements DatabaseConnection.QueryRowContext.
// pgx.Row naturally implements the Row interface (it has Scan method).
func (c *PgxDatabaseConnection) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	return c.Pool.QueryRow(ctx, query, args...)
}

// Close implements DatabaseConnection.Close.
func (*PgxDatabaseConnection) Close() error {
	// Note: We don't close the pool here as it might be shared.
	// The pool will be closed when PgxConnectionProvider.Close() is called.
	return nil
}
