package main

import (
	"context"
	"fmt"
	"os"

	"github.com/redhatinsights/ros-ocp-backend/librobne/container"
	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/engine"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/spf13/cobra"
)

func newRecommendCmd() *cobra.Command {
	var f commonFlags
	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Compute container recommendations from ROS CSV input",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRecommend(f)
		},
	}
	bindCommonFlags(cmd, &f, true, true)
	return cmd
}

func runRecommend(f commonFlags) error {
	result, err := computeRecommendations(f)
	if err != nil {
		return err
	}
	return writeRecs(os.Stdout, result, f.format)
}

func computeRecommendations(f commonFlags) (recommendResult, error) {
	var out recommendResult
	env, err := overlayEnvFromOS(f.noUserConfig)
	if err != nil {
		return out, err
	}
	cfg, err := loadFileConfig(env, f.configPath)
	if err != nil {
		return out, err
	}
	if err := validatePlugins(cfg, f.plugins); err != nil {
		return out, err
	}
	loaded, err := csv.Load(f.input)
	if err != nil {
		return out, err
	}
	reportUnparseableRows(loaded.RowsSkipped)
	clusterID, err := resolveClusterID(cfg, loaded.Rows)
	if err != nil {
		return out, err
	}
	digests, ds, err := csv.DailyDigests(loaded.Rows)
	if err != nil {
		return out, err
	}
	now, err := parseNow(f.now, cfg, ds.MaxEnd)
	if err != nil {
		return out, err
	}
	orgID := cfg.OrgID
	ec := engineConfigFromFile(cfg, orgID, clusterID, now)
	ec.ClusterLastReported = ds.MaxEnd

	var recs []types.ContainerRec
	err = engine.RecommendWorkloads(context.Background(), digests, ec, func(batch []types.ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	if err != nil {
		return out, err
	}

	cardFile, err := loadRateCardFile(env, f.rateCardPath)
	if err != nil {
		return out, err
	}
	hours := types.HoursInMonth(now.Year(), now.Month())
	if err := applySavings(recs, cardFile, clusterID, ds.Meta, hours); err != nil {
		return out, err
	}
	out.Recs = recs
	out.ClusterID = clusterID
	out.Now = now
	out.SkippedRows = loaded.RowsSkipped
	return out, nil
}

func applySavings(
	recs []types.ContainerRec,
	file *rateCardFile,
	clusterID string,
	meta map[types.ContainerKey]csv.RowMeta,
	hoursPerMonth int64,
) error {
	if file == nil {
		container.ApplySavingsEstimates(recs, nil, hoursPerMonth)
		return nil
	}
	groups := map[types.ContainerKey][]int{}
	for i := range recs {
		k := types.ContainerKey{
			Namespace:     recs[i].Namespace,
			Workload:      recs[i].Workload,
			WorkloadType:  recs[i].WorkloadType,
			ContainerName: recs[i].ContainerName,
		}
		groups[k] = append(groups[k], i)
	}
	for k, idxs := range groups {
		card, err := rateCardForRow(file, clusterID, meta[k], hoursPerMonth)
		if err != nil {
			return err
		}
		subset := make([]types.ContainerRec, len(idxs))
		for j, idx := range idxs {
			subset[j] = recs[idx]
		}
		container.ApplySavingsEstimates(subset, card, hoursPerMonth)
		for j, idx := range idxs {
			recs[idx] = subset[j]
		}
	}
	return nil
}

func newValidateCmd() *cobra.Command {
	var f commonFlags
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate ROS container CSV input without computing recommendations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runValidate(f)
		},
	}
	bindCommonFlags(cmd, &f, false, false)
	return cmd
}

func runValidate(f commonFlags) error {
	env, err := overlayEnvFromOS(f.noUserConfig)
	if err != nil {
		return err
	}
	cfg, err := loadFileConfig(env, f.configPath)
	if err != nil {
		return err
	}
	loaded, err := csv.Load(f.input)
	if err != nil {
		return err
	}
	reportUnparseableRows(loaded.RowsSkipped)
	clusterID, err := resolveClusterID(cfg, loaded.Rows)
	if err != nil {
		return err
	}
	_, ds, err := csv.DailyDigests(loaded.Rows)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "files: %d\n", len(loaded.Files)); err != nil {
		return err
	}
	for _, name := range loaded.Files {
		if _, err := fmt.Fprintf(os.Stdout, "  %s\n", name); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(os.Stdout, "rows: %d\n", len(loaded.Rows)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(os.Stdout, "cluster_id: %s\n", clusterID); err != nil {
		return err
	}
	if !ds.MaxEnd.IsZero() {
		if _, err := fmt.Fprintf(os.Stdout, "max_interval_end: %s\n", ds.MaxEnd.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
			return err
		}
	}
	if len(loaded.CostOnlySkipped) > 0 {
		if _, err := fmt.Fprintf(os.Stdout, "skipped_cost_only: %v\n", loaded.CostOnlySkipped); err != nil {
			return err
		}
	}
	fmt.Fprintln(os.Stdout, "ok")
	return nil
}

func reportUnparseableRows(n int) {
	if n <= 0 {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "skipped %d unparseable rows\n", n)
}
