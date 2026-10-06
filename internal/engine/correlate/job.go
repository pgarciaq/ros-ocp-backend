package correlate

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// advisoryVerdict is the only v1 verdict. The column exists so future
// verdicts extend rows, not schemas.
const advisoryVerdict = "do_not_add_workers_first"

// writeAdvisory upserts one fired advisory; reruns refresh evidence in place.
func writeAdvisory(ctx context.Context, pool *pgxpool.Pool, orgID, hcID, mgmtCluster string, windowStart, windowEnd time.Time, a Advice, p Policy) error {
	signals := a.NSignals
	if signals == nil {
		signals = []string{}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO hcp_correlation_advisories (
			org_id, hc_cluster_id, management_cluster_uuid,
			window_start, window_end, verdict, confidence,
			h_p99_s, h_threshold_s, c_ratio, n_signals,
			expires_at
		) VALUES ($1,$2,$3,$4,$5,'do_not_add_workers_first','high',$6,$7,$8,$9,$10)
		ON CONFLICT (org_id, hc_cluster_id, window_start)
		DO UPDATE SET verdict = EXCLUDED.verdict, confidence = EXCLUDED.confidence,
			h_p99_s = EXCLUDED.h_p99_s, h_threshold_s = EXCLUDED.h_threshold_s,
			c_ratio = EXCLUDED.c_ratio, n_signals = EXCLUDED.n_signals,
			expires_at = EXCLUDED.expires_at`,
		orgID, hcID, mgmtCluster,
		windowStart.UTC(), windowEnd.UTC(),
		a.HCP99, a.HThreshold, a.CRatio, signals,
		windowEnd.UTC().Add(time.Duration(p.AdvisoryExpiryHours)*time.Hour))
	if err != nil {
		return fmt.Errorf("write hcp advisory: %w", err)
	}
	metrics.HCPAdvisoriesTotal.WithLabelValues(advisoryVerdict).Inc()
	return nil
}

// sweepExpired deletes lapsed advisories; every run self-cleans so no
// housekeeper sweep entry is needed.
func sweepExpired(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	res, err := pool.Exec(ctx, `DELETE FROM hcp_correlation_advisories WHERE expires_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("sweep expired hcp advisories: %w", err)
	}
	return res.RowsAffected(), nil
}

// windowProof requires a complete snapshot observed no earlier than the
// fire window (minus skew): association must be proven FOR this window, not
// merely sometime in the lookback. No upper bound — a fresher proof only
// strengthens the claim — combined with the strict incarnation rule in
// ResolveHCAssociation (a recreated HC fails there, not here).
func windowProof(ctx context.Context, pool *pgxpool.Pool, orgID, mgmtCluster, hcID string, windowStart, windowEnd time.Time, p Policy) bool {
	snaps, err := pgrec.LoadHCPSnapshotsForRun(ctx, pool, orgID, mgmtCluster,
		windowStart.Add(-time.Duration(p.SkewToleranceMinutes)*time.Minute))
	if err != nil || len(snaps) == 0 {
		return false
	}
	floor := windowStart.Add(-time.Duration(p.SkewToleranceMinutes) * time.Minute)
	proven := false
	for _, s := range snaps {
		if !s.Complete || s.HostedClusterID != hcID {
			continue
		}
		if s.ObservedAt.Equal(floor) || s.ObservedAt.After(floor) {
			proven = true
			break
		}
	}
	if !proven {
		return false
	}
	return ResolveHCAssociationForHC(snaps, hcID)
}

// ResolveHCAssociationForHC reports whether the snapshot set proves one
// incarnation for hc (strict rule shared with the association path).
func ResolveHCAssociationForHC(snaps []pgrec.SnapshotRow, hcID string) bool {
	seen := make(map[[2]string]bool)
	n := 0
	for _, s := range snaps {
		if !s.Complete || s.HostedClusterID != hcID {
			continue
		}
		n++
		seen[[2]string{s.HostedClusterID, s.HcUID}] = true
	}
	return n > 0 && len(seen) == 1
}

// CycleResult tallies one correlator run for logs and metrics.
type CycleResult struct {
	Fired  int
	Silent int
}

