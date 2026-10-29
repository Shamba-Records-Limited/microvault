package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScreeningProviders(t *testing.T) {
	t.Setenv("COMPLIANCE_SCREENING_PROVIDERS", "")
	got, err := screeningProviders(&ComplianceConfig{})
	require.NoError(t, err)
	assert.Equal(t, []string{ScreeningElliptic}, got)

	t.Setenv("COMPLIANCE_SCREENING_PROVIDERS", " Elliptic , merkle,merkle ")
	got, err = screeningProviders(&ComplianceConfig{MerkleAPIKey: "k"})
	require.NoError(t, err)
	assert.Equal(t, []string{"elliptic", "merkle"}, got)

	t.Setenv("COMPLIANCE_SCREENING_PROVIDERS", "merkle")
	_, err = screeningProviders(&ComplianceConfig{})
	assert.ErrorContains(t, err, "MERKLE_API_KEY")

	t.Setenv("COMPLIANCE_SCREENING_PROVIDERS", "didit")
	_, err = screeningProviders(&ComplianceConfig{})
	assert.ErrorContains(t, err, "DIDIT_API_KEY")

	t.Setenv("COMPLIANCE_SCREENING_PROVIDERS", "chainalysis")
	_, err = screeningProviders(&ComplianceConfig{})
	assert.ErrorContains(t, err, "unknown provider")
}

func TestLevelInRange(t *testing.T) {
	assert.NoError(t, levelInRange("MERKLE_REVIEW_LEVEL", 0, 1, 5))
	assert.NoError(t, levelInRange("MERKLE_REVIEW_LEVEL", 3, 1, 5))
	assert.Error(t, levelInRange("MERKLE_REVIEW_LEVEL", 6, 1, 5))
}
