// Command migrate is the database migration CLI. It loads configuration from
// environment (with godotenv autoload) and dispatches to the corresponding
// function in platform/database:
//
//	migrate up              Run all pending migrations.
//	migrate down            Roll back the last migration.
//	migrate version         Print the current migration version and dirty flag.
//	migrate force <version> Pin the migration version without running migrations
//	                        (use only to recover from a dirty state).
//
// The migrations themselves are embedded SQL files under
// platform/database/migrations and are shared with the API server's boot
// path; this CLI exists for operational control — applying, inspecting, or
// recovering schema state without starting the full server.
package main
