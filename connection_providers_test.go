package pgdbtemplate_test

import (
	"context"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	_ "github.com/lib/pq"

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

	c.Run("Connection provider with options", func(c *qt.C) {
		connStringFunc := func(dbName string) string {
			return "postgres://localhost/" + dbName
		}

		// Test all connection options.
		provider := pgdbtemplate.NewStandardConnectionProvider(
			connStringFunc,
			pgdbtemplate.WithMaxOpenConns(25),
			pgdbtemplate.WithMaxIdleConns(10),
			pgdbtemplate.WithConnMaxLifetime(time.Hour),
			pgdbtemplate.WithConnMaxIdleTime(30*time.Minute),
		)

		// Verify the provider was created successfully.
		c.Assert(provider, qt.IsNotNil)

		// Test connection string generation.
		connStr := provider.GetConnectionString("testdb")
		c.Assert(connStr, qt.Equals, "postgres://localhost/testdb")

		// Attempt connection (will fail without real DB, but tests the code path).
		_, err := provider.Connect(ctx, "testdb")
		c.Assert(err, qt.IsNotNil) // Expected to fail without real PostgreSQL.
	})

	c.Run("Connection provider with single options", func(c *qt.C) {
		connStringFunc := func(dbName string) string {
			return "postgres://localhost/" + dbName
		}

		// Test each option individually.
		provider1 := pgdbtemplate.NewStandardConnectionProvider(
			connStringFunc,
			pgdbtemplate.WithMaxOpenConns(15),
		)
		c.Assert(provider1, qt.IsNotNil)

		provider2 := pgdbtemplate.NewStandardConnectionProvider(
			connStringFunc,
			pgdbtemplate.WithMaxIdleConns(5),
		)
		c.Assert(provider2, qt.IsNotNil)

		provider3 := pgdbtemplate.NewStandardConnectionProvider(
			connStringFunc,
			pgdbtemplate.WithConnMaxLifetime(2*time.Hour),
		)
		c.Assert(provider3, qt.IsNotNil)

		provider4 := pgdbtemplate.NewStandardConnectionProvider(
			connStringFunc,
			pgdbtemplate.WithConnMaxIdleTime(15*time.Minute),
		)
		c.Assert(provider4, qt.IsNotNil)
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
		provider := pgdbtemplate.NewStandardConnectionProvider(testConnectionStringFunc)

		// Create a context that's already cancelled.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := provider.Connect(ctx, "testdb")
		c.Assert(err, qt.IsNotNil)
	})
}
