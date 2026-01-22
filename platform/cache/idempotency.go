package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// IdempotencyRecord stores the cached response for an idempotent request
type IdempotencyRecord struct {
	Key         string    `json:"key"`
	Response    []byte    `json:"response"`
	ContentType string    `json:"content_type"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

// IdempotencyStore interface for storing/retrieving idempotency records
type IdempotencyStore interface {
	// Get retrieves a cached response by idempotency key
	// Returns nil, nil if key does not exist
	Get(ctx context.Context, key string) (*IdempotencyRecord, error)

	// Store saves a response with the given idempotency key and TTL
	Store(ctx context.Context, key string, record *IdempotencyRecord, ttl time.Duration) error

	// Exists checks if an idempotency key exists (for quick checks)
	Exists(ctx context.Context, key string) (bool, error)

	// Delete removes an idempotency record
	Delete(ctx context.Context, key string) error
}

// RedisIdempotencyStore implements IdempotencyStore using Redis
type RedisIdempotencyStore struct {
	client *redis.Client
	prefix string
}

// NewRedisIdempotencyStore creates a new Redis-backed idempotency store
// prefix: namespace for keys (e.g., "idempotency" -> "idempotency:key")
func NewRedisIdempotencyStore(client *redis.Client, prefix string) *RedisIdempotencyStore {
	return &RedisIdempotencyStore{
		client: client,
		prefix: prefix,
	}
}

// key formats the Redis key with the configured prefix
func (s *RedisIdempotencyStore) key(id string) string {
	return fmt.Sprintf("%s:%s", s.prefix, id)
}

// Get retrieves a cached response by idempotency key
// Returns nil, nil if key does not exist (not an error condition)
func (s *RedisIdempotencyStore) Get(ctx context.Context, key string) (*IdempotencyRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	data, err := s.client.Get(ctx, s.key(key)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get idempotency record: %w", err)
	}

	var record IdempotencyRecord
	if err := json.Unmarshal([]byte(data), &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal idempotency record: %w", err)
	}

	return &record, nil
}

// Store saves a response with the given idempotency key and TTL
func (s *RedisIdempotencyStore) Store(ctx context.Context, key string, record *IdempotencyRecord, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal idempotency record: %w", err)
	}

	if err := s.client.Set(ctx, s.key(key), data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to store idempotency record: %w", err)
	}

	return nil
}

// Exists checks if an idempotency key exists (for quick checks without fetching data)
func (s *RedisIdempotencyStore) Exists(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	result, err := s.client.Exists(ctx, s.key(key)).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check idempotency key existence: %w", err)
	}

	return result > 0, nil
}

// Delete removes an idempotency record
func (s *RedisIdempotencyStore) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := s.client.Del(ctx, s.key(key)).Err(); err != nil {
		return fmt.Errorf("failed to delete idempotency record: %w", err)
	}

	return nil
}
