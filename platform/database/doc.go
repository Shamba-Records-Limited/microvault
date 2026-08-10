// Package database manages PostgreSQL connection lifecycle and runs schema
// migrations.
//
// Connections are named and cached process-wide. GetConnection returns the
// existing *gorm.DB for a name or creates one with double-checked locking;
// the underlying pool is sized at 10 idle / 100 open connections with a
// one-hour max lifetime. Ready pings a named connection under a 1 s timeout
// and is the hook the health package uses for its readiness probe. GetDB,
// ListConnections, CloseConnection, and CloseAll cover lookup and graceful
// shutdown.
//
// Migrations are embedded at build time from migrations/*.sql (see
// migrations.go) and driven by golang-migrate. RunMigrations applies all
// pending up migrations and treats migrate.ErrNoChange as success;
// RollbackMigration steps back one version; MigrationVersion reports the
// current version and dirty flag; ForceMigrationVersion pins the version
// without running migrations — use it only to recover a dirty state.
package database
