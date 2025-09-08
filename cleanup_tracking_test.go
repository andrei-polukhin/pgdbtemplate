package pgdbtemplate_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestCleanupTracksAndRemovesTestDatabases verifies that Cleanup removes
// all tracked test databases.
func TestCleanupTracksAndRemovesTestDatabases(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	connProvider := setupTestConnectionProvider()
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       fmt.Sprintf("cleanup_test_template_%d_%d", time.Now().UnixNano(), os.Getpid()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	// Create multiple test databases.
	var testDBNames []string
	var testConns []pgdbtemplate.DatabaseConnection

	for i := 0; i < 3; i++ {
		testConn, testDBName, err := tm.CreateTestDatabase(ctx)
		c.Assert(err, qt.IsNil)
		testDBNames = append(testDBNames, testDBName)
		testConns = append(testConns, testConn)
	}

	// Close connections before cleanup.
	for _, conn := range testConns {
		c.Assert(conn.Close(), qt.IsNil)
	}

	// Verify databases exist before cleanup.
	for _, dbName := range testDBNames {
		exists := databaseExists(ctx, connProvider, dbName)
		c.Assert(exists, qt.IsTrue)
	}

	// Call Cleanup - should remove all tracked test databases and template.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.IsNil)

	// Verify all test databases were removed.
	for _, dbName := range testDBNames {
		exists := databaseExists(ctx, connProvider, dbName)
		c.Assert(exists, qt.IsFalse)
	}

	// Verify template database was also removed.
	exists := databaseExists(ctx, connProvider, config.TemplateName)
	c.Assert(exists, qt.IsFalse)
}

// TestDropTestDatabaseRemovesFromTracking verifies that manually dropping databases removes them from tracking.
func TestDropTestDatabaseRemovesFromTracking(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	connProvider := setupTestConnectionProvider()
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       fmt.Sprintf("drop_tracking_template_%d_%d", time.Now().UnixNano(), os.Getpid()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(tm.Cleanup(ctx), qt.IsNil)
	}()

	// Create a test database.
	testConn, testDBName, err := tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNil)
	c.Assert(testConn.Close(), qt.IsNil)

	// Verify database exists.
	exists := databaseExists(ctx, connProvider, testDBName)
	c.Assert(exists, qt.IsTrue)

	// Manually drop the test database.
	err = tm.DropTestDatabase(ctx, testDBName)
	c.Assert(err, qt.IsNil)

	// Verify database no longer exists.
	exists = databaseExists(ctx, connProvider, testDBName)
	c.Assert(exists, qt.IsFalse)

	// Verify template database still exists.
	exists = databaseExists(ctx, connProvider, config.TemplateName)
	c.Assert(exists, qt.IsTrue)

	// Create another test database to ensure cleanup still works after manual drop.
	testConn2, testDBName2, err := tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNil)
	c.Assert(testConn2.Close(), qt.IsNil)

	// Cleanup should only drop the remaining database (and template).
	// This test verifies that the manually dropped database doesn't cause issues.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.IsNil)

	// Verify the second database was cleaned up.
	exists = databaseExists(ctx, connProvider, testDBName2)
	c.Assert(exists, qt.IsFalse)

	// Verify template database was also removed.
	exists = databaseExists(ctx, connProvider, config.TemplateName)
	c.Assert(exists, qt.IsFalse)
}

// TestCleanupFailureResilience tests that cleanup continues even if some databases fail to drop.
func TestCleanupFailureResilience(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	connProvider := setupTestConnectionProvider()
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       fmt.Sprintf("failure_test_template_%d_%d", time.Now().UnixNano(), os.Getpid()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	// Create test databases.
	testConn1, testDBName1, err := tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNil)
	defer testConn1.Close()

	testConn2, testDBName2, err := tm.CreateTestDatabase(ctx)
	c.Assert(err, qt.IsNil)
	c.Assert(testConn2.Close(), qt.IsNil)

	// Cleanup should handle the open connection and clean everything up.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.IsNil)

	// Verify all databases were cleaned up despite potential connection issues.
	exists := databaseExists(ctx, connProvider, testDBName1)
	c.Assert(exists, qt.IsFalse)

	exists = databaseExists(ctx, connProvider, testDBName2)
	c.Assert(exists, qt.IsFalse)

	exists = databaseExists(ctx, connProvider, config.TemplateName)
	c.Assert(exists, qt.IsFalse)
}

// TestConcurrentDatabaseCreationAndCleanup tests
// concurrent database operations with cleanup.
func TestConcurrentDatabaseCreationAndCleanup(t *testing.T) {
	t.Parallel()
	c := qt.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	connProvider := setupTestConnectionProvider()
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    &pgdbtemplate.NoOpMigrationRunner{},
		TemplateName:       fmt.Sprintf("concurrent_test_template_%d_%d", time.Now().UnixNano(), os.Getpid()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)

	const numGoroutines = 5
	const opsPerGoroutine = 3

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*opsPerGoroutine)

	// Launch multiple goroutines creating and dropping
	// databases concurrently.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < opsPerGoroutine; j++ {
				// Create database.
				testConn, testDBName, err := tm.CreateTestDatabase(ctx)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d op %d create error: %w", id, j, err)
					return
				}

				// Immediately close connection.
				if err := testConn.Close(); err != nil {
					errors <- fmt.Errorf("goroutine %d op %d close error: %w", id, j, err)
					return
				}

				// Sometimes drop manually, sometimes let cleanup handle it.
				if j%2 == 0 {
					if err := tm.DropTestDatabase(ctx, testDBName); err != nil {
						errors <- fmt.Errorf("goroutine %d op %d drop error: %w", id, j, err)
						return
					}
				}
				// Else: let cleanup handle it.
			}
		}(i)
	}

	// Wait for all operations to complete.
	wg.Wait()
	close(errors)

	// Check for any errors.
	for err := range errors {
		c.Fatal(err)
	}

	// Final cleanup should handle any remaining databases.
	err = tm.Cleanup(ctx)
	c.Assert(err, qt.IsNil)

	// Verify template database was removed.
	exists := databaseExists(ctx, connProvider, config.TemplateName)
	c.Assert(exists, qt.IsFalse)
}
