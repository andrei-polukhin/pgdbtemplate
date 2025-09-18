package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// databaseProvider is an interface for providers that manage databases.
type databaseProvider interface {
	getDatabases() map[string]bool
	getMutex() *sync.RWMutex
}

// sharedMockDatabaseConnection is a shared mock implementation of DatabaseConnection
// that can be reused across different test files.
type sharedMockDatabaseConnection struct {
	provider databaseProvider
	dbName   string
}

// ExecContext implements pgdbtemplate.DatabaseConnection.ExecContext.
func (m *sharedMockDatabaseConnection) ExecContext(ctx context.Context, query string, args ...any) (any, error) {
	if strings.Contains(query, "CREATE DATABASE") {
		parts := strings.Fields(query)
		if len(parts) >= 3 {
			dbName := strings.Trim(parts[2], `"`)
			mu := m.provider.getMutex()
			mu.Lock()
			databases := m.provider.getDatabases()
			if databases[dbName] {
				mu.Unlock()
				return nil, fmt.Errorf("database \"%s\" already exists", dbName)
			}
			databases[dbName] = true
			mu.Unlock()
		}
	} else if strings.Contains(query, "DROP DATABASE") {
		parts := strings.Fields(query)
		var dbName string
		if len(parts) >= 5 && parts[2] == "IF" && parts[3] == "EXISTS" {
			// Handle "DROP DATABASE IF EXISTS dbname"
			dbName = strings.Trim(parts[4], `"`)
		} else if len(parts) >= 3 {
			// Handle "DROP DATABASE dbname"
			dbName = strings.Trim(parts[2], `"`)
		}
		if dbName != "" {
			mu := m.provider.getMutex()
			mu.Lock()
			databases := m.provider.getDatabases()
			if databases[dbName] {
				delete(databases, dbName)
			} else if !(len(parts) >= 5 && parts[2] == "IF" && parts[3] == "EXISTS") {
				// Only error if it's not an "IF EXISTS" query
				mu.Unlock()
				return nil, fmt.Errorf("database \"%s\" does not exist", dbName)
			}
			mu.Unlock()
		}
	}
	return nil, nil
}

// QueryRowContext implements pgdbtemplate.DatabaseConnection.QueryRowContext.
func (m *sharedMockDatabaseConnection) QueryRowContext(ctx context.Context, query string, args ...any) pgdbtemplate.Row {
	if strings.Contains(query, "SELECT datname FROM pg_database WHERE NOT datistemplate") {
		var dbs []any
		mu := m.provider.getMutex()
		mu.RLock()
		databases := m.provider.getDatabases()
		for db, exists := range databases {
			if exists && db != "template0" && db != "template1" {
				dbs = append(dbs, db)
			}
		}
		mu.RUnlock()
		return &sharedMockRow{data: dbs}
	}
	if strings.Contains(query, "SELECT datname FROM pg_database WHERE datistemplate") {
		var dbs []any
		mu := m.provider.getMutex()
		mu.RLock()
		databases := m.provider.getDatabases()
		for db, exists := range databases {
			if exists && (db == "template0" || db == "template1") {
				dbs = append(dbs, db)
			}
		}
		mu.RUnlock()
		return &sharedMockRow{data: dbs}
	}
	if strings.Contains(query, "SELECT TRUE FROM pg_database WHERE datname") {
		// Handle both parameterized and literal queries
		if len(args) > 0 {
			// Parameterized query: SELECT TRUE FROM pg_database WHERE datname = $1
			if dbName, ok := args[0].(string); ok {
				mu := m.provider.getMutex()
				mu.RLock()
				databases := m.provider.getDatabases()
				exists := databases[dbName]
				mu.RUnlock()
				if exists {
					return &sharedMockRow{data: []any{true}}
				}
			}
		} else if strings.Contains(query, "WHERE datname =") {
			// Literal query: SELECT TRUE FROM pg_database WHERE datname = 'db_name'
			parts := strings.Split(query, "WHERE datname =")
			if len(parts) == 2 {
				dbNameWithQuotes := strings.TrimSpace(parts[1])
				dbNameWithQuotes = strings.TrimSuffix(dbNameWithQuotes, " LIMIT 1")
				dbName := strings.Trim(dbNameWithQuotes, "'")
				mu := m.provider.getMutex()
				mu.RLock()
				databases := m.provider.getDatabases()
				exists := databases[dbName]
				mu.RUnlock()
				if exists {
					return &sharedMockRow{data: []any{true}}
				}
			}
		}
		return &sharedMockRow{err: sql.ErrNoRows}
	}
	if strings.Contains(query, "SELECT 1 FROM pg_database") {
		if len(args) > 0 {
			if dbName, ok := args[0].(string); ok {
				mu := m.provider.getMutex()
				mu.RLock()
				databases := m.provider.getDatabases()
				exists := databases[dbName]
				mu.RUnlock()
				if exists {
					return &sharedMockRow{data: []any{1}}
				}
			}
		}
		return &sharedMockRow{err: sql.ErrNoRows}
	}
	if strings.Contains(query, "SELECT 1 FROM information_schema.tables") {
		// Assume test_table exists if db exists
		mu := m.provider.getMutex()
		mu.RLock()
		databases := m.provider.getDatabases()
		exists := databases[m.dbName]
		mu.RUnlock()
		if exists {
			return &sharedMockRow{data: []any{1}}
		}
		return &sharedMockRow{err: sql.ErrNoRows}
	}
	return &sharedMockRow{}
}

// Close implements pgdbtemplate.DatabaseConnection.Close.
func (m *sharedMockDatabaseConnection) Close() error {
	return nil
}

// sharedMockRow is a shared mock implementation of pgdbtemplate.Row.
type sharedMockRow struct {
	data []any
	err  error
}

func (r *sharedMockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i < len(r.data) {
			switch v := d.(type) {
			case *string:
				*v = r.data[i].(string)
			case *int:
				*v = r.data[i].(int)
			case *bool:
				*v = r.data[i].(bool)
			}
		}
	}
	return nil
}
