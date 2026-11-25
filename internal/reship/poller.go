package reship

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// Poller retries business-hours reshships with reship_pending_since set.
type Poller struct {
	pool     *pgxpool.Pool
	service  *Service
	interval time.Duration
}

// PollerConfig configures the background poller.
type PollerConfig struct {
	Interval   time.Duration
	MaxRetries int
	MasuURL    string
	LockTTL    time.Duration
	// ForwardOnlyFallback transitions an exhausted cluster to forward-only
	// BH recommendations (clears the pending flag) instead of leaving it
	// pending. Mirrors ServiceConfig.ForwardOnlyFallback; wired from
	// ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK.
	ForwardOnlyFallback bool
}

// PollerConfigFromApp reads poller settings from application config.
func PollerConfigFromApp(cfg *config.Config) PollerConfig {
	interval := time.Duration(cfg.ReshipPollerIntervalSecs) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return PollerConfig{
		Interval:   interval,
		MaxRetries: cfg.ReshipMaxRetries,
		MasuURL:    cfg.KokuMasuURL,
		LockTTL:    time.Hour,
		// #936 wiring fix: the poller is the only path with a retry loop, so
		// the forward-only fallback env var must apply here — previously it
		// was only read by the API's one-shot path, where exhaustion never
		// happens, making the documented option dead.
		ForwardOnlyFallback: cfg.ReshipForwardOnlyFallback,
	}
}

// NewPoller creates a poller. Returns nil when masu URL is unset.
func NewPoller(pool *pgxpool.Pool, cfg PollerConfig) *Poller {
	if cfg.MasuURL == "" || pool == nil {
		return nil
	}
	svc := NewService(pool, ServiceConfig{
		MasuURL:             cfg.MasuURL,
		LockTTL:             cfg.LockTTL,
		MaxRetries:          cfg.MaxRetries,
		ForwardOnlyFallback: cfg.ForwardOnlyFallback,
	})
	if svc == nil {
		return nil
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Poller{pool: pool, service: svc, interval: interval}
}

// Run executes the poll loop until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	if p == nil {
		return
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	pending, err := ListPendingClusters(ctx, p.pool)
	if err != nil {
		reshipLog.Errorf("list pending reships: %v", err)
		return
	}
	for _, pc := range pending {
		if ctx.Err() != nil {
			return
		}
		log := reshipLog.WithFields(map[string]interface{}{
			"org_id":       pc.OrgID,
			"cluster_uuid": pc.ClusterUUID.String(),
		})
		if err := p.service.RetryPending(ctx, pc.OrgID, pc.ClusterUUID); err != nil {
			if errors.Is(err, ErrRetriesExhausted) {
				// #936: steady-state, not news. The exhaustion was
				// ERROR-logged (once, with its metric) at the final real
				// attempt; the pending flag stays set so an API restart
				// re-arms the budget (owner decision D2).
				log.Debugf("RetryPending skipped: %v", err)
			} else {
				log.Warnf("RetryPending failed: %v", err)
			}
		}
	}
}

// StartPoller launches the background poller when masu URL is configured.
func StartPoller(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) {
	if cfg == nil || cfg.KokuMasuURL == "" || !config.BusinessHoursFeatureEnabled() {
		return
	}
	p := NewPoller(pool, PollerConfigFromApp(cfg))
	if p == nil {
		return
	}
	go p.Run(ctx)
	reshipLog.Infof("reship poller started (interval=%s)", p.interval)
}
