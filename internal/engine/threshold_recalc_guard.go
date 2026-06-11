package engine

// ADR-0086: Single-flight coalescing per (org_id, recommendation_type) on threshold recalc.
import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
)

var thresholdRecalcCoalescedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "rosocp_threshold_recalc_coalesced_total",
		Help: "Threshold recalculation triggers coalesced because a job was already in-flight for the same org and recommendation type",
	},
	[]string{"org_id", "recommendation_type"},
)

type recalcFlight struct {
	mu            sync.Mutex
	running       bool
	pending       bool
	latestSavings savingsRecalcParams
}

var thresholdRecalcFlights sync.Map // map[string]*recalcFlight

func thresholdRecalcFlightKey(orgID, recType string) string {
	return orgID + "\x00" + recType
}

// resetThresholdRecalcFlightsForTest clears in-flight state between tests.
func resetThresholdRecalcFlightsForTest() {
	thresholdRecalcFlights = sync.Map{}
}

func triggerThresholdRecalcCoalesced(ctx context.Context, pool *pgxpool.Pool, orgID, recType string) {
	key := thresholdRecalcFlightKey(orgID, recType)
	flightIface, _ := thresholdRecalcFlights.LoadOrStore(key, &recalcFlight{})
	flight := flightIface.(*recalcFlight)

	flight.mu.Lock()
	if flight.running {
		flight.pending = true
		flight.mu.Unlock()
		thresholdRecalcCoalescedTotal.WithLabelValues(orgID, recType).Inc()
		return
	}
	flight.running = true
	flight.mu.Unlock()

	for {
		RecalculateThresholdsForOrg(ctx, pool, orgID, recType)
		fleetsummary.InvalidateOrg(orgID)

		flight.mu.Lock()
		if flight.pending {
			flight.pending = false
			flight.mu.Unlock()
			continue
		}
		flight.running = false
		flight.mu.Unlock()
		return
	}
}
