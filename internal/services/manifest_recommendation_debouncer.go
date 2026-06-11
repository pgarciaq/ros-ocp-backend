package services

// ADR-0165: Defer recommendation engines for synthesized manifest IDs until quiet period expires.
import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/asyncjobs"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func init() {
	asyncjobs.RegisterShutdownHook(ShutdownSynthManifestDebouncers)
}

var manifestRecommendationDeferredTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_manifest_recommendation_deferred_total",
	Help: "Manifest recommendation runs deferred for synthesized manifest IDs pending quiet period",
})

type synthManifestDebounce struct {
	mu         sync.Mutex
	timer      *time.Timer
	generation uint64
	kafkaMsg   types.KafkaMsg
	pool       *pgxpool.Pool
}

var (
	synthManifestDebouncers sync.Map // manifestID -> *synthManifestDebounce

	debouncerLifecycleCtx    context.Context
	debouncerLifecycleCancel context.CancelFunc
	debouncerShutdown        atomic.Bool
)

func isSynthesizedManifestID(manifestID string) bool {
	return strings.HasPrefix(manifestID, synthesizedManifestPrefix)
}

func synthManifestQuietPeriod() time.Duration {
	secs := config.GetConfig().SynthManifestQuietPeriodSecs
	if secs <= 0 {
		secs = 30
	}
	return time.Duration(secs) * time.Second
}

// InitSynthManifestDebouncer wires debouncer shutdown to parent lifecycle (processor or API).
// Pending quiet-period timers are stopped when parent is cancelled.
func InitSynthManifestDebouncer(parent context.Context) {
	if debouncerLifecycleCancel != nil {
		debouncerLifecycleCancel()
	}
	debouncerShutdown.Store(false)
	debouncerLifecycleCtx, debouncerLifecycleCancel = context.WithCancel(parent)
	go func() {
		<-parent.Done()
		ShutdownSynthManifestDebouncers()
	}()
}

func debouncerRunContext() context.Context {
	if debouncerLifecycleCtx != nil {
		return debouncerLifecycleCtx
	}
	return asyncjobs.Context()
}

// scheduleManifestRecommendations runs recommendations immediately for real manifest IDs,
// or debounces synthesized manifests until a quiet period passes with no new file activity.
func scheduleManifestRecommendations(ctx context.Context, pool *pgxpool.Pool, kafkaMsg types.KafkaMsg) error {
	manifestID := manifestIDFromMsg(kafkaMsg)
	if !isSynthesizedManifestID(manifestID) {
		return runManifestRecommendations(ctx, pool, kafkaMsg)
	}
	deferSynthManifestRecommendations(pool, kafkaMsg, manifestID)
	return nil
}

func deferSynthManifestRecommendations(pool *pgxpool.Pool, kafkaMsg types.KafkaMsg, manifestID string) {
	manifestRecommendationDeferredTotal.Inc()

	entryIface, _ := synthManifestDebouncers.LoadOrStore(manifestID, &synthManifestDebounce{})
	entry := entryIface.(*synthManifestDebounce)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.kafkaMsg = kafkaMsg
	entry.pool = pool

	period := synthManifestQuietPeriod()
	scheduleSynthManifestTimer(entry, manifestID, period)

	log := logging.ForOrg(kafkaMsg.Metadata.Org_id, kafkaMsg.Metadata.Cluster_uuid)
	log.Infof("manifest %s synthesized — deferring recommendations for %s quiet period", manifestID, period)
}

func scheduleSynthManifestTimer(entry *synthManifestDebounce, manifestID string, period time.Duration) {
	entry.generation++
	gen := entry.generation
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.timer = time.AfterFunc(period, func() {
		fireSynthManifestRecommendations(manifestID, entry, gen)
	})
}

func fireSynthManifestRecommendations(manifestID string, entry *synthManifestDebounce, gen uint64) {
	entry.mu.Lock()
	if debouncerShutdown.Load() || entry.generation != gen {
		entry.mu.Unlock()
		return
	}
	msg := entry.kafkaMsg
	p := entry.pool
	entry.mu.Unlock()

	ctx := debouncerRunContext()
	if err := ctx.Err(); err != nil {
		return
	}
	if err := runManifestRecommendations(ctx, p, msg); err != nil {
		log := logging.ForOrg(msg.Metadata.Org_id, msg.Metadata.Cluster_uuid)
		log.Errorf("deferred manifest recommendations failed: %v", err)
	}
	synthManifestDebouncers.Delete(manifestID)
}

// notifySynthManifestFileActivity resets the quiet-period timer when new files register
// for a synthesized manifest ID.
func notifySynthManifestFileActivity(manifestID string) {
	if !isSynthesizedManifestID(manifestID) {
		return
	}
	entryIface, ok := synthManifestDebouncers.Load(manifestID)
	if !ok {
		return
	}
	entry := entryIface.(*synthManifestDebounce)

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.timer == nil {
		return
	}
	scheduleSynthManifestTimer(entry, manifestID, synthManifestQuietPeriod())
}

// ShutdownSynthManifestDebouncers stops pending quiet-period timers and skips their callbacks.
func ShutdownSynthManifestDebouncers() {
	debouncerShutdown.Store(true)
	if debouncerLifecycleCancel != nil {
		debouncerLifecycleCancel()
	}
	synthManifestDebouncers.Range(func(key, value any) bool {
		entry := value.(*synthManifestDebounce)
		entry.mu.Lock()
		entry.generation++
		if entry.timer != nil {
			entry.timer.Stop()
			entry.timer = nil
		}
		entry.mu.Unlock()
		synthManifestDebouncers.Delete(key)
		return true
	})
}

func resetSynthManifestDebouncersForTest() {
	debouncerShutdown.Store(false)
	if debouncerLifecycleCancel != nil {
		debouncerLifecycleCancel()
	}
	debouncerLifecycleCtx = nil
	debouncerLifecycleCancel = nil
	synthManifestDebouncers.Range(func(key, value any) bool {
		entry := value.(*synthManifestDebounce)
		entry.mu.Lock()
		entry.generation++
		if entry.timer != nil {
			entry.timer.Stop()
			entry.timer = nil
		}
		entry.mu.Unlock()
		synthManifestDebouncers.Delete(key)
		return true
	})
}
