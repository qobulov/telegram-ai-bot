package storage

import "testing"

// No two migration files share a number: the second would never run where
// the first was applied, as each version is recorded once.
func TestMigrationNumbersAreUnique(t *testing.T) {
	if _, err := loadMigrations(); err != nil {
		t.Fatal(err)
	}
}
