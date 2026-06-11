package services

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

var manifestRecommendationDeferredTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_manifest_recommendation_deferred_total",
	Help: "Manifest recommendation runs deferred for synthesized manifest IDs pending quiet period",
})

type synthManifestDebounce struct {
	mu       sync.Mutex
	timer    *time.Timer
	kafkaMsg types.KafkaMsg
	pool     *pgxpool.Pool
}

var synthManifestDebouncers sync.Map // manifestID -> *synthManifestDebounce

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

	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.kafkaMsg = kafkaMsg
	entry.pool = pool

	period := synthManifestQuietPeriod()
	entry.timer = time.AfterFunc(period, func() {
		fireSynthManifestRecommendations(manifestID, entry)
	})

	log := logging.ForOrg(kafkaMsg.Metadata.Org_id, kafkaMsg.Metadata.Cluster_uuid)
	log.Infof("manifest %s synthesized — deferring recommendations for %s quiet period", manifestID, period)
}

func fireSynthManifestRecommendations(manifestID string, entry *synthManifestDebounce) {
	entry.mu.Lock()
	msg := entry.kafkaMsg
	p := entry.pool
	entry.mu.Unlock()

	if err := runManifestRecommendations(context.Background(), p, msg); err != nil {
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
	entry.timer.Stop()
	period := synthManifestQuietPeriod()
	entry.timer = time.AfterFunc(period, func() {
		fireSynthManifestRecommendations(manifestID, entry)
	})
}

func resetSynthManifestDebouncersForTest() {
	synthManifestDebouncers.Range(func(key, value any) bool {
		entry := value.(*synthManifestDebounce)
		entry.mu.Lock()
		if entry.timer != nil {
			entry.timer.Stop()
		}
		entry.mu.Unlock()
		synthManifestDebouncers.Delete(key)
		return true
	})
}
