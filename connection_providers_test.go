package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestStandardPgConnectionProvider tests the connection provider functionality.
func TestStandardPgConnectionProvider(t *testing.T) {
	c := quicktest.New(t)

	c.Run("Connect returns provided connection", func(c *quicktest.C) {
		// Create mock connection for this test since we're testing the provider logic.
		mockConn := &mockConnection{}
		connStringFunc := func(dbName string) string {
			return "postgres://localhost/" + dbName
		}

		provider := pgdbtemplate.NewStandardPgConnectionProvider(mockConn, connStringFunc)

		returnedConn, err := provider.Connect(context.Background(), "testdb")
		c.Assert(err, quicktest.IsNil)
		c.Assert(returnedConn, quicktest.Equals, mockConn)
	})

	c.Run("GetConnectionString uses provided function", func(c *quicktest.C) {
		// Create mock connection for this test.
		mockConn := &mockConnection{}
		connStringFunc := func(dbName string) string {
			return "postgres://localhost/" + dbName + "?sslmode=disable"
		}

		provider := pgdbtemplate.NewStandardPgConnectionProvider(mockConn, connStringFunc)

		connString := provider.GetConnectionString("mydb")
		expected := "postgres://localhost/mydb?sslmode=disable"

		c.Assert(connString, quicktest.Equals, expected)
	})
}

// mockConnection implements PgDatabaseConnection for testing.
type mockConnection struct{}

func (m *mockConnection) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return nil, nil
}

func (m *mockConnection) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return nil
}

func (m *mockConnection) Close() error {
	return nil
}
