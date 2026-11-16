//go:build integration

package sms_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
	"github.com/Shamba-Records-Limited/microvault/platform/database"
)

var testDB *gorm.DB

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func TestMain(m *testing.M) {
	cfg := &config.PostgresConfig{
		Host:     env("DB_HOST", "localhost"),
		Port:     env("DB_PORT", "5435"),
		User:     env("DB_USER", "microvault_test"),
		Password: env("DB_PASSWORD", "microvault_test"),
		DBName:   env("DB_NAME", "microvault_test"),
		SSLMode:  env("DB_SSL_MODE", "disable"),
		TimeZone: "UTC",
	}
	if err := database.RunMigrations(cfg); err != nil {
		fmt.Println("integration: skipping — migrations failed:", err)
		os.Exit(0)
	}
	db, err := database.GetConnection("test", cfg)
	if err != nil {
		fmt.Println("integration: skipping — connect failed:", err)
		os.Exit(0)
	}
	testDB = db
	os.Exit(m.Run())
}

func TestLogFollowsReportsUntilFinal(t *testing.T) {
	testDB.Exec("TRUNCATE sms_messages")
	l, err := sms.NewLog(testDB)
	require.NoError(t, err)
	ctx := context.Background()

	id := "ATXid_it_1"
	require.NoError(t, l.Record(ctx, &sms.Message{Provider: "africastalking", ProviderMessageID: &id,
		PhoneNumber: "+254799000001", Kind: "loan.disbursed", Ref: "L1", Status: sms.StatusSubmitted}))
	require.NoError(t, l.Record(ctx, &sms.Message{Provider: "africastalking",
		PhoneNumber: "0799000001", Kind: "account.pilot_invite", Status: sms.StatusSendFailed, FailureReason: "gateway down"}))

	changed, err := l.ApplyReport(ctx, sms.DeliveryReport{ID: id, Status: "Buffered"})
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = l.ApplyReport(ctx, sms.DeliveryReport{ID: id, Status: "Success", NetworkCode: "63902"})
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = l.ApplyReport(ctx, sms.DeliveryReport{ID: id, Status: "Sent"})
	require.NoError(t, err)
	assert.False(t, changed, "a final status is never overwritten")
	changed, err = l.ApplyReport(ctx, sms.DeliveryReport{ID: "unknown", Status: "Success"})
	require.NoError(t, err)
	assert.False(t, changed)

	byLoan, err := l.ForRef(ctx, "L1", 10)
	require.NoError(t, err)
	require.Len(t, byLoan, 1)
	assert.True(t, byLoan[0].Delivered())
	assert.Equal(t, "63902", byLoan[0].NetworkCode)

	byPhone, err := l.ForPhone(ctx, "+254799000001", 10)
	require.NoError(t, err)
	assert.Len(t, byPhone, 2)

	invites, err := l.LatestOfKind(ctx, "account.pilot_invite", []string{"254799000001", "254799000002"})
	require.NoError(t, err)
	require.Contains(t, invites, "254799000001")
	assert.Equal(t, sms.StatusSendFailed, invites["254799000001"].Status)
	assert.NotContains(t, invites, "254799000002")
}