// RunCycle evaluates the last complete hour for every evidenced HC and
// writes advisories for high-confidence H && C && !N only. It never fails
// on evidence problems: those are silence, and silence is unmetered except
// for the run counter. Infrastructure errors (DB down) do return errors.
func RunCycle(ctx context.Context, pool *pgxpool.Pool) (CycleResult, error) {
	var res CycleResult
	cands, err := enumerateCandidates(ctx, pool)
	if err != nil {
		if pgrec.IsUndefinedTable(err) {
			// Pre-migration database (code ahead of SLO/advisory tables):
			// degrade to an empty run, never error-loop hourly.
			logging.GetLogger().Warnf("correlator: SLO tables unavailable (pre-migration, skipping): %v", err)
			return res, nil
		}
		return res, err
	}
	for _, c := range cands {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		p := policyForOrg(ctx, pool, c.orgID)
		windowEnd := time.Now().UTC().Truncate(time.Hour)
		windowStart := windowEnd.Add(-time.Duration(p.HWindowHours) * time.Hour)
		advice, known, err := evaluateCandidate(ctx, pool, c, windowStart, windowEnd, p)
		if err != nil {
			logging.ForOrg(c.orgID, c.hcID).Warnf("correlator: evaluation failed, silent: %v", err)
			res.Silent++
			continue
		}
		if !known || !advice.Fire {
			res.Silent++
			continue
		}
		if err := writeAdvisory(ctx, pool, c.orgID, c.hcID, c.clusterUUID, windowStart, windowEnd, advice, p); err != nil {
			return res, err
		}
		res.Fired++
	}
	if swept, err := sweepExpired(ctx, pool); err != nil {
		logging.GetLogger().Warnf("correlator: expiry sweep failed: %v", err)
	} else if swept > 0 {
		logging.GetLogger().Infof("correlator: swept %d expired advisories", swept)
	}
	// Thin W5 (#393): webhook advisories share the sweep, metrics, and
	// never-fail-on-evidence contract. Independent of the HC pass.
	if taxFired, err := RunAPITaxCycle(ctx, pool); err != nil {
		logging.GetLogger().Warnf("correlator: api tax cycle failed: %v", err)
	} else if taxFired > 0 {
		logging.GetLogger().Infof("correlator: fired %d api tax advisories", taxFired)
	}
	// W3 (#658): zombie advisories share the same contract. Daily
	// windows refresh in place; independent of both passes above.
	if zombieFired, err := RunZombieCycle(ctx, pool); err != nil {
		logging.GetLogger().Warnf("correlator: zombie cycle failed: %v", err)
	} else if zombieFired > 0 {
		logging.GetLogger().Infof("correlator: fired %d zombie advisories", zombieFired)
	}
	metrics.HCPCorrelatorRunsTotal.WithLabelValues("fired").Add(float64(res.Fired))
	metrics.HCPCorrelatorRunsTotal.WithLabelValues("silent").Add(float64(res.Silent))
	return res, nil
}

// evaluateCandidate runs H/C/N for one HC and applies the verdict rule.
// known=false means silence regardless of Fire.
func evaluateCandidate(ctx context.Context, pool *pgxpool.Pool, c hcCandidate, windowStart, windowEnd time.Time, p Policy) (Advice, bool, error) {
	hP99, hThreshold, hHigh, hKnown := evalH(ctx, pool, c.orgID, c.hcID, windowEnd, p)
	if !hKnown {
		return Advice{}, false, nil
	}
	cRatio, cHigh, cKnown := evalC(ctx, pool, c.orgID, c.clusterUUID, c.hcID, p)
	if !cKnown {
		return Advice{}, false, nil
	}
	nPressured, nKnown, nSignals, nTotal, nHot := evalN(ctx, pool, c.orgID, c.hcID, p)
	if !nKnown {
		return Advice{}, false, nil
	}
	// Persist the derived pressure row for audit and future M3 use (#644):
	// written only when fresh evidence exists, never synthesized otherwise.
	coverage := 0.0
	if nTotal > 0 {
		coverage = 100 * float64(nHot) / float64(nTotal)
	}
	if err := ingestion.UpsertWorkerPressureDerived(ctx, pool, c.orgID, c.hcID, c.hcID,
		windowStart, windowEnd, nPressured, coverage, nSignals, windowEnd); err != nil {
		logging.ForOrg(c.orgID, c.hcID).Warnf("correlator: worker pressure persist failed: %v", err)
	}
	if !windowProof(ctx, pool, c.orgID, c.clusterUUID, c.hcID, windowStart, windowEnd, p) {
		return Advice{}, false, nil
	}
	a := Evaluate(hHigh, true, cHigh, true, nPressured, true, hP99, hThreshold, cRatio, nSignals, c.clusterUUID)
	return a, true, nil
}
