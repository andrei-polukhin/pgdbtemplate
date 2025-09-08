package pgdbtemplate_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
	_ "github.com/lib/pq"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// concurrentDBCounter is an atomic counter used to generate unique database names
// in concurrent benchmark tests to prevent name collisions between goroutines.
var concurrentDBCounter int64

// benchConnectionStringFunc creates a connection string for the given database name.
func benchConnectionStringFunc(dbName string) string {
	return pgdbtemplate.ReplaceDatabaseInConnectionString(testConnectionString, dbName)
}

// createSampleMigrations creates a set of realistic migrations for benchmarking.
func createSampleMigrations(tempDir string, numTables int) error {
	allMigrations := []struct {
		filename string
		content  string
	}{{
		"001_create_users_table.sql",
		`CREATE TABLE users (
			id SERIAL PRIMARY KEY,
			email VARCHAR(255) UNIQUE NOT NULL,
			username VARCHAR(100) NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			first_name VARCHAR(100),
			last_name VARCHAR(100),
			is_active BOOLEAN DEFAULT true,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
		
		CREATE INDEX idx_users_email ON users(email);
		CREATE INDEX idx_users_username ON users(username);
		CREATE INDEX idx_users_created_at ON users(created_at);`,
	}, {
		"002_create_posts_table.sql",
		`CREATE TABLE posts (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title VARCHAR(255) NOT NULL,
			content TEXT,
			slug VARCHAR(255) UNIQUE NOT NULL,
			status VARCHAR(20) DEFAULT 'draft',
			published_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
		
		CREATE INDEX idx_posts_user_id ON posts(user_id);
		CREATE INDEX idx_posts_slug ON posts(slug);
		CREATE INDEX idx_posts_status ON posts(status);
		CREATE INDEX idx_posts_published_at ON posts(published_at);`,
	}, {
		"003_create_comments_table.sql",
		`CREATE TABLE comments (
			id SERIAL PRIMARY KEY,
			post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			content TEXT NOT NULL,
			parent_id INTEGER REFERENCES comments(id) ON DELETE CASCADE,
			is_approved BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW()
		);
		
		CREATE INDEX idx_comments_post_id ON comments(post_id);
		CREATE INDEX idx_comments_user_id ON comments(user_id);
		CREATE INDEX idx_comments_parent_id ON comments(parent_id);
		CREATE INDEX idx_comments_created_at ON comments(created_at);`,
	}, {
		"004_create_tags_and_relations.sql",
		`CREATE TABLE tags (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) UNIQUE NOT NULL,
			slug VARCHAR(100) UNIQUE NOT NULL,
			description TEXT,
			created_at TIMESTAMP DEFAULT NOW()
		);
		
		CREATE TABLE post_tags (
			post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
			tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (post_id, tag_id)
		);
		
		CREATE INDEX idx_tags_name ON tags(name);
		CREATE INDEX idx_tags_slug ON tags(slug);
		CREATE INDEX idx_post_tags_post_id ON post_tags(post_id);
		CREATE INDEX idx_post_tags_tag_id ON post_tags(tag_id);`,
	}, {
		"005_insert_sample_data.sql",
		`INSERT INTO users (email, username, password_hash, first_name, last_name) VALUES
			('admin@example.com', 'admin', 'hash1', 'Admin', 'User'),
			('john@example.com', 'john_doe', 'hash2', 'John', 'Doe'),
			('jane@example.com', 'jane_smith', 'hash3', 'Jane', 'Smith'),
			('bob@example.com', 'bob_wilson', 'hash4', 'Bob', 'Wilson'),
			('alice@example.com', 'alice_brown', 'hash5', 'Alice', 'Brown');
		
		INSERT INTO tags (name, slug, description) VALUES
			('Technology', 'technology', 'Posts about technology'),
			('Programming', 'programming', 'Programming tutorials and tips'),
			('Database', 'database', 'Database design and optimization'),
			('Web Development', 'web-development', 'Web development topics'),
			('DevOps', 'devops', 'DevOps and deployment topics');
		
		INSERT INTO posts (user_id, title, content, slug, status, published_at) VALUES
			(1, 'Welcome to Our Blog', 'This is our first post!', 'welcome-to-our-blog', 'published', NOW() - INTERVAL '10 days'),
			(2, 'Getting Started with Go', 'Go is a great language...', 'getting-started-with-go', 'published', NOW() - INTERVAL '5 days'),
			(3, 'Database Design Patterns', 'Learn about database patterns...', 'database-design-patterns', 'published', NOW() - INTERVAL '3 days'),
			(4, 'Modern Web Development', 'Web development has evolved...', 'modern-web-development', 'published', NOW() - INTERVAL '1 day'),
			(5, 'Draft Post', 'This is a draft post...', 'draft-post', 'draft', NULL);
		
		INSERT INTO post_tags (post_id, tag_id) VALUES
			(1, 1), (2, 1), (2, 2), (3, 3), (4, 4), (5, 1);
		
		INSERT INTO comments (post_id, user_id, content, is_approved) VALUES
			(1, 2, 'Great first post!', true),
			(1, 3, 'Looking forward to more content.', true),
			(2, 3, 'Very helpful tutorial, thanks!', true),
			(2, 4, 'Could you cover testing next?', true),
			(3, 2, 'Excellent explanation of the patterns.', true);`,
	}}

	// Limit the number of migrations based on numTables parameter.
	maxTables := len(allMigrations)
	if numTables > maxTables {
		numTables = maxTables
	}
	if numTables < 1 {
		numTables = 1
	}

	migrations := allMigrations[:numTables]

	for _, migration := range migrations {
		filePath := tempDir + "/" + migration.filename
		if err := os.WriteFile(filePath, []byte(migration.content), 0644); err != nil {
			return err
		}
	}
	return nil
}

