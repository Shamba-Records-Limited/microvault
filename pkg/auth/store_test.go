package auth

import (
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestNewRedisStore(t *testing.T) {
	if _, err := NewRedisStore(nil, "mv"); err == nil {
		t.Error("expected error for nil client")
	}
	client := redis.NewClient(&redis.Options{Addr: "localhost:0"}) // not connected; fine for construction
	store, err := NewRedisStore(client, "mv")
	if err != nil || store == nil {
		t.Fatalf("NewRedisStore: store=%v err=%v", store, err)
	}
}

func TestRedisStore_Key(t *testing.T) {
	s := &RedisStore{keyPrefix: "mv"}
	if got := s.key("abc123"); got != "mv:challenge:abc123" {
		t.Errorf("key() = %q, want mv:challenge:abc123", got)
	}
}
