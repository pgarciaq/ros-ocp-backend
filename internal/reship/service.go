package reship

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

var reshipLog = logging.GetLogger().WithField("component", "reship")

// Service orchestrates masu reship_ros with DB pending flags, locks, and metrics.
type Service struct {
	pool   *pgxpool.Pool
	client *HTTPClient
	lock   *LockCoordinator

	maxRetries int
	retryMu    sync.Mutex
	retries    map[lockKey]int
}

// ServiceConfig tunes reship behavior.
type ServiceConfig struct {
	MasuURL  string
	LockTTL  time.Duration
	MaxRetries int
}

// NewService wires a reship Service. Returns nil when masu URL is empty.
func NewService(pool *pgxpool.Pool, cfg ServiceConfig) *Service {
	if cfg.MasuURL == "" || pool == nil {
		return nil
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 10
	}
	ttl := cfg.LockTTL
	if ttl <= 0 {
		ttl = defaultLockTTL
	}
	return &Service{
		pool:       pool,
		client:     NewHTTPClient(cfg.MasuURL, nil),
		lock:       NewLockCoordinator(ttl),
		maxRetries: maxRetries,
		retries:    make(map[lockKey]int),
	}
}

// ServiceConfigFromApp builds ServiceConfig from application config.
func ServiceConfigFromApp(cfg *config.Config) ServiceConfig {
	return ServiceConfig{
		MasuURL:    cfg.KokuMasuURL,
		LockTTL:    time.Hour,
		MaxRetries: cfg.ReshipMaxRetries,
	}
}

// TriggerReship runs masu reship_ros with single-flight lock and trailing reship.
func (s *Service) TriggerReship(ctx context.Context, orgID string, clusterUUID uuid.UUID) error {
	if s == nil {
		return nil
	}
	clusterID := clusterUUID.String()

	for pass := 0; pass < 2; pass++ {
		release, acquired := s.lock.Acquire(orgID, clusterID)
		if !acquired {
			return nil
		}

		scheduleAt, _ := MaxScheduleUpdatedAt(ctx, s.pool, orgID, clusterUUID)
		err := s.doReship(ctx, orgID, clusterUUID)
		release()

		updatedAt, _ := MaxScheduleUpdatedAt(ctx, s.pool, orgID, clusterUUID)
		if err != nil {
			return err
		}
		if !updatedAt.After(scheduleAt) {
			return nil
		}
		reshipLog.WithFields(map[string]interface{}{
			"msg":          "trailing reship",
			"org_id":       orgID,
			"cluster_uuid": clusterID,
			"schedule_at":  scheduleAt,
			"updated_at":   updatedAt,
		}).Info("schedule changed during reship; running trailing reship")
	}
	return nil
}

func (s *Service) doReship(ctx context.Context, orgID string, clusterUUID uuid.UUID) error {
	clusterID := clusterUUID.String()
	start := time.Now().UTC()
	observeReshipStart(orgID, clusterID)

	result, err := s.client.PostReship(ctx, orgID, clusterUUID)
	if err != nil {
		observeReshipEnd(orgID, clusterID, start, 0)
		if markErr := MarkReshipPending(ctx, s.pool, orgID, clusterUUID); markErr != nil {
			reshipLog.Errorf("mark reship pending: %v", markErr)
		}
		return err
	}

	observeReshipEnd(orgID, clusterID, start, result.FilesProcessed)

	if err := ClearReshipPending(ctx, s.pool, orgID, clusterUUID); err != nil {
		return fmt.Errorf("clear reship pending: %w", err)
	}

	reshipLog.WithFields(map[string]interface{}{
		"msg":             "reship progress",
		"org_id":          orgID,
		"cluster_uuid":    clusterID,
		"files_done":      result.FilesProcessed,
		"files_total":     result.FilesTotal,
		"duration_seconds": time.Since(start).Seconds(),
	}).Info("reship completed")

	return nil
}

// RetryPending attempts reship for a pending cluster (poller path).
func (s *Service) RetryPending(ctx context.Context, orgID string, clusterUUID uuid.UUID) error {
	if s == nil {
		return nil
	}
	key := lockKey{orgID: orgID, clusterUUID: clusterUUID.String()}
	s.retryMu.Lock()
	if s.retries[key] >= s.maxRetries {
		s.retryMu.Unlock()
		return fmt.Errorf("reship max retries exceeded for %s/%s", orgID, clusterUUID)
	}
	attempt := s.retries[key] + 1
	s.retries[key] = attempt
	s.retryMu.Unlock()

	err := s.TriggerReship(WithReshipAttempt(ctx, attempt), orgID, clusterUUID)
	if err != nil {
		if attempt >= s.maxRetries {
			incReshipFailures(orgID)
			reshipLog.WithFields(map[string]interface{}{
				"msg":          "reship max retries exceeded",
				"org_id":       orgID,
				"cluster_uuid": clusterUUID.String(),
				"attempts":     attempt,
			}).Error(err.Error())
		}
		return err
	}

	s.retryMu.Lock()
	delete(s.retries, key)
	s.retryMu.Unlock()
	return nil
}

// DefaultService returns a shared Service when masu URL and DB pool are available.
func DefaultService() *Service {
	cfg := config.GetConfig()
	pool := db.GetPool()
	return NewService(pool, ServiceConfigFromApp(cfg))
}