// Traditional approach: create database + run migrations every time.
func benchmarkTraditionalDatabaseCreation(b *testing.B, numTables int) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, numTables)
	c.Assert(err, qt.IsNil)

	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dbName := fmt.Sprintf("bench_traditional_%d_%d", i, time.Now().UnixNano())

		// Create database.
		adminDB, err := sql.Open("postgres", testConnectionString)
		c.Assert(err, qt.IsNil)

		_, err = adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
		c.Assert(err, qt.IsNil)
		c.Assert(adminDB.Close(), qt.IsNil)

		// Connect to the new database and run migrations.
		testDB, err := sql.Open("postgres", benchConnectionStringFunc(dbName))
		c.Assert(err, qt.IsNil)

		// Run migrations.
		conn := &pgdbtemplate.StandardDatabaseConnection{DB: testDB}
		err = migrationRunner.RunMigrations(ctx, conn)
		c.Assert(err, qt.IsNil)
		c.Assert(testDB.Close(), qt.IsNil)

		// Cleanup.
		adminDB, err = sql.Open("postgres", testConnectionString)
		c.Assert(err, qt.IsNil)
		_, err = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE %s", dbName))
		c.Assert(err, qt.IsNil)
		c.Assert(adminDB.Close(), qt.IsNil)
	}
}

// Template approach: create database from template.
func benchmarkTemplateDatabaseCreation(b *testing.B, numTables int) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, numTables)
	c.Assert(err, qt.IsNil)

	connProvider := pgdbtemplate.NewStandardConnectionProvider(benchConnectionStringFunc)
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	templateName := fmt.Sprintf("bench_template_%d_%d", time.Now().UnixNano(), os.Getpid())
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    migrationRunner,
		TemplateName:       templateName,
		TestDBPrefix:       fmt.Sprintf("bench_test_%d_%d", time.Now().UnixNano(), os.Getpid()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Initialize template (this is done once, not measured).
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(tm.Cleanup(ctx), qt.IsNil)
	}()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		testDB, testDBName, err := tm.CreateTestDatabase(ctx)
		c.Assert(err, qt.IsNil)

		c.Assert(testDB.Close(), qt.IsNil)
		c.Assert(tm.DropTestDatabase(ctx, testDBName), qt.IsNil)
	}
}

// BenchmarkDatabaseCreation_Traditional_5Tables benchmarks traditional approach with 5 tables.
func BenchmarkDatabaseCreation_Traditional_5Tables(b *testing.B) {
	benchmarkTraditionalDatabaseCreation(b, 5)
}

