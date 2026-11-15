package engine

// ADR-0125: Single-flight coalescing with trailing runs. Since #812 the
// trailing state is a SET of pending clusters (not a single latest slot):
// an arriving cluster-B request must not be dropped by a later cluster-C
// request while a flight runs — koku's shared-price-list dispatch fires
// overlapping per-provider callbacks for different clusters.
import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
)

var savingsRecalcCoalescedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "rosocp_savings_recalc_coalesced_total",
		Help: "Savings recalculation triggers coalesced because a job was already in-flight for the same org",
	},
)

type savingsRecalcParams struct {
	clusterUUID string
	recTypes    []string
}

// recalcFlight is the per-org single-flight state. pending holds the set of
// clusters awaiting a trailing recalculation, keyed by clusterUUID (the raw
// string; "" — org-wide — is its own key). Same-cluster arrivals merge their
// recTypes (union), so bursts still coalesce into one run per cluster.
type recalcFlight struct {
	mu      sync.Mutex
	running bool
	pending map[string]*savingsRecalcParams
}

var savingsRecalcFlights sync.Map // map[string]*recalcFlight

func resetSavingsRecalcFlightsForTest() {
	savingsRecalcFlights = sync.Map{}
}

// unionRecTypes merges two recTypes lists, preserving order and dropping
// duplicates. Downstream (RecalculateSavingsForOrg) normalizes again, so no
// case/validity handling is needed here.
func unionRecTypes(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [2][]string{a, b} {
		for _, rt := range list {
			if _, ok := seen[rt]; ok {
				continue
			}
			seen[rt] = struct{}{}
			out = append(out, rt)
		}
	}
	return out
}

// addPending merges an arriving request into the flight's pending set.
// Same cluster: union recTypes into the existing entry. New cluster (or
// org-wide ""): insert a copied entry. No org-wide subsumption — an org-wide
// entry and specific-cluster entries coexist; the redundant specific recalc
// after an org-wide that covered it recomputes the same values (a harmless
// no-op), and subsumption logic would risk dropping a request whose recTypes
// the org-wide entry does not cover.
func (f *recalcFlight) addPending(clusterUUID string, recTypes []string) {
	if f.pending == nil {
		f.pending = make(map[string]*savingsRecalcParams)
	}
	if existing, ok := f.pending[clusterUUID]; ok {
		existing.recTypes = unionRecTypes(existing.recTypes, recTypes)
		return
	}
	f.pending[clusterUUID] = &savingsRecalcParams{
		clusterUUID: clusterUUID,
		recTypes:    append([]string(nil), recTypes...),
	}
}

// popPending removes and returns the entry with the lexicographically
// smallest cluster key (deterministic drain order; each entry is independent,
// so order only affects which runs first). Returns ok=false when the set is
// empty. Callers must hold f.mu.
func (f *recalcFlight) popPending() (savingsRecalcParams, bool) {
	if len(f.pending) == 0 {
		return savingsRecalcParams{}, false
	}
	key := ""
	first := true
	for k := range f.pending {
		if first || k < key {
			key = k
			first = false
		}
	}
	params := *f.pending[key]
	delete(f.pending, key)
	return params, true
}

func triggerSavingsRecalcCoalesced(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, recTypes []string) {
	key := orgID
	flightIface, _ := savingsRecalcFlights.LoadOrStore(key, &recalcFlight{})
	flight := flightIface.(*recalcFlight)

	flight.mu.Lock()
	flight.addPending(clusterUUID, recTypes)
	if flight.running {
		flight.mu.Unlock()
		savingsRecalcCoalescedTotal.Inc()
		return
	}
	flight.running = true
	flight.mu.Unlock()

	for {
		flight.mu.Lock()
		params, ok := flight.popPending()
		if !ok {
			// The emptiness check and the running clear share this mutex
			// hold: an arrival after this either landed in pending (picked
			// up by the loop) or sees running==false and starts its own
			// flight — no lost wakeups.
			flight.running = false
			flight.mu.Unlock()
			return
		}
		flight.mu.Unlock()

		RecalculateSavingsForOrg(ctx, pool, orgID, params.clusterUUID, params.recTypes)
		fleetsummary.InvalidateOrg(orgID)
		fleetheatmap.InvalidateOrg(orgID)
		clustercache.InvalidateOrg(orgID)
	}
}
