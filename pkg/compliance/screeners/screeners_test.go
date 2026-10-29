package screeners_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/elliptic"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/merkle"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/screeners"
	"github.com/Shamba-Records-Limited/microvault/pkg/config"
)

func TestDefaultIsElliptic(t *testing.T) {
	s, err := screeners.New(config.ComplianceConfig{}, screeners.Thresholds{})
	require.NoError(t, err)
	assert.IsType(t, &elliptic.Client{}, s)
}

func TestMerkleAlone(t *testing.T) {
	s, err := screeners.New(config.ComplianceConfig{ScreeningProviders: []string{"merkle"}, MerkleAPIKey: "k"}, screeners.Thresholds{})
	require.NoError(t, err)
	assert.IsType(t, &merkle.Client{}, s)
}

func TestCombinedIsNotASingleClient(t *testing.T) {
	s, err := screeners.New(config.ComplianceConfig{ScreeningProviders: []string{"elliptic", "merkle"}, MerkleAPIKey: "k"}, screeners.Thresholds{})
	require.NoError(t, err)
	assert.NotNil(t, s)
	_, single := s.(*merkle.Client)
	assert.False(t, single)
}

func TestDiditIsRefusedForTheStellarVault(t *testing.T) {
	_, err := screeners.New(config.ComplianceConfig{ScreeningProviders: []string{"didit"}, DiditAPIKey: "k"}, screeners.Thresholds{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support Stellar")
}

func TestTransactionScreeners(t *testing.T) {
	s, err := screeners.NewTransaction(config.ComplianceConfig{}, screeners.Thresholds{})
	require.NoError(t, err)
	assert.IsType(t, &elliptic.Client{}, s)

	_, err = screeners.NewTransaction(config.ComplianceConfig{ScreeningProviders: []string{"didit"}, DiditAPIKey: "k"}, screeners.Thresholds{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support Stellar")
}
