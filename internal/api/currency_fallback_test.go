package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// #462: the display-currency fallback chain — user preference → cost-model
// currency → USD (koku's own Coalesce behavior, D2). The provider stops
// inventing USD on error; the unset case activates when koku's user_currency
// one-liner lands (recorded on #462).

type stubCurrencyProvider struct {
	currency string
	userErr  bool
}

func (s *stubCurrencyProvider) GetEffectiveRates(
	ctx context.Context, orgID, clusterID string, start, end time.Time,
) (*costdata.ClusterCostData, error) {
	return nil, nil
}

func (s *stubCurrencyProvider) GetUserCurrency(ctx context.Context, orgID string) (string, error) {
	if s.userErr {
		return "", errors.New("koku unreachable")
	}
	return s.currency, nil
}

func (s *stubCurrencyProvider) GetExchangeRate(ctx context.Context, orgID, from, to string) (int64, error) {
	return 1_000_000_000, nil
}

func TestResolveUserCurrencyFallbackChain(t *testing.T) {
	// The chain's middle leg: an unavailable user preference falls back to
	// the stored (cost-model) currency instead of inventing USD.
	t.Run("provider error and stored EUR -> EUR", func(t *testing.T) {
		restore := costdata.SetActiveProviderForTest(&stubCurrencyProvider{userErr: true})
		defer restore()
		assert.Equal(t, "EUR", resolveUserCurrency(context.Background(), "org-x", "EUR"),
			"an unavailable preference must fall back to the stored currency, not USD (#462)")
	})

	t.Run("provider error and no stored currency -> USD (koku's final fallback, D2)", func(t *testing.T) {
		restore := costdata.SetActiveProviderForTest(&stubCurrencyProvider{userErr: true})
		defer restore()
		assert.Equal(t, "USD", resolveUserCurrency(context.Background(), "org-x", ""),
			"nothing known: USD, matching koku's Coalesce")
	})

	t.Run("user preference wins over stored", func(t *testing.T) {
		restore := costdata.SetActiveProviderForTest(&stubCurrencyProvider{currency: "JPY"})
		defer restore()
		assert.Equal(t, "JPY", resolveUserCurrency(context.Background(), "org-x", "EUR"),
			"a real preference always wins")
	})
}
