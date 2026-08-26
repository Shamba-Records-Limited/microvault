package database

import (
	"errors"
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

// RunMigrations executes all pending database migrations
func RunMigrations(cfg *config.PostgresConfig) error {
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
		cfg.SSLMode,
	)

	source, err := iofs.New(Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, dbURL)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil {
			log.Printf("Error closing migrate source: %v", sourceErr)
		}
		if dbErr != nil {
			log.Printf("Error closing migrate database: %v", dbErr)
		}
	}()

	// Run migrations
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		log.Println("Database migrations: no changes to apply")
	} else {
		log.Println("Database migrations: applied successfully")
	}

	return nil
}

// RollbackMigration rolls back the last migration
func RollbackMigration(cfg *config.PostgresConfig) error {
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
		cfg.SSLMode,
	)

	source, err := iofs.New(Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, dbURL)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil {
			log.Printf("Error closing migrate source: %v", sourceErr)
		}
		if dbErr != nil {
			log.Printf("Error closing migrate database: %v", dbErr)
		}
	}()

	// Roll back one migration
	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		log.Println("Database rollback: no migrations to rollback")
	} else {
		log.Println("Database rollback: completed successfully")
	}

	return nil
}

// MigrationVersion returns the current migration version
func MigrationVersion(cfg *config.PostgresConfig) (uint, bool, error) {
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
		cfg.SSLMode,
	)

	source, err := iofs.New(Migrations, "migrations")
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, dbURL)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil {
			log.Printf("Error closing migrate source: %v", sourceErr)
		}
		if dbErr != nil {
			log.Printf("Error closing migrate database: %v", dbErr)
		}
	}()

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, fmt.Errorf("failed to get migration version: %w", err)
	}

	return version, dirty, nil
}

// ForceMigrationVersion sets the migration version without running migrations
// Use with caution - this is for fixing dirty state issues
func ForceMigrationVersion(cfg *config.PostgresConfig, version int) error {
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
		cfg.SSLMode,
	)

	source, err := iofs.New(Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, dbURL)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer func() {
		sourceErr, dbErr := m.Close()
		if sourceErr != nil {
			log.Printf("Error closing migrate source: %v", sourceErr)
		}
		if dbErr != nil {
			log.Printf("Error closing migrate database: %v", dbErr)
		}
	}()

	if err := m.Force(version); err != nil {
		return fmt.Errorf("failed to force migration version: %w", err)
	}

	log.Printf("Database migration: forced to version %d", version)
	return nil
}