// BenchmarkDatabaseCreation_Template_5Tables benchmarks template approach with 5 tables.
func BenchmarkDatabaseCreation_Template_5Tables(b *testing.B) {
	benchmarkTemplateDatabaseCreation(b, 5)
}

// BenchmarkDatabaseCreation_Traditional_1Table benchmarks traditional approach with 1 table.
func BenchmarkDatabaseCreation_Traditional_1Table(b *testing.B) {
	benchmarkTraditionalDatabaseCreation(b, 1)
}

// BenchmarkDatabaseCreation_Template_1Table benchmarks template approach with 1 table.
func BenchmarkDatabaseCreation_Template_1Table(b *testing.B) {
	benchmarkTemplateDatabaseCreation(b, 1)
}

// BenchmarkDatabaseCreation_Traditional_3Tables benchmarks traditional approach with 3 tables.
func BenchmarkDatabaseCreation_Traditional_3Tables(b *testing.B) {
	benchmarkTraditionalDatabaseCreation(b, 3)
}

// BenchmarkDatabaseCreation_Template_3Tables benchmarks template approach with 3 tables.
func BenchmarkDatabaseCreation_Template_3Tables(b *testing.B) {
	benchmarkTemplateDatabaseCreation(b, 3)
}

// BenchmarkConcurrentDatabaseCreation_Traditional tests traditional approach with concurrent database creation.
func BenchmarkConcurrentDatabaseCreation_Traditional(b *testing.B) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, 5)
	c.Assert(err, qt.IsNil)

	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		c := qt.New(b)
		for pb.Next() {
			// Use atomic counter + timestamp + process ID for uniqueness.
			counter := atomic.AddInt64(&concurrentDBCounter, 1)
			timestamp := time.Now().UnixNano()
			dbName := fmt.Sprintf("bench_trad_conc_%d_%d_%d", counter, timestamp, os.Getpid())

			// Create database.
			adminDB, err := sql.Open("postgres", testConnectionString)
			c.Assert(err, qt.IsNil)

			_, err = adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
			c.Assert(err, qt.IsNil)
			c.Assert(adminDB.Close(), qt.IsNil)

			// Connect and run migrations.
			testDB, err := sql.Open("postgres", benchConnectionStringFunc(dbName))
			c.Assert(err, qt.IsNil)

			conn := &pgdbtemplate.StandardDatabaseConnection{DB: testDB}
			err = migrationRunner.RunMigrations(ctx, conn)
			c.Assert(testDB.Close(), qt.IsNil)
			c.Assert(err, qt.IsNil)

			// Cleanup.
			adminDB, err = sql.Open("postgres", testConnectionString)
			c.Assert(err, qt.IsNil)
			_, err = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE %s", dbName))
			c.Assert(err, qt.IsNil)
			c.Assert(adminDB.Close(), qt.IsNil)
		}
	})
}

// BenchmarkConcurrentDatabaseCreation_Template tests template approach with concurrent database creation.
func BenchmarkConcurrentDatabaseCreation_Template(b *testing.B) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, 5)
	c.Assert(err, qt.IsNil)

	connProvider := pgdbtemplate.NewStandardConnectionProvider(benchConnectionStringFunc)
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	templateName := fmt.Sprintf("bench_template_concurrent_%d_%d", time.Now().UnixNano(), os.Getpid())
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    migrationRunner,
		TemplateName:       templateName,
		TestDBPrefix:       fmt.Sprintf("bench_concurrent_%d_", time.Now().UnixNano()),
		AdminDBName:        "postgres",
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Initialize template.
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(tm.Cleanup(ctx), qt.IsNil)
	}()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		c := qt.New(b)
		for pb.Next() {
			testDB, testDBName, err := tm.CreateTestDatabase(ctx)
			c.Assert(err, qt.IsNil)

			c.Assert(testDB.Close(), qt.IsNil)
			c.Assert(tm.DropTestDatabase(ctx, testDBName), qt.IsNil)
		}
	})
}

