package cache

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

// Instances map and reader/writer lock
var (
	redisInstances = make(map[string]*redis.Client)
	redisMutex     sync.RWMutex
)

// GetConnection returns a cached Redis connection or creates a new one
func GetConnection(name string, cfg *config.RedisConfig) (*redis.Client, error) {
	// Get existing connection
	redisMutex.RLock()
	if client, exists := redisInstances[name]; exists {
		redisMutex.RUnlock()
		return client, nil
	}
	redisMutex.RUnlock()

	// Create new connection
	redisMutex.Lock()
	defer redisMutex.Unlock()

	// Double-check locking pattern
	if client, exists := redisInstances[name]; exists {
		return client, nil
	}

	// Create and verify connection
	client, err := createClient(cfg)
	if err != nil {
		return nil, err
	}

	redisInstances[name] = client
	return client, nil
}

// createClient creates a new Redis client
func createClient(cfg *config.RedisConfig) (*redis.Client, error) {
	options := &redis.Options{
		Addr:     cfg.Addr(),
		Password: cfg.Password,
		DB:       cfg.DBNumber,

		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	}

	client := redis.NewClient(options)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		// Clean up resources on failure
		defer func() {
			if err := client.Close(); err != nil {
				log.Printf("Failed to close Redis client: %v", err)
			}
		}()
		return nil, fmt.Errorf("failed to ping Redis at %s: %w", cfg.Addr(), err)
	}

	return client, nil
}

// Ready checks if a specific Redis connection is alive
func Ready(name string) bool {
	redisMutex.RLock()
	client, exists := redisInstances[name]

	if !exists || client == nil {
		redisMutex.RUnlock()
		return false
	}
	redisMutex.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := client.Ping(ctx).Err()
	return err == nil
}

// GetClient returns a specific Redis client by name
func GetClient(name string) (*redis.Client, bool) {
	redisMutex.RLock()
	defer redisMutex.RUnlock()
	client, exists := redisInstances[name]
	return client, exists
}

// ListConnections returns all connection names
func ListConnections() []string {
	redisMutex.RLock()
	defer redisMutex.RUnlock()

	connections := make([]string, 0, len(redisInstances))
	for name := range redisInstances {
		connections = append(connections, name)
	}
	return connections
}

// CloseConnection safely closes and removes a specific connection
func CloseConnection(name string) error {
	redisMutex.Lock()
	defer redisMutex.Unlock()

	client, exists := redisInstances[name]
	if !exists {
		return fmt.Errorf("redis client '%s' not found", name)
	}

	log.Printf("Closing redis connection: %s", name)
	if err := client.Close(); err != nil {
		return fmt.Errorf("failed to close redis client: %w", err)
	}

	delete(redisInstances, name)
	return nil
}

// CloseAll closes all Redis connections
func CloseAll() error {
	redisMutex.Lock()
	defer redisMutex.Unlock()

	var errors []error

	log.Printf("Closing all redis connections")
	for name, client := range redisInstances {
		if err := client.Close(); err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", name, err))
		}
	}

	// Clear the map even if some connections failed to close
	redisInstances = make(map[string]*redis.Client)

	if len(errors) > 0 {
		return fmt.Errorf("errors closing redis connections: %v", errors)
	}
	return nil
}
