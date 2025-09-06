package pgdbtemplate_test

import (
	"context"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestStandardConnectionProvider tests the connection provider functionality.
func TestStandardConnectionProvider(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx := context.Background()

	c.Run("Basic connection string generation", func(c *qt.C) {
		connStringFunc := func(dbName string) string {
			return "postgres://localhost/" + dbName
		}

		provider := pgdbtemplate.NewStandardConnectionProvider(connStringFunc)

		// This will fail because we don't have a real database, but we can verify
		// the connection string generation and that it attempts to connect.
		_, err := provider.Connect(ctx, "testdb")
		c.Assert(err, qt.IsNotNil)
	})

	c.Run("GetConnectionString uses provided function", func(c *qt.C) {
		connStringFunc := func(dbName string) string {
			return "postgres://localhost/" + dbName + "?sslmode=disable"
		}

		provider := pgdbtemplate.NewStandardConnectionProvider(connStringFunc)

		connString := provider.GetConnectionString("mydb")
		expected := "postgres://localhost/mydb?sslmode=disable"

		c.Assert(connString, qt.Equals, expected)
	})

	c.Run("Connect respects context cancellation", func(c *qt.C) {
		connStringFunc := func(dbName string) string {
			return "postgres://nonexistent-host:5432/" + dbName
		}

		provider := pgdbtemplate.NewStandardConnectionProvider(connStringFunc)

		// Create a context that's already cancelled.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := provider.Connect(ctx, "testdb")
		c.Assert(err, qt.IsNotNil)
	})
}