// BenchmarkTemplateInitialization measures the one-time cost of template initialization.
func BenchmarkTemplateInitialization(b *testing.B) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, 5)
	c.Assert(err, qt.IsNil)

	connProvider := pgdbtemplate.NewStandardConnectionProvider(benchConnectionStringFunc)
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		templateName := fmt.Sprintf("bench_init_%d_%d", i, time.Now().UnixNano())
		config := pgdbtemplate.Config{
			ConnectionProvider: connProvider,
			MigrationRunner:    migrationRunner,
			TemplateName:       templateName,
			TestDBPrefix:       fmt.Sprintf("bench_init_test_%d_", i),
			AdminDBName:        "postgres",
		}

		tm, err := pgdbtemplate.NewTemplateManager(config)
		c.Assert(err, qt.IsNil)

		err = tm.Initialize(ctx)
		c.Assert(err, qt.IsNil)

		c.Assert(tm.Cleanup(ctx), qt.IsNil)
	}
}

// BenchmarkComprehensiveCleanup measures the performance of cleaning up multiple test databases.
func BenchmarkComprehensiveCleanup(b *testing.B) {
	scales := []int{5, 10, 20, 50}

	for _, numDBs := range scales {
		b.Run(fmt.Sprintf("Template_%dDBs", numDBs), func(b *testing.B) {
			benchmarkTemplateComprehensiveCleanup(b, numDBs)
		})

		b.Run(fmt.Sprintf("Traditional_%dDBs", numDBs), func(b *testing.B) {
			benchmarkTraditionalBulkCleanup(b, numDBs)
		})
	}
}

func benchmarkTemplateComprehensiveCleanup(b *testing.B, numDBs int) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, 3)
	c.Assert(err, qt.IsNil)

	connProvider := pgdbtemplate.NewStandardConnectionProvider(benchConnectionStringFunc)
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		templateName := fmt.Sprintf("bench_cleanup_%d_%d_%d", i, numDBs, time.Now().UnixNano())
		config := pgdbtemplate.Config{
			ConnectionProvider: connProvider,
			MigrationRunner:    migrationRunner,
			TemplateName:       templateName,
			TestDBPrefix:       fmt.Sprintf("bench_cleanup_test_%d_%d_", i, numDBs),
		}

		tm, err := pgdbtemplate.NewTemplateManager(config)
		c.Assert(err, qt.IsNil)

		// Initialize template (not measured).
		b.StopTimer()
		err = tm.Initialize(ctx)
		c.Assert(err, qt.IsNil)

		// Create multiple test databases.
		var testConns []pgdbtemplate.DatabaseConnection
		for j := 0; j < numDBs; j++ {
			testConn, _, err := tm.CreateTestDatabase(ctx)
			c.Assert(err, qt.IsNil)
			testConns = append(testConns, testConn)
		}

		// Close all connections before cleanup.
		for _, conn := range testConns {
			c.Assert(conn.Close(), qt.IsNil)
		}
		b.StartTimer()

		// Measure comprehensive cleanup performance.
		err = tm.Cleanup(ctx)
		c.Assert(err, qt.IsNil)
	}
}

func benchmarkTraditionalBulkCleanup(b *testing.B, numDBs int) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, 3)
	c.Assert(err, qt.IsNil)

	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var dbNames []string

		// Create multiple databases with migrations (not measured).
		b.StopTimer()
		for j := 0; j < numDBs; j++ {
			dbName := fmt.Sprintf("bench_bulk_trad_%d_%d_%d_%d", i, j, time.Now().UnixNano(), os.Getpid())
			dbNames = append(dbNames, dbName)

			// Create database.
			adminDB, err := sql.Open("postgres", testConnectionString)
			c.Assert(err, qt.IsNil)

			_, err = adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
			c.Assert(err, qt.IsNil)
			c.Assert(adminDB.Close(), qt.IsNil)

			// Connect and run migrations.
			testDB, err := sql.Open("postgres", benchConnectionStringFunc(dbName))
			c.Assert(err, qt.IsNil)

			conn := &pgdbtemplate.StandardDatabaseConnection{DB: testDB}
			err = migrationRunner.RunMigrations(ctx, conn)
			c.Assert(err, qt.IsNil)
			c.Assert(testDB.Close(), qt.IsNil)
		}
		b.StartTimer()

		// Measure bulk cleanup performance.
		adminDB, err := sql.Open("postgres", testConnectionString)
		c.Assert(err, qt.IsNil)

		for _, dbName := range dbNames {
			_, err = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE %s", dbName))
			c.Assert(err, qt.IsNil)
		}
		c.Assert(adminDB.Close(), qt.IsNil)
	}
}

