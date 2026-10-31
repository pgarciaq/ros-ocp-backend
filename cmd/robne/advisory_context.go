package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/librobne/advisory"
	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// loadAdvisoryContext loads the extra --input payloads and extracts the
// cross-cluster facts the W8 sleep advisory needs, joined on the primary
// cluster's identity (#721, option d — the design record and the deferred
// generalizations live on #721/#725).
//
// Per-payload consistency: each context payload's manifest must match its
// own CSVs (the W1-review guard, applied per load — the same trap the
// primary enforces). A context payload never scopes the primary's
// guardrails; only these explicit joins consume it.
func loadAdvisoryContext(fl *fileLoad, _ commonFlags, extraInputs []string, primaryClusterID string) error {
	var latestObserved string
	for _, in := range extraInputs {
		if isPostgresURL(in) {
			return fmt.Errorf("context --input %q: a postgres:// URL is valid only as the first (primary) input", in)
		}
		if _, err := os.Stat(in); err != nil {
			return fmt.Errorf("context --input %q: %w", in, err)
		}
		loaded, err := csv.Load(in)
		if err != nil {
			return fmt.Errorf("context --input %q: %w", in, err)
		}
		// Per-payload guard: the context manifest must agree with the
		// cluster identity its own rows carry (empty either side skips —
		// a topology-only payload has no cluster-bearing CSVs).
		if m := loaded.Manifest; m != nil && m.ClusterUUID != "" {
			if rowsID, rerr := resolveClusterIDFromLoad(fl.cfg, loaded); rerr == nil && rowsID != "" &&
				!strings.EqualFold(m.ClusterUUID, rowsID) {
				return fmt.Errorf("context --input %q: manifest cluster_id %q does not match payload cluster %q",
					in, m.ClusterUUID, rowsID)
			}
		}

		// Tier (latest-wins over the topology entries naming the primary).
		if m := loaded.Manifest; m != nil {
			for _, e := range m.Topology.HCPSnapshot {
				if e.HostedClusterID != primaryClusterID || !e.Complete {
					continue
				}
				if e.ObservedAt >= latestObserved {
					latestObserved = e.ObservedAt
					fl.hcTier = e.HcTier
					fl.hcContextLoaded = true
				}
			}
		}

		// Worker nodes: latest spec_replicas per pool for this HC.
		latest := map[string]int64{}
		for _, r := range loaded.NodepoolRows {
			if r.HCClusterID != primaryClusterID {
				continue
			}
			if prev, ok := latest[r.PoolName]; !ok || !r.CollectedAt.IsZero() {
				_ = prev
				latest[r.PoolName] = r.SpecReplicas
			}
		}
		var sum int64
		for _, v := range latest {
			sum += v
		}
		if sum > 0 || len(latest) > 0 {
			fl.hcWorkerNodes = int(sum)
			fl.hcContextLoaded = true
		}
	}
	return nil
}

// computeSleepAdvisory runs the W8 sleep-candidate evaluation over the
// primary's dual digests and the loaded context facts (#721). Silence on
// any missing input mirrors the server's never-fail-on-evidence posture:
// no schedule, no context facts, or no business-hours digests means no
// advisory row — never an error.
func computeSleepAdvisory(fl fileLoad) []advisory.Advisory {
	if !fl.bhEnabled || !fl.hcContextLoaded || fl.hcTier == "" {
		return nil
	}
	if len(fl.containerBHDigests) == 0 || len(fl.containerDigests) == 0 {
		return nil
	}
	pol := sleepPolicyFromYAML(fl.cfg.SleepAdvisory)
	syncedExact, syncedPrefixes := platformExclusionsFromYAML(fl.cfg.SleepAdvisory)
	sums := advisory.DaySums(fl.containerDigests, fl.containerBHDigests)
	got := advisory.SleepCandidate(
		fl.clusterID, fl.hcTier, sums, fl.bhSchedule, pol,
		fl.hcWorkerNodes, syncedExact, syncedPrefixes, fl.now)
	if got == nil {
		return nil
	}
	return []advisory.Advisory{*got}
}

// sortAdvisoryOut orders output deterministically (by HC then verdict).
func sortAdvisoryOut(a []advisory.Advisory) {
	sort.Slice(a, func(i, j int) bool {
		if a[i].HostedClusterID != a[j].HostedClusterID {
			return a[i].HostedClusterID < a[j].HostedClusterID
		}
		return a[i].Verdict < a[j].Verdict
	})
}
