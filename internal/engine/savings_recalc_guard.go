package engine

// ADR-0125: Single-flight coalescing with trailing run using latest caller parameters.
import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
)

var savingsRecalcCoalescedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "rosocp_savings_recalc_coalesced_total",
		Help: "Savings recalculation triggers coalesced because a job was already in-flight for the same org",
	},
	[]string{"org_id"},
)

type savingsRecalcParams struct {
	clusterUUID string
	recTypes    []string
}

var savingsRecalcFlights sync.Map // map[string]*recalcFlight

func resetSavingsRecalcFlightsForTest() {
	savingsRecalcFlights = sync.Map{}
}

func triggerSavingsRecalcCoalesced(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recTypes []string) {
	key := orgID
	flightIface, _ := savingsRecalcFlights.LoadOrStore(key, &recalcFlight{})
	flight := flightIface.(*recalcFlight)

	flight.mu.Lock()
	flight.latestSavings = savingsRecalcParams{
		clusterUUID: clusterUUID,
		recTypes:    append([]string(nil), recTypes...),
	}
	if flight.running {
		flight.pending = true
		flight.mu.Unlock()
		savingsRecalcCoalescedTotal.WithLabelValues(orgID).Inc()
		return
	}
	flight.running = true
	flight.mu.Unlock()

	for {
		flight.mu.Lock()
		params := flight.latestSavings
		flight.mu.Unlock()

		RecalculateSavingsForOrg(ctx, pool, orgID, params.clusterUUID, params.recTypes)
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