// BenchmarkScalingComparison_Sequential runs sequential database creation comparisons.
func BenchmarkScalingComparison_Sequential(b *testing.B) {
	scales := []int{1, 5, 10, 20}

	for _, scale := range scales {
		b.Run(fmt.Sprintf("Traditional_%dDBs", scale), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				benchmarkTraditionalSequential(b, scale)
			}
		})

		b.Run(fmt.Sprintf("Template_%dDBs", scale), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				benchmarkTemplateSequential(b, scale)
			}
		})
	}
}

func benchmarkTraditionalSequential(b *testing.B, numDBs int) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, 5)
	c.Assert(err, qt.IsNil)

	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	b.StopTimer()
	start := time.Now()
	b.StartTimer()

	for i := 0; i < numDBs; i++ {
		dbName := fmt.Sprintf("bench_seq_trad_%d_%d_%d", i, time.Now().UnixNano(), os.Getpid())

		// Create database.
		adminDB, err := sql.Open("postgres", testConnectionString)
		c.Assert(err, qt.IsNil)

		_, err = adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
		c.Assert(err, qt.IsNil)
		c.Assert(adminDB.Close(), qt.IsNil)

		// Connect and run migrations.
		testDB, err := sql.Open("postgres", benchConnectionStringFunc(dbName))
		c.Assert(err, qt.IsNil)

		conn := &pgdbtemplate.StandardDatabaseConnection{DB: testDB}
		err = migrationRunner.RunMigrations(ctx, conn)
		c.Assert(err, qt.IsNil)
		c.Assert(testDB.Close(), qt.IsNil)

		// Cleanup.
		adminDB, err = sql.Open("postgres", testConnectionString)
		c.Assert(err, qt.IsNil)
		_, err = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE %s", dbName))
		c.Assert(err, qt.IsNil)
		c.Assert(adminDB.Close(), qt.IsNil)
	}

	b.StopTimer()
	elapsed := time.Since(start)
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(numDBs), "ns/db")
}

func benchmarkTemplateSequential(b *testing.B, numDBs int) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, 5)
	c.Assert(err, qt.IsNil)

	connProvider := pgdbtemplate.NewStandardConnectionProvider(benchConnectionStringFunc)
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	templateName := fmt.Sprintf("bench_seq_template_%d_%d", time.Now().UnixNano(), os.Getpid())
	config := pgdbtemplate.Config{
		ConnectionProvider: connProvider,
		MigrationRunner:    migrationRunner,
		TemplateName:       templateName,
		TestDBPrefix:       fmt.Sprintf("bench_seq_test_%d_", time.Now().UnixNano()),
	}

	tm, err := pgdbtemplate.NewTemplateManager(config)
	c.Assert(err, qt.IsNil)

	// Initialize template (one-time cost, not measured).
	err = tm.Initialize(ctx)
	c.Assert(err, qt.IsNil)
	defer func() {
		c.Assert(tm.Cleanup(ctx), qt.IsNil)
	}()

	b.StopTimer()
	start := time.Now()
	b.StartTimer()

	for i := 0; i < numDBs; i++ {
		testDB, testDBName, err := tm.CreateTestDatabase(ctx)
		c.Assert(err, qt.IsNil)

		c.Assert(testDB.Close(), qt.IsNil)
		c.Assert(tm.DropTestDatabase(ctx, testDBName), qt.IsNil)
	}

	b.StopTimer()
	elapsed := time.Since(start)
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(numDBs), "ns/db")
}

