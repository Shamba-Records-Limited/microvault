package pglock

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These run against a real Postgres, as the e2e one:
//
//	PGLOCK_TEST_DSN=postgres://portal_e2e:portal_e2e@127.0.0.1:5446/portal_e2e go test ./pkg/pglock/
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PGLOCK_TEST_DSN")
	if dsn == "" {
		t.Skip("set PGLOCK_TEST_DSN to run against Postgres")
	}
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.PingContext(context.Background()))
	return db
}

func key(t *testing.T) string { return "pglock-test:" + t.Name() }

// holding runs lockFn in the background until release is closed.
func holding(t *testing.T, lockFn func(fn func(context.Context)) (bool, error)) (release, finished chan struct{}) {
	t.Helper()
	held, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		ran, err := lockFn(func(context.Context) { close(held); <-release })
		assert.NoError(t, err)
		assert.True(t, ran)
	}()
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("the first holder never took the lock")
	}
	return release, finished
}

func TestTryXact_RefusedWhileHeld(t *testing.T) {
	db, k := testDB(t), key(t)
	release, finished := holding(t, func(fn func(context.Context)) (bool, error) { return TryXact(context.Background(), db, k, fn) })

	ran, err := TryXact(context.Background(), db, k, func(context.Context) { t.Error("ran while another holder had the lock") })
	require.NoError(t, err)
	assert.False(t, ran)

	close(release)
	<-finished
	ran, err = TryXact(context.Background(), db, k, func(context.Context) {})
	require.NoError(t, err)
	assert.True(t, ran, "free again once the holder's transaction ended")
}

func TestTryHold_RefusedWhileHeldAcrossConnections(t *testing.T) {
	db, k := testDB(t), key(t)
	release, finished := holding(t, func(fn func(context.Context)) (bool, error) { return TryHold(context.Background(), db, k, fn) })

	ran, err := TryHold(context.Background(), db, k, func(context.Context) { t.Error("ran while held") })
	require.NoError(t, err)
	assert.False(t, ran)
	ran, err = TryXact(context.Background(), db, k, func(context.Context) { t.Error("ran while held") })
	require.NoError(t, err)
	assert.False(t, ran, "the session and transaction forms share the key")

	close(release)
	<-finished
	ran, err = TryHold(context.Background(), db, k, func(context.Context) {})
	require.NoError(t, err)
	assert.True(t, ran, "released when the holder returned")
}

func TestHold_WaitsAndDoesNotLeakOnCancel(t *testing.T) {
	db, k := testDB(t), key(t)
	release, finished := holding(t, func(fn func(context.Context)) (bool, error) { return TryHold(context.Background(), db, k, fn) })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := Hold(ctx, db, k, func(context.Context) error { t.Error("ran while held"); return nil })
	require.Error(t, err, "gave up when the context ended")

	var got atomic.Bool
	waited := make(chan error, 1)
	go func() {
		waited <- Hold(context.Background(), db, k, func(context.Context) error { got.Store(true); return nil })
	}()
	time.Sleep(200 * time.Millisecond)
	assert.False(t, got.Load(), "waits while held")
	close(release)
	<-finished
	require.NoError(t, <-waited)
	assert.True(t, got.Load())

	ran, err := TryHold(context.Background(), db, k, func(context.Context) {})
	require.NoError(t, err)
	assert.True(t, ran, "neither the cancelled wait nor the holder leaked the lock")
}

// worker counts how many leaders work at once.
type worker struct {
	mu      sync.Mutex
	active  int
	peak    int
	started atomic.Int32
}

func (w *worker) work(ctx context.Context) {
	w.mu.Lock()
	w.active++
	w.peak = max(w.peak, w.active)
	w.mu.Unlock()
	w.started.Add(1)
	<-ctx.Done()
	w.mu.Lock()
	w.active--
	w.mu.Unlock()
}

func TestLeader_OneWorksAndHandsOver(t *testing.T) {
	db, k := testDB(t), key(t)
	w := &worker{}
	fast := func() *Leader {
		return &Leader{DB: db, Key: k, Retry: 100 * time.Millisecond, Ping: 100 * time.Millisecond}
	}

	ctxA, stopA := context.WithCancel(context.Background())
	ctxB, stopB := context.WithCancel(context.Background())
	defer stopB()
	var wg sync.WaitGroup
	wg.Go(func() { fast().Run(ctxA, w.work) })
	wg.Go(func() { fast().Run(ctxB, w.work) })

	require.Eventually(t, func() bool { return w.started.Load() == 1 }, 5*time.Second, 20*time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int32(1), w.started.Load(), "the standby does not work")

	stopA()
	stopB()
	wg.Wait()
	assert.Equal(t, 1, w.peak, "never two at once")
}

// A leader whose connection dies stops working, and the standby takes over.
func TestLeader_StopsWhenItsConnectionIsKilled(t *testing.T) {
	db, k := testDB(t), key(t)
	w := &worker{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			(&Leader{DB: db, Key: k, Retry: 100 * time.Millisecond, Ping: 100 * time.Millisecond}).Run(ctx, w.work)
		})
	}
	require.Eventually(t, func() bool { return w.started.Load() == 1 }, 5*time.Second, 20*time.Millisecond)

	var killed bool
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT pg_terminate_backend(pid) FROM pg_locks
		WHERE locktype = 'advisory' AND granted
		  AND ((classid::bigint << 32) | objid::bigint) = hashtext($1)::bigint`, k).Scan(&killed))
	require.True(t, killed)

	require.Eventually(t, func() bool { return w.started.Load() == 2 }, 5*time.Second, 20*time.Millisecond, "the standby took over")
	stop()
	wg.Wait()
	assert.Equal(t, 1, w.peak, "the old leader stopped before the new one started")
}
