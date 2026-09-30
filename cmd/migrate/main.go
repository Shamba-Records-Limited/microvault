package main

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"

	_ "github.com/joho/godotenv/autoload"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
	"github.com/Shamba-Records-Limited/microvault/platform/database"
)

func main() {
	logging.Setup()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	command := os.Args[1]

	switch command {
	case "up":
		// Run all pending migrations
		slog.Info("Running database migrations")
		if err := database.RunMigrations(&cfg.Postgres); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
		slog.Info("Migrations completed successfully")

	case "down":
		// Rollback last migration
		slog.Info("Rolling back last migration")
		if err := database.RollbackMigration(&cfg.Postgres); err != nil {
			log.Fatalf("Rollback failed: %v", err)
		}
		slog.Info("Rollback completed successfully")

	case "version":
		// Show current migration version
		version, dirty, err := database.MigrationVersion(&cfg.Postgres)
		if err != nil {
			log.Fatalf("Failed to get migration version: %v", err)
		}
		status := "clean"
		if dirty {
			status = "dirty"
		}
		fmt.Printf("Current migration version: %d (%s)\n", version, status)

	case "force":
		// Force migration to specific version
		if len(os.Args) < 3 {
			log.Fatal("Usage: migrate force <version>")
		}
		version, err := strconv.Atoi(os.Args[2])
		if err != nil {
			log.Fatalf("Invalid version number: %v", err)
		}
		slog.Info("forcing migration version", slog.Int("version", version))
		if err := database.ForceMigrationVersion(&cfg.Postgres, version); err != nil {
			log.Fatalf("Force migration failed: %v", err)
		}
		slog.Info("Force migration completed successfully")

	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Migration CLI Tool")
	fmt.Println("\nUsage:")
	fmt.Println("  migrate <command> [arguments]")
	fmt.Println("\nCommands:")
	fmt.Println("  up              Run all pending migrations")
	fmt.Println("  down            Rollback the last migration")
	fmt.Println("  version         Show current migration version")
	fmt.Println("  force <version> Force migration to a specific version (use with caution)")
	fmt.Println("\nExamples:")
	fmt.Println("  migrate up")
	fmt.Println("  migrate down")
	fmt.Println("  migrate version")
	fmt.Println("  migrate force 3")
}
