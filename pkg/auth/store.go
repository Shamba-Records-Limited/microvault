package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Challenge represents a cryptographic challenge used in the authentication flow.
// It contains a Stellar transaction that must be signed by the user to prove ownership
// of their private key. Challenges are temporary and expire after a configured duration.
type Challenge struct {
	ID          string    `json:"id"`          // Unique identifier for the challenge
	Transaction string    `json:"transaction"` // Base64-encoded Stellar transaction envelope
	CreatedAt   time.Time `json:"created_at"`  // Timestamp when the challenge was created
	ExpiresAt   time.Time `json:"expires_at"`  // Timestamp when the challenge expires
}

// ChallengeStore defines the interface for storing and retrieving authentication challenges.
// Implementations must provide thread-safe operations with automatic expiration support.
type ChallengeStore interface {
	// Store persists a challenge with automatic expiration based on the challenge's ExpiresAt field.
	// Returns an error if the challenge is already expired or if storage fails.
	Store(challengeID string, challenge *Challenge) error

	// Get retrieves a challenge by its ID. Returns an error if the challenge is not found,
	// has expired, or if retrieval fails.
	Get(challengeID string) (*Challenge, error)

	// Delete removes a challenge from storage. This should be called after successful
	// authentication to prevent challenge reuse. Returns an error if deletion fails.
	Delete(challengeID string) error
}

// authStoreErr starts an error builder for challenge persistence.
func authStoreErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainIdentity).
		Tags("auth", "challenge-store").
		With(pkgErrors.AttrOperation, op)
}

// RedisStore is a Redis-backed implementation of ChallengeStore.
// It uses Redis's native TTL functionality for automatic challenge expiration.
type RedisStore struct {
	client    *redis.Client // Redis client for data operations
	keyPrefix string        // Prefix for Redis keys to namespace challenges
}

// NewRedisStore creates a new RedisStore instance for storing authentication challenges.
// The keyPrefix parameter is used to namespace challenge keys in Redis, allowing multiple
// applications or environments to share the same Redis instance without key collisions.
//
// Example:
//
//	store := NewRedisStore(redisClient, "microvault:auth")
//	// This will create keys like: "microvault:auth:challenge:abc123"
func NewRedisStore(client *redis.Client, keyPrefix string) (ChallengeStore, error) {
	if client == nil {
		return nil, authStoreErr("new").With(pkgErrors.AttrDependency, "redis_client").
			Code(pkgErrors.CodeMissingDependency).Errorf("required dependency is missing")
	}
	return &RedisStore{
		client:    client,
		keyPrefix: keyPrefix,
	}, nil
}

// key generates a Redis key for a challenge ID using the configured prefix.
// The key format is: "{keyPrefix}:challenge:{id}"
func (s *RedisStore) key(id string) string {
	return fmt.Sprintf("%s:challenge:%s", s.keyPrefix, id)
}

// Store persists a challenge to Redis with automatic expiration.
// The challenge is serialized to JSON and stored with a TTL based on the ExpiresAt field.
// Redis will automatically remove the challenge when the TTL expires.
//
// Returns an error if:
//   - The challenge is already expired (ExpiresAt is in the past)
//   - JSON marshaling fails
//   - Redis SET operation fails
//   - The operation times out (3 second timeout)
func (s *RedisStore) Store(challengeID string, challenge *Challenge) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	data, err := json.Marshal(challenge)
	if err != nil {
		return authStoreErr("save").Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the challenge")
	}

	// Use Redis TTL for automatic expiration
	ttl := time.Until(challenge.ExpiresAt)
	if ttl <= 0 {
		return authStoreErr("save").Code(pkgErrors.CodeQuoteExpired).Wrapf(ErrChallengeExpired, "refusing to store an already-expired challenge")
	}

	err = s.client.Set(ctx, s.key(challengeID), data, ttl).Err()
	if err != nil {
		return authStoreErr("save").Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not store the challenge")
	}

	return nil
}

// Get retrieves a challenge from Redis by its ID.
// The challenge data is retrieved from Redis and deserialized from JSON.
//
// Returns an error if:
//   - The challenge is not found (may have expired or never existed)
//   - Redis GET operation fails
//   - JSON unmarshaling fails
//   - The operation times out (3 second timeout)
//
// Note: If a challenge has expired, Redis will automatically delete it and this
// method will return a "challenge not found" error.
func (s *RedisStore) Get(challengeID string) (*Challenge, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	data, err := s.client.Get(ctx, s.key(challengeID)).Bytes()
	if errors.Is(err, redis.Nil) {
		// Wraps the sentinel so callers can tell a genuinely absent challenge
		// from a store that is unreachable. Collapsing the two reports an
		// outage as a client error.
		return nil, authStoreErr("get").Code(pkgErrors.CodeNotFound).
			Wrapf(ErrChallengeNotFound, "challenge is not in the store")
	}
	if err != nil {
		return nil, authStoreErr("get").Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the challenge")
	}

	var challenge Challenge
	if err := json.Unmarshal(data, &challenge); err != nil {
		return nil, authStoreErr("get").Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the stored challenge")
	}

	return &challenge, nil
}

// Delete removes a challenge from Redis by its ID.
// This method should be called after successful authentication to prevent replay attacks
// where an attacker might try to reuse a valid signed challenge.
//
// Returns an error if:
//   - Redis DEL operation fails
//   - The operation times out (3 second timeout)
//
// Note: This method does not return an error if the challenge doesn't exist.
// DEL operations in Redis are idempotent.
func (s *RedisStore) Delete(challengeID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.client.Del(ctx, s.key(challengeID)).Err()
	if err != nil {
		return authStoreErr("delete").Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not delete the challenge")
	}

	return nil
}
