package pgdbtemplate_test

import (
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate"
)

func TestReplaceDatabaseInConnectionString(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	tests := []struct {
		name string

		connStr string
		dbName  string

		expected string
	}{
		// PostgreSQL URL format tests.
		{
			name:     "basic postgres URL",
			connStr:  "postgres://user:pass@localhost:5432/postgres",
			dbName:   "testdb",
			expected: "postgres://user:pass@localhost:5432/testdb",
		},
		{
			name:     "postgresql URL",
			connStr:  "postgresql://user:pass@localhost:5432/postgres",
			dbName:   "testdb",
			expected: "postgresql://user:pass@localhost:5432/testdb",
		},
		{
			name:     "postgres URL with query params",
			connStr:  "postgres://user:pass@localhost:5432/postgres?sslmode=disable&connect_timeout=10",
			dbName:   "testdb",
			expected: "postgres://user:pass@localhost:5432/testdb?sslmode=disable&connect_timeout=10",
		},
		{
			name:     "postgresql URL with query params",
			connStr:  "postgresql://user:pass@localhost:5432/postgres?sslmode=disable",
			dbName:   "testdb",
			expected: "postgresql://user:pass@localhost:5432/testdb?sslmode=disable",
		},
		{
			name:     "postgres URL without port",
			connStr:  "postgres://user:pass@localhost/postgres",
			dbName:   "testdb",
			expected: "postgres://user:pass@localhost/testdb",
		},
		{
			name:     "postgres URL with special characters in database name",
			connStr:  "postgres://user:pass@localhost:5432/postgres",
			dbName:   "test_db-123",
			expected: "postgres://user:pass@localhost:5432/test_db-123",
		},
		{
			name:     "postgres URL with IPv6 address",
			connStr:  "postgres://user:pass@[::1]:5432/postgres",
			dbName:   "testdb",
			expected: "postgres://user:pass@[::1]:5432/testdb",
		},

		// DSN format tests.
		{
			name:     "basic DSN format",
			connStr:  "host=localhost user=postgres dbname=postgres port=5432",
			dbName:   "testdb",
			expected: "host=localhost user=postgres dbname=testdb port=5432",
		},
		{
			name:     "DSN format with password",
			connStr:  "host=localhost user=postgres password=secret dbname=postgres port=5432",
			dbName:   "testdb",
			expected: "host=localhost user=postgres password=secret dbname=testdb port=5432",
		},
		{
			name:     "DSN format with SSL mode",
			connStr:  "host=localhost user=postgres dbname=postgres port=5432 sslmode=disable",
			dbName:   "testdb",
			expected: "host=localhost user=postgres dbname=testdb port=5432 sslmode=disable",
		},
		{
			name:     "DSN format with special characters in dbname",
			connStr:  "host=localhost user=postgres dbname=postgres port=5432",
			dbName:   "test_db-123",
			expected: "host=localhost user=postgres dbname=test_db-123 port=5432",
		},
		{
			name:     "DSN format different order",
			connStr:  "dbname=postgres host=localhost port=5432 user=postgres",
			dbName:   "testdb",
			expected: "dbname=testdb host=localhost port=5432 user=postgres",
		},

		// Edge cases and fallback tests.
		{
			name:     "malformed postgres URL fallback",
			connStr:  "postgres://user:pass@localhost:5432/postgres",
			dbName:   "testdb",
			expected: "postgres://user:pass@localhost:5432/testdb",
		},
		{
			name:     "simple path-like connection string",
			connStr:  "/tmp/postgres_socket/postgres",
			dbName:   "testdb",
			expected: "/tmp/postgres_socket/testdb",
		},
		{
			name:     "connection string ending with slash",
			connStr:  "localhost:5432/",
			dbName:   "testdb",
			expected: "localhost:5432/testdb",
		},
		{
			name:     "connection string without slash - fallback",
			connStr:  "localhost:5432",
			dbName:   "testdb",
			expected: "localhost:5432/testdb",
		},

		// Empty and special input tests.
		{
			name:     "empty database name",
			connStr:  "postgres://user:pass@localhost:5432/postgres",
			dbName:   "",
			expected: "postgres://user:pass@localhost:5432/",
		},
		{
			name:     "database name with spaces",
			connStr:  "postgres://user:pass@localhost:5432/postgres",
			dbName:   "test db",
			expected: "postgres://user:pass@localhost:5432/test%20db",
		},
		{
			name:     "unicode database name",
			connStr:  "postgres://user:pass@localhost:5432/postgres",
			dbName:   "тест_база",
			expected: "postgres://user:pass@localhost:5432/%D1%82%D0%B5%D1%81%D1%82_%D0%B1%D0%B0%D0%B7%D0%B0",
		},

		// Complex scenarios.
		{
			name:     "URL with encoded characters",
			connStr:  "postgres://user%40domain:p%40ss@localhost:5432/postgres?application_name=myapp",
			dbName:   "testdb",
			expected: "postgres://user%40domain:p%40ss@localhost:5432/testdb?application_name=myapp",
		},
		{
			name:     "DSN with multiple dbname-like keys",
			connStr:  "host=localhost user=postgres dbname=postgres port=5432 options=--search_path=dbname",
			dbName:   "testdb",
			expected: "host=localhost user=postgres dbname=testdb port=5432 options=--search_path=dbname",
		},
	}

	for _, test := range tests {
		c.Run(test.name, func(c *qt.C) {
			result := pgdbtemplate.ReplaceDatabaseInConnectionString(test.connStr, test.dbName)
			c.Assert(result, qt.Equals, test.expected)
		})
	}
}

// TestReplaceDatabaseInConnectionStringErrorCases tests error handling and edge cases.
func TestReplaceDatabaseInConnectionStringErrorCases(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	tests := []struct {
		name string

		connStr string
		dbName  string

		expected string
	}{{
		name:     "malformed URL uses fallback",
		connStr:  "postgres://user:pass@[invalid-ipv6:5432/postgres", // Invalid IPv6.
		dbName:   "testdb",
		expected: "postgres://user:pass@[invalid-ipv6:5432/testdb", // Fallback replacement.
	}, {
		name:     "DSN without dbname falls through to fallback",
		connStr:  "host=localhost user=postgres port=5432", // No dbname.
		dbName:   "testdb",
		expected: "host=localhost user=postgres port=5432/testdb", // Fallback.
	}, {
		name:     "empty connection string",
		connStr:  "",
		dbName:   "testdb",
		expected: "/testdb", // Fallback behavior.
	}}

	for _, test := range tests {
		c.Run(test.name, func(c *qt.C) {
			result := pgdbtemplate.ReplaceDatabaseInConnectionString(test.connStr, test.dbName)
			c.Assert(result, qt.Equals, test.expected)
		})
	}
}
