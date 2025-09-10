package pgdbtemplate_test

import (
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate"
)

// TestAlphabeticalMigrationFilesSorting tests the sorting function.
func TestAlphabeticalMigrationFilesSorting(t *testing.T) {
	c := qt.New(t)

	files := []string{
		"/path/003_third.sql",
		"/path/001_first.sql",
		"/path/002_second.sql",
	}

	sorted := pgdbtemplate.AlphabeticalMigrationFilesSorting(files)

	expected := []string{
		"/path/001_first.sql",
		"/path/002_second.sql",
		"/path/003_third.sql",
	}

	c.Assert(sorted, qt.DeepEquals, expected)

	// Verify original slice wasn't modified.
	c.Assert(files[0], qt.Equals, "/path/003_third.sql")
}
