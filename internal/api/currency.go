package api

import (
	"context"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func fetchClusterCurrency(ctx context.Context, orgID, clusterUUID string) string {
	return GetCachedCurrency(ctx, orgID, clusterUUID)
}

// resolveUserCurrency returns the user's preferred display currency via the
// CostDataProvider, falling back to DefaultCurrency on error.
func resolveUserCurrency(ctx context.Context, orgID string) string {
	provider := costdata.ActiveProvider()
	if provider == nil {
		return costdata.DefaultCurrency
	}
	currency, err := provider.GetUserCurrency(ctx, orgID)
	if err != nil {
		log.Warnf("failed to resolve user currency for org=%s: %v", orgID, err)
		return costdata.DefaultCurrency
	}
	return currency
}

// fetchExchangeRate returns the exchange rate to convert from storedCurrency
// to userCurrency. Returns 1.0 on error (no conversion).
func fetchExchangeRate(ctx context.Context, orgID, storedCurrency, userCurrency string) float64 {
	if storedCurrency == userCurrency {
		return 1.0
	}
	provider := costdata.ActiveProvider()
	if provider == nil {
		return 1.0
	}
	rate, err := provider.GetExchangeRate(ctx, orgID, storedCurrency, userCurrency)
	if err != nil {
		log.Warnf("exchange rate unavailable for %s->%s (org=%s): %v, returning in stored currency",
			storedCurrency, userCurrency, orgID, err)
		return 1.0
	}
	return rate
}

// convertAndPatchAmount converts a MoneyAmount's value from storedCurrency to
// targetCurrency using the given rate, and updates its Units field.
func convertAndPatchAmount(m *money.MoneyAmount, rate float64, targetCurrency string) {
	if m == nil {
		return
	}
	if rate != 1.0 {
		cents := money.ParseCentsFromAmount(m)
		converted := costdata.ConvertCents(cents, rate)
		money.SetAmountFromCents(m, converted)
	}
	money.PatchUnits(m, targetCurrency)
}

func enrichContainerCurrency(ctx context.Context, orgID string, results []model.NativeContainerResult) {
	if len(results) == 0 {
		return
	}
	userCurrency := resolveUserCurrency(ctx, orgID)

	sampleCluster := results[0].ClusterUUID
	storedCurrency := GetCachedCurrency(ctx, orgID, sampleCluster)
	rate := fetchExchangeRate(ctx, orgID, storedCurrency, userCurrency)

	displayCurrency := userCurrency
	if rate == 1.0 && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}

	for i := range results {
		clusterUUID := results[i].ClusterUUID
		cur := displayCurrency
		r := rate
		if clusterUUID != sampleCluster {
			sc := GetCachedCurrency(ctx, orgID, clusterUUID)
			r = fetchExchangeRate(ctx, orgID, sc, userCurrency)
			cur = userCurrency
			if r == 1.0 && sc != userCurrency {
				cur = sc
			}
		}
		results[i].Currency = cur
		convertAndPatchAmount(results[i].EstimatedMonthlySavings, r, cur)
		convertAndPatchAmount(results[i].CPUSavings, r, cur)
		convertAndPatchAmount(results[i].MemorySavings, r, cur)
		convertAndPatchAmount(results[i].EstimatedMonthlyWaste, r, cur)

		for gpuKey, gpuRec := range results[i].GPU {
			if gpuRec == nil {
				continue
			}
			convertAndPatchAmount(gpuRec.EstimatedMonthlyGPUSavings, r, cur)
			convertAndPatchAmount(gpuRec.EstimatedMonthlyTimeslicingSavings, r, cur)
			convertAndPatchAmount(gpuRec.EstimatedMonthlyGPUWaste, r, cur)
			gpuRec.Currency = cur
			results[i].GPU[gpuKey] = gpuRec
		}
	}
}

