package pgdbtemplate

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgxConnectionOption configures PgxConnectionProvider.
type PgxConnectionOption func(*PgxConnectionProvider)

// WithPgxPoolConfig sets custom pool configuration.
func WithPgxPoolConfig(config *pgxpool.Config) PgxConnectionOption {
	return func(p *PgxConnectionProvider) {
		p.poolConfig = config
	}
}

// WithPgxMaxConns sets the maximum number of connections in the pool.
func WithPgxMaxConns(maxConns int32) PgxConnectionOption {
	return func(p *PgxConnectionProvider) {
		if p.poolConfig == nil {
			p.poolConfig = &pgxpool.Config{}
		}
		p.poolConfig.MaxConns = maxConns
	}
}

// WithPgxMinConns sets the minimum number of connections in the pool.
func WithPgxMinConns(minConns int32) PgxConnectionOption {
	return func(p *PgxConnectionProvider) {
		if p.poolConfig == nil {
			p.poolConfig = &pgxpool.Config{}
		}
		p.poolConfig.MinConns = minConns
	}
}
