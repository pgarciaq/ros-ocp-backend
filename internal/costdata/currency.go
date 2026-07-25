package costdata

import (
	"sync"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

const DefaultCurrency = "USD"

// ResolveCurrency returns the currency from cost data, defaulting to USD when unset.
func ResolveCurrency(data *ClusterCostData) string {
	if data == nil || data.Currency == "" {
		return DefaultCurrency
	}
	return data.Currency
}

var (
	activeProviderMu   sync.RWMutex
	activeProviderInst CostDataProvider
	activeProviderBase string
)

// ActiveProvider returns the process-wide CostDataProvider singleton. Returns
// nil when KOKU_MASU_URL is unset (no Koku available) and no test override
// has been injected via SetActiveProviderForTest.
func ActiveProvider() CostDataProvider {
	activeProviderMu.RLock()
	if activeProviderInst != nil {
		p := activeProviderInst
		base := activeProviderBase
		activeProviderMu.RUnlock()

		cfg := config.GetConfig()
		if base == "test" || (cfg != nil && cfg.KokuMasuURL == base) {
			return p
		}
	} else {
		activeProviderMu.RUnlock()
	}

	cfg := config.GetConfig()
	if cfg == nil || cfg.KokuMasuURL == "" {
		return nil
	}

	activeProviderMu.Lock()
	defer activeProviderMu.Unlock()
	if activeProviderInst != nil && activeProviderBase == cfg.KokuMasuURL {
		return activeProviderInst
	}
	timeout := time.Duration(cfg.GlobalHTTPClientTimeoutSecs) * time.Second
	activeProviderInst = NewHTTPCostDataProvider(cfg.KokuMasuURL, timeout)
	activeProviderBase = cfg.KokuMasuURL
	return activeProviderInst
}

// ResetActiveProviderForTest clears the singleton for test isolation.
func ResetActiveProviderForTest() {
	activeProviderMu.Lock()
	defer activeProviderMu.Unlock()
	activeProviderInst = nil
	activeProviderBase = ""
}

// SetActiveProviderForTest replaces the singleton with a test double.
func SetActiveProviderForTest(p CostDataProvider) func() {
	activeProviderMu.Lock()
	prev := activeProviderInst
	prevBase := activeProviderBase
	activeProviderInst = p
	activeProviderBase = "test"
	activeProviderMu.Unlock()
	return func() {
		activeProviderMu.Lock()
		activeProviderInst = prev
		activeProviderBase = prevBase
		activeProviderMu.Unlock()
	}
}