func enrichNamespaceCurrency(ctx context.Context, orgID string, results []model.NativeNamespaceResult) {
	if len(results) == 0 {
		return
	}
	userCurrency := resolveUserCurrency(ctx, orgID)

	sampleCluster := results[0].ClusterUUID
	storedCurrency := GetCachedCurrency(ctx, orgID, sampleCluster)
	rate := fetchExchangeRate(ctx, orgID, storedCurrency, userCurrency)

	displayCurrency := userCurrency
	if rate == 1.0 && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}

	for i := range results {
		clusterUUID := results[i].ClusterUUID
		cur := displayCurrency
		r := rate
		if clusterUUID != sampleCluster {
			sc := GetCachedCurrency(ctx, orgID, clusterUUID)
			r = fetchExchangeRate(ctx, orgID, sc, userCurrency)
			cur = userCurrency
			if r == 1.0 && sc != userCurrency {
				cur = sc
			}
		}
		convertAndPatchAmount(results[i].EstimatedMonthlyWaste, r, cur)
		for _, v := range results[i].Recommendations {
			if ma, ok := v.(*money.MoneyAmount); ok {
				convertAndPatchAmount(ma, r, cur)
			}
		}
	}
}

// convertMoneyAmounts applies convertAndPatchAmount to a batch of MoneyAmount pointers.
func convertMoneyAmounts(rate float64, currency string, amounts ...*money.MoneyAmount) {
	for _, m := range amounts {
		convertAndPatchAmount(m, rate, currency)
	}
}

// resolveClusterCurrency is a helper for handlers that need currency from cost data.
func resolveClusterCurrency(ctx context.Context, orgID, clusterUUID string) string {
	if clusterUUID == "" {
		return costdata.DefaultCurrency
	}
	return fetchClusterCurrency(ctx, orgID, clusterUUID)
}

// resolveListCurrencyFromRequest returns ISO currency for list endpoints using cluster filter when set.
// Post-#364: returns the user's preferred display currency (converting from
// the cost model's stored currency) when exchange rates are available.
func resolveListCurrencyFromRequest(c echo.Context, orgID string) string {
	ctx := c.Request().Context()
	userCurrency := resolveUserCurrency(ctx, orgID)
	return userCurrency
}

// resolveDisplayCurrency returns the target display currency, the stored
// currency, and the exchange rate for converting between them. When exchange
// rates are unavailable, it falls back to storedCurrency with rate 1.0.
func resolveDisplayCurrency(ctx context.Context, orgID, clusterUUID string) (displayCurrency, storedCurrency string, rate float64) {
	storedCurrency = resolveClusterCurrency(ctx, orgID, clusterUUID)
	userCurrency := resolveUserCurrency(ctx, orgID)
	rate = fetchExchangeRate(ctx, orgID, storedCurrency, userCurrency)
	displayCurrency = userCurrency
	if rate == 1.0 && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}
	return displayCurrency, storedCurrency, rate
}

// convertNodeUtilRecsAmounts converts EstimatedMonthlySavings in all
// term/engine combinations of node utilization recommendations.
func convertNodeUtilRecsAmounts(recs []model.NodeUtilizationRec, rate float64, currency string) {
	for _, rec := range recs {
		for _, termRec := range rec.RecommendationTerms {
			if termRec.RecommendationEngines == nil {
				continue
			}
			if termRec.RecommendationEngines.Cost != nil {
				convertAndPatchAmount(termRec.RecommendationEngines.Cost.EstimatedMonthlySavings, rate, currency)
			}
			if termRec.RecommendationEngines.Performance != nil {
				convertAndPatchAmount(termRec.RecommendationEngines.Performance.EstimatedMonthlySavings, rate, currency)
			}
		}
	}
}

// convertNodeGPURecsToUserCurrency converts SavingsPerGPU and
// EstimatedMonthlySavings fields in GPU recommendations, handling
// per-cluster stored currencies.
func convertNodeGPURecsToUserCurrency(ctx context.Context, orgID string, recs []model.NodeGPURecommendation, userCurrency string) {
	if len(recs) == 0 {
		return
	}

	sampleCluster := recs[0].ClusterUUID
	storedCurrency := fetchClusterCurrency(ctx, orgID, sampleCluster)
	rate := fetchExchangeRate(ctx, orgID, storedCurrency, userCurrency)

	displayCurrency := userCurrency
	if rate == 1.0 && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}

	for i := range recs {
		cur := displayCurrency
		r := rate
		if recs[i].ClusterUUID != sampleCluster {
			sc := fetchClusterCurrency(ctx, orgID, recs[i].ClusterUUID)
			r = fetchExchangeRate(ctx, orgID, sc, userCurrency)
			cur = userCurrency
			if r == 1.0 && sc != userCurrency {
				cur = sc
			}
		}
		convertAndPatchAmount(recs[i].SavingsPerGPU, r, cur)
		convertAndPatchAmount(recs[i].EstimatedMonthlySavings, r, cur)
	}
}
