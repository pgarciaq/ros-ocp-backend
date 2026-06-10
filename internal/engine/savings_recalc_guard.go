package engine

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var savingsRecalcCoalescedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "rosocp_savings_recalc_coalesced_total",
		Help: "Savings recalculation triggers coalesced because a job was already in-flight for the same org",
	},
	[]string{"org_id"},
)

var savingsRecalcFlights sync.Map // map[string]*recalcFlight

func resetSavingsRecalcFlightsForTest() {
	savingsRecalcFlights = sync.Map{}
}

func triggerSavingsRecalcCoalesced(pool *pgxpool.Pool, orgID, clusterUUID string, recTypes []string) {
	key := orgID
	flightIface, _ := savingsRecalcFlights.LoadOrStore(key, &recalcFlight{})
	flight := flightIface.(*recalcFlight)

	flight.mu.Lock()
	if flight.running {
		flight.pending = true
		flight.mu.Unlock()
		savingsRecalcCoalescedTotal.WithLabelValues(orgID).Inc()
		return
	}
	flight.running = true
	flight.mu.Unlock()

	for {
		ctx := context.Background()
		RecalculateSavingsForOrg(ctx, pool, orgID, clusterUUID, recTypes)

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
