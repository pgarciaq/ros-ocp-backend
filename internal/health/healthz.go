package health

import (
	"context"
	"runtime"
	"strconv"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// HealthzResult holds runtime health check results for GET /healthz.
type HealthzResult struct {
	OK     bool              `json:"ok"`
	Checks map[string]string `json:"checks"`
}

// RunHealthzChecks evaluates goroutine count, GC pressure, and scheduler responsiveness.
func RunHealthzChecks(ctx context.Context) HealthzResult {
	checks := make(map[string]string)
	ok := true

	goroutines := runtime.NumGoroutine()
	checks["goroutines"] = strconv.Itoa(goroutines)
	cfg := config.GetConfig()
	if goroutines > cfg.HealthzMaxGoroutines {
		checks["goroutines_status"] = "warning: count exceeds threshold " + strconv.Itoa(cfg.HealthzMaxGoroutines)
		ok = false
	} else {
		checks["goroutines_status"] = "ok"
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	checks["heap_alloc_mb"] = strconv.FormatUint(mem.HeapAlloc/1024/1024, 10)
	checks["heap_sys_mb"] = strconv.FormatUint(mem.HeapSys/1024/1024, 10)
	checks["gc_cycles"] = strconv.FormatUint(uint64(mem.NumGC), 10)
	if mem.NumGC > 0 {
		lastPauseNs := mem.PauseNs[(mem.NumGC+255)%256]
		checks["last_gc_pause_ms"] = strconv.FormatFloat(float64(lastPauseNs)/1e6, 'f', 2, 64)
		if lastPauseNs > uint64(cfg.HealthzMaxGCPauseMs)*1_000_000 {
			checks["gc_status"] = "warning: last pause exceeds " + strconv.Itoa(cfg.HealthzMaxGCPauseMs) + "ms"
			ok = false
		} else {
			checks["gc_status"] = "ok"
		}
	} else {
		checks["gc_status"] = "ok"
	}

	if deadlockCanary(ctx) {
		checks["scheduler"] = "ok"
	} else {
		checks["scheduler"] = "warning: scheduler unresponsive within timeout"
		ok = false
	}

	return HealthzResult{OK: ok, Checks: checks}
}

func deadlockCanary(ctx context.Context) bool {
	done := make(chan struct{}, 1)
	go func() {
		runtime.Gosched()
		done <- struct{}{}
	}()

	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()

	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}
