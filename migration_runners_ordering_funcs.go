package pgdbtemplate

import "sort"

// AlphabeticalMigrationFilesSorting makes a copy of the provided slice
// and sorts migration files alphabetically in the copied slice.
//
// The original slice is not modified.
func AlphabeticalMigrationFilesSorting(files []string) []string {
	sorted := make([]string, len(files))
	copy(sorted, files)

	sort.Strings(sorted)
	return sorted
}