// BenchmarkRealisticTestSuite simulates a realistic test suite workflow.
func BenchmarkRealisticTestSuite(b *testing.B) {
	testScenarios := []struct {
		name     string
		numTests int
		tables   int
	}{
		{"SmallSuite_5Tests_3Tables", 5, 3},
		{"MediumSuite_15Tests_3Tables", 15, 3},
		{"LargeSuite_30Tests_5Tables", 30, 5},
	}

	for _, scenario := range testScenarios {
		b.Run(fmt.Sprintf("Template_%s", scenario.name), func(b *testing.B) {
			benchmarkRealisticTemplateWorkflow(b, scenario.numTests, scenario.tables)
		})

		b.Run(fmt.Sprintf("Traditional_%s", scenario.name), func(b *testing.B) {
			benchmarkRealisticTraditionalWorkflow(b, scenario.numTests, scenario.tables)
		})
	}
}

func benchmarkRealisticTemplateWorkflow(b *testing.B, numTests, numTables int) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, numTables)
	c.Assert(err, qt.IsNil)

	connProvider := pgdbtemplate.NewStandardConnectionProvider(benchConnectionStringFunc)
	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		templateName := fmt.Sprintf("bench_realistic_%d_%d_%d", i, numTests, time.Now().UnixNano())
		config := pgdbtemplate.Config{
			ConnectionProvider: connProvider,
			MigrationRunner:    migrationRunner,
			TemplateName:       templateName,
			TestDBPrefix:       fmt.Sprintf("bench_real_test_%d_", i),
		}

		tm, err := pgdbtemplate.NewTemplateManager(config)
		c.Assert(err, qt.IsNil)

		// Template initialization (one-time setup cost).
		err = tm.Initialize(ctx)
		c.Assert(err, qt.IsNil)

		// Simulate running multiple tests (each creates and uses a database).
		var testConns []pgdbtemplate.DatabaseConnection
		for j := 0; j < numTests; j++ {
			testConn, _, err := tm.CreateTestDatabase(ctx)
			c.Assert(err, qt.IsNil)

			// Simulate some database work (minimal for benchmarking).
			var count int
			err = testConn.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
			c.Assert(err, qt.IsNil)

			testConns = append(testConns, testConn)
		}

		// Close all connections.
		for _, conn := range testConns {
			c.Assert(conn.Close(), qt.IsNil)
		}

		// Comprehensive cleanup (removes all test databases + template).
		err = tm.Cleanup(ctx)
		c.Assert(err, qt.IsNil)
	}
}

func benchmarkRealisticTraditionalWorkflow(b *testing.B, numTests, numTables int) {
	c := qt.New(b)
	ctx := context.Background()
	tempDir := b.TempDir()

	err := createSampleMigrations(tempDir, numTables)
	c.Assert(err, qt.IsNil)

	migrationRunner := pgdbtemplate.NewFileMigrationRunner(
		[]string{tempDir},
		pgdbtemplate.AlphabeticalMigrationFilesSorting,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var dbNames []string
		var testConns []*sql.DB

		// Simulate running multiple tests (each creates database + runs migrations).
		for j := 0; j < numTests; j++ {
			dbName := fmt.Sprintf("bench_real_trad_%d_%d_%d_%d", i, j, time.Now().UnixNano(), os.Getpid())
			dbNames = append(dbNames, dbName)

			// Create database.
			adminDB, err := sql.Open("postgres", testConnectionString)
			c.Assert(err, qt.IsNil)

			_, err = adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
			c.Assert(err, qt.IsNil)
			c.Assert(adminDB.Close(), qt.IsNil)

			// Connect and run migrations.
			testDB, err := sql.Open("postgres", benchConnectionStringFunc(dbName))
			c.Assert(err, qt.IsNil)

			conn := &pgdbtemplate.StandardDatabaseConnection{DB: testDB}
			err = migrationRunner.RunMigrations(ctx, conn)
			c.Assert(err, qt.IsNil)

			// Simulate some database work.
			var count int
			err = testDB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
			c.Assert(err, qt.IsNil)

			testConns = append(testConns, testDB)
		}

		// Close all connections.
		for _, conn := range testConns {
			c.Assert(conn.Close(), qt.IsNil)
		}

		// Bulk cleanup (similar to what our Cleanup() does).
		adminDB, err := sql.Open("postgres", testConnectionString)
		c.Assert(err, qt.IsNil)

		for _, dbName := range dbNames {
			_, err = adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE %s", dbName))
			c.Assert(err, qt.IsNil)
		}
		c.Assert(adminDB.Close(), qt.IsNil)
	}
}
