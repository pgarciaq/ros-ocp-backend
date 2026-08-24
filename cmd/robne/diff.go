package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff LEFT.json RIGHT.json",
		Short: "Compare two recommend JSON envelopes",
		Long: `Compare two robne recommend --format json envelopes on disk.
Does not re-run recommend or require Postgres.

Exit codes: 0 identical, 1 recs or metadata differ, 2 unreadable JSON / version mismatch / duplicate row keys.

Row identity is the persist key (not array index). Envelope version mismatch is a hard error.
Empty sibling array vs missing key is a delta. cluster_id, now, and skipped_rows are deltas.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd.OutOrStdout(), args[0], args[1])
		},
	}
	return cmd
}

func runDiff(stdout io.Writer, leftPath, rightPath string) error {
	left, err := loadEnvelope(leftPath)
	if err != nil {
		return exitErr(2, err)
	}
	right, err := loadEnvelope(rightPath)
	if err != nil {
		return exitErr(2, err)
	}
	report, differs, err := compareEnvelopes(left, right)
	if err != nil {
		return exitErr(2, err)
	}
	if !differs {
		_, err = fmt.Fprintln(stdout, "identical")
		return err
	}
	if _, err := io.WriteString(stdout, report); err != nil {
		return err
	}
	if !strings.HasSuffix(report, "\n") {
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}
	return exitSilent(1)
}

func loadEnvelope(path string) (recommendJSON, error) {
	var env recommendJSON
	raw, err := os.ReadFile(path) //nolint:gosec // G304: operator-supplied CLI path
	if err != nil {
		return env, err
	}
	if !json.Valid(raw) {
		return env, fmt.Errorf("%s: invalid JSON", path)
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, fmt.Errorf("%s: %w", path, err)
	}
	if env.Version < 1 {
		return env, fmt.Errorf("%s: not a robne recommend JSON envelope (missing version)", path)
	}
	return env, nil
}

func compareEnvelopes(left, right recommendJSON) (string, bool, error) {
	if left.Version != right.Version {
		return "", false, fmt.Errorf("envelope version mismatch: left %d, right %d", left.Version, right.Version)
	}
	var b strings.Builder
	differs := false
	writeMeta := func(name, l, r string) {
		if l == r {
			return
		}
		differs = true
		fmt.Fprintf(&b, "%s: %s → %s\n", name, l, r)
	}
	writeMeta("cluster_id", left.ClusterID, right.ClusterID)
	writeMeta("now", left.Now, right.Now)
	writeMeta("skipped_rows", fmt.Sprintf("%d", left.SkippedRows), fmt.Sprintf("%d", right.SkippedRows))

	add := func(lines []string, err error) error {
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return nil
		}
		differs = true
		for _, line := range lines {
			b.WriteString(line)
			if !strings.HasSuffix(line, "\n") {
				b.WriteByte('\n')
			}
		}
		return nil
	}

	if err := add(diffSlice("recommendations", slicePtr(left.Recommendations), slicePtr(right.Recommendations), containerKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("business_hours_recommendations", left.BusinessHoursRecommendations, right.BusinessHoursRecommendations, containerKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("namespace_recommendations", left.NamespaceRecommendations, right.NamespaceRecommendations, namespaceKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("business_hours_namespace_recommendations", left.BusinessHoursNamespaceRecommendations, right.BusinessHoursNamespaceRecommendations, namespaceKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("node_recommendations", left.NodeRecommendations, right.NodeRecommendations, nodeKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("gpu_recommendations", left.GPURecommendations, right.GPURecommendations, gpuKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("gpu_timeslicing_recommendations", left.GPUTimeslicingRecommendations, right.GPUTimeslicingRecommendations, gpuTimeslicingKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("business_hours_node_recommendations", left.BusinessHoursNodeRecommendations, right.BusinessHoursNodeRecommendations, nodeKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("business_hours_gpu_recommendations", left.BusinessHoursGPURecommendations, right.BusinessHoursGPURecommendations, gpuKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("business_hours_gpu_timeslicing_recommendations", left.BusinessHoursGPUTimeslicingRecommendations, right.BusinessHoursGPUTimeslicingRecommendations, gpuTimeslicingKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("business_hours_vm_recommendations", left.BusinessHoursVMRecommendations, right.BusinessHoursVMRecommendations, vmKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("pvc_recommendations", left.PVCRecommendations, right.PVCRecommendations, pvcKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("vm_recommendations", left.VMRecommendations, right.VMRecommendations, vmKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("quota_recommendations", left.QuotaRecommendations, right.QuotaRecommendations, quotaKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("cluster_quota_recommendations", left.ClusterQuotaRecommendations, right.ClusterQuotaRecommendations, clusterQuotaKey)); err != nil {
		return "", false, err
	}
	if err := add(diffSlice("snapshot_recommendations", left.SnapshotRecommendations, right.SnapshotRecommendations, snapshotKey)); err != nil {
		return "", false, err
	}
	return b.String(), differs, nil
}

func slicePtr[T any](s []T) *[]T {
	return &s
}

func diffSlice[T any](name string, left, right *[]T, keyFn func(T) string) ([]string, error) {
	if left == nil && right == nil {
		return nil, nil
	}
	if left == nil {
		return []string{fmt.Sprintf("%s: missing → %s", name, arraySummary(right))}, nil
	}
	if right == nil {
		return []string{fmt.Sprintf("%s: %s → missing", name, arraySummary(left))}, nil
	}
	lm, err := indexRows(*left, name+" (left)", keyFn)
	if err != nil {
		return nil, err
	}
	rm, err := indexRows(*right, name+" (right)", keyFn)
	if err != nil {
		return nil, err
	}
	var added, removed, changed []string
	for k := range rm {
		if _, ok := lm[k]; !ok {
			added = append(added, k)
		}
	}
	for k := range lm {
		if _, ok := rm[k]; !ok {
			removed = append(removed, k)
		}
	}
	for k, lv := range lm {
		rv, ok := rm[k]
		if !ok {
			continue
		}
		fields := fieldDiffs(lv, rv)
		if len(fields) == 0 {
			continue
		}
		changed = append(changed, k+" "+strings.Join(fields, ", "))
	}
	if len(added) == 0 && len(removed) == 0 && len(changed) == 0 {
		return nil, nil
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	lines := []string{fmt.Sprintf("%s: %d added, %d removed, %d changed", name, len(added), len(removed), len(changed))}
	for _, k := range added {
		lines = append(lines, "  + "+k)
	}
	for _, k := range removed {
		lines = append(lines, "  - "+k)
	}
	for _, k := range changed {
		lines = append(lines, "  ~ "+k)
	}
	return lines, nil
}

func arraySummary[T any](s *[]T) string {
	if s == nil {
		return "missing"
	}
	if len(*s) == 0 {
		return "[]"
	}
	return fmt.Sprintf("[%d]", len(*s))
}

func indexRows[T any](rows []T, side string, keyFn func(T) string) (map[string]T, error) {
	out := make(map[string]T, len(rows))
	for _, row := range rows {
		k := keyFn(row)
		if _, ok := out[k]; ok {
			return nil, fmt.Errorf("%s: duplicate row key %q", side, k)
		}
		out[k] = row
	}
	return out, nil
}

func fieldDiffs(left, right any) []string {
	lm := jsonFields(left)
	rm := jsonFields(right)
	keys := make([]string, 0, len(lm)+len(rm))
	seen := map[string]struct{}{}
	for k := range lm {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range rm {
		if _, ok := seen[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var diffs []string
	for _, k := range keys {
		lv, lok := lm[k]
		rv, rok := rm[k]
		switch {
		case !lok:
			diffs = append(diffs, fmt.Sprintf("%s: missing → %s", k, rv))
		case !rok:
			diffs = append(diffs, fmt.Sprintf("%s: %s → missing", k, lv))
		case lv != rv:
			diffs = append(diffs, fmt.Sprintf("%s: %s → %s", k, lv, rv))
		}
	}
	return diffs
}

func jsonFields(v any) map[string]string {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]string{"<marshal>": err.Error()}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]string{"<unmarshal>": err.Error()}
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		jb, err := json.Marshal(val)
		if err != nil {
			out[k] = fmt.Sprint(val)
			continue
		}
		out[k] = string(jb)
	}
	return out
}

func containerKey(r containerOut) string {
	return fmt.Sprintf("%s/%s/%s/%s %s/%s", r.Namespace, r.Workload, r.WorkloadType, r.ContainerName, r.Term, r.Engine)
}

func namespaceKey(r namespaceOut) string {
	return fmt.Sprintf("%s %s/%s", r.Namespace, r.Term, r.Engine)
}

func nodeKey(r nodeOut) string {
	return fmt.Sprintf("%s %s/%s", r.Node, r.Term, r.Engine)
}

func gpuKey(r gpuOut) string {
	return fmt.Sprintf("%s/%s/%s %s/%s", r.Namespace, r.Workload, r.ContainerName, r.Term, r.GPUModelName)
}

func gpuTimeslicingKey(r gpuTimeslicingOut) string {
	return fmt.Sprintf("%s/%s %s", r.Node, r.GPUModel, r.Term)
}

func pvcKey(r pvcOut) string {
	return fmt.Sprintf("%s/%s %s", r.Namespace, r.PVC, r.Term)
}

func vmKey(r vmOut) string {
	return fmt.Sprintf("%s/%s %s/%s", r.Namespace, r.VMName, r.Term, r.Engine)
}

func quotaKey(r quotaOut) string {
	return fmt.Sprintf("%s/%s %s", r.Namespace, r.QuotaName, r.RecommendationType)
}

func clusterQuotaKey(r clusterQuotaOut) string {
	return fmt.Sprintf("%s %s", r.ClusterQuotaName, r.RecommendationType)
}

func snapshotKey(r snapshotOut) string {
	return fmt.Sprintf("%s/%s", r.Namespace, r.SnapshotName)
}
