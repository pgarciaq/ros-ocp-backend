package reship

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
)

var reshipCoalescedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "rosocp_reship_coalesced_total",
		Help: "Business-hours reship triggers coalesced because a job was already in-flight for the same org",
	},
)

// reshipErrorsTotal counts per-cluster masu reship trigger failures inside a
// batch. Unlabeled like reshipCoalescedTotal (per-org labels would blow up
// cardinality); org/cluster travel in the error log fields.
var reshipErrorsTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "rosocp_reship_errors_total",
		Help: "Per-cluster masu reship trigger failures during batch fan-out",
	},
)

type reshipFlight struct {
	mu                 sync.Mutex
	running            bool
	pending            bool
	latestClusterUUIDs []uuid.UUID
}

// reshipFlights tracks in-flight reship jobs per org. ADR-0125: Single-flight coalescing with trailing reship.
var reshipFlights sync.Map // map[string]*reshipFlight

// reshipBatchHookMu guards access to reshipBatchHook.
var reshipBatchHookMu sync.Mutex

// reshipBatchHook is invoked with the cluster list for each coalesced batch (tests only).
var reshipBatchHook func([]uuid.UUID)

func resetReshipFlightsForTest() {
	reshipFlights = sync.Map{}
	reshipBatchHookMu.Lock()
	reshipBatchHook = nil
	reshipBatchHookMu.Unlock()
}

func copyClusterUUIDs(ids []uuid.UUID) []uuid.UUID {
	if len(ids) == 0 {
		return nil
	}
	return append([]uuid.UUID(nil), ids...)
}

func triggerReshipCoalesced(ctx context.Context, trigger Triggerer, orgID string, clusterUUIDs []uuid.UUID) {
	key := orgID
	flightIface, _ := reshipFlights.LoadOrStore(key, &reshipFlight{})
	flight := flightIface.(*reshipFlight)

	flight.mu.Lock()
	flight.latestClusterUUIDs = copyClusterUUIDs(clusterUUIDs)
	if flight.running {
		flight.pending = true
		flight.mu.Unlock()
		reshipCoalescedTotal.Inc()
		return
	}
	flight.running = true
	flight.mu.Unlock()

	for {
		flight.mu.Lock()
		clusters := copyClusterUUIDs(flight.latestClusterUUIDs)
		flight.mu.Unlock()

		runReshipBatch(ctx, trigger, orgID, clusters)
		fleetsummary.InvalidateOrg(orgID)
		fleetheatmap.InvalidateOrg(orgID)
		clustercache.InvalidateOrg(orgID)

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

func runReshipBatch(ctx context.Context, trigger Triggerer, orgID string, clusterUUIDs []uuid.UUID) {
	reshipBatchHookMu.Lock()
	hook := reshipBatchHook
	reshipBatchHookMu.Unlock()
	if hook != nil {
		hook(copyClusterUUIDs(clusterUUIDs))
	}
	sem := make(chan struct{}, orgMaxConcurrent())
	var wg sync.WaitGroup
	for _, id := range clusterUUIDs {
		wg.Add(1)
		go func(clusterID uuid.UUID) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// Log-only by design (#534): retry policy lives in
			// Service.TriggerReship (trailing reship + MaxRetries). The
			// guard fans out; it observes failures via metric + log.
			// promauto counters are goroutine-safe, no extra mutex needed.
			if err := trigger.TriggerReship(ctx, orgID, clusterID); err != nil {
				reshipErrorsTotal.Inc()
				reshipLog.WithFields(map[string]interface{}{
					"msg":          "reship trigger failed",
					"org_id":       orgID,
					"cluster_uuid": clusterID.String(),
					"error":        err.Error(),
				}).Error("reship batch cluster failed")
			}
		}(id)
	}
	wg.Wait()
}
