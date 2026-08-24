package database

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

// Instances map and mutex reader/writer lock
var (
	instances = make(map[string]*gorm.DB)
	mutex     sync.RWMutex
)

// GetConnection returns a database connection by name
func GetConnection(name string, cfg *config.PostgresConfig) (*gorm.DB, error) {
	// Get existing connection
	mutex.RLock()
	if db, exists := instances[name]; exists {
		mutex.RUnlock()
		return db, nil
	}
	mutex.RUnlock()

	// Create new connection
	mutex.Lock()
	defer mutex.Unlock()

	// Check if connection already exists
	if db, ok := instances[name]; ok {
		return db, nil
	}

	// Create new connection
	db, err := createConnection(cfg)
	if err != nil {
		return nil, err
	}

	// Store connection in map
	instances[name] = db

	return db, nil
}

// createConnection creates a new database connection
func createConnection(cfg *config.PostgresConfig) (*gorm.DB, error) {
	// Create new connection
	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pooling
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database object: %w", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// Ready checks if a specific connection is alive
func Ready(name string) bool {
	mutex.RLock()
	db, exists := instances[name]

	if !exists || db == nil {
		return false
	}

	sqlDB, err := db.DB()
	mutex.RUnlock()

	if err != nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	return sqlDB.PingContext(ctx) == nil
}

// GetDB returns a specific connection by name
func GetDB(name string) (*gorm.DB, bool) {
	mutex.RLock()
	defer mutex.RUnlock()
	db, exists := instances[name]
	return db, exists
}

// ListConnections returns a list of all connection names
func ListConnections() []string {
	mutex.RLock()
	defer mutex.RUnlock()

	connections := make([]string, 0, len(instances))
	for name := range instances {
		connections = append(connections, name)
	}

	return connections
}

// CloseConnection safely closes and removes a connection
func CloseConnection(name string) error {
	mutex.Lock()
	defer mutex.Unlock()

	db, exists := instances[name]
	if !exists {
		return fmt.Errorf("connection %s not found", name)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	log.Printf("Closing connection %s", name)

	if err := sqlDB.Close(); err != nil {
		return err
	}

	delete(instances, name)
	return nil
}

// CloseAll closes all connections
func CloseAll() error {
	mutex.Lock()
	defer mutex.Unlock()

	var errors []error

	log.Printf("Closing all database connections")
	for name, db := range instances {
		if sqlDB, err := db.DB(); err == nil {
			if err := sqlDB.Close(); err != nil {
				errors = append(errors, fmt.Errorf("%s: %w", name, err))
			}
		}
	}

	instances = make(map[string]*gorm.DB)

	if len(errors) > 0 {
		return fmt.Errorf("errors closing connections: %v", errors)
	}
	return nil
}
