package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/librobne/container"
	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/engine"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgdigest"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/spf13/cobra"
)

func newRecommendCmd() *cobra.Command {
	var f commonFlags
	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Compute container and namespace recommendations from ROS CSV or stored digests",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRecommend(f)
		},
	}
	bindCommonFlags(cmd, &f, true, true)
	cmd.Flags().StringVar(&f.output, "output", "", "postgres:// or postgresql:// URL for a CLI-owned database (use case c)")
	cmd.Flags().StringVar(&f.pgURLFile, "pg-url-file", "", "file containing a postgres URL (password off argv)")
	cmd.Flags().BoolVar(&f.applySchema, "apply-schema", false, "bootstrap or upgrade embedded migrations on a dedicated database")
	return cmd
}

func runRecommend(f commonFlags) error {
	result, err := executeRecommend(f)
	if err != nil {
		return err
	}
	return writeRecs(os.Stdout, result, f.format)
}

func executeRecommend(f commonFlags) (recommendResult, error) {
	var out recommendResult
	if isPostgresURL(f.input) {
		if f.applySchema {
			return out, fmt.Errorf("recompute does not migrate; use files with --output postgres:// and --apply-schema to bootstrap a dedicated database")
		}
		if err := rejectMismatchedPostgres(f); err != nil {
			return out, err
		}
		return executePathA(context.Background(), f)
	}
	if f.output != "" || f.pgURLFile != "" {
		return executePathB(context.Background(), f)
	}
	return computeRecommendations(f)
}

func rejectMismatchedPostgres(f commonFlags) error {
	if f.output == "" && f.pgURLFile == "" {
		return nil
	}
	in, err := pathADSN(f)
	if err != nil {
		return err
	}
	out, err := resolvePostgresDSN(f.output, f.pgURLFile)
	if err != nil {
		return err
	}
	if !samePostgresDB(in, out) {
		return fmt.Errorf("--input and --output PostgreSQL URLs must be the same database")
	}
	return nil
}

func pathADSN(f commonFlags) (string, error) {
	if f.pgURLFile != "" {
		return resolvePostgresDSN("postgres://", f.pgURLFile)
	}
	return resolvePostgresDSN(f.input, "")
}

func persistRecommendations(ctx context.Context, f commonFlags, result recommendResult) error {
	if err := requirePostgresIdentity(result.OrgID, result.ClusterID); err != nil {
		return err
	}
	dsn, err := resolvePostgresDSN(f.output, f.pgURLFile)
	if err != nil {
		return err
	}
	pool, err := openCLIPool(ctx, dsn, f.applySchema)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := persistDigestsOnPool(ctx, pool, result); err != nil {
		return err
	}
	return persistRecsOnPool(ctx, pool, result)
}

func openCLIPool(ctx context.Context, dsn string, applySchema bool) (*pgxpool.Pool, error) {
	pool, err := openPostgres(ctx, dsn, applySchema)
	if err != nil {
		return nil, err
	}
	if err := pgrec.AssertCLIOwned(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func persistDigestsOnPool(ctx context.Context, pool *pgxpool.Pool, result recommendResult) error {
	if err := requirePostgresIdentity(result.OrgID, result.ClusterID); err != nil {
		return err
	}
	if err := pgrec.EnsureAccountCluster(ctx, pool, result.OrgID, result.ClusterID, result.Now); err != nil {
		return err
	}
	return pgdigest.WriteContainerDigests(ctx, pool, result.OrgID, result.ClusterID, result.Digests)
}

func persistRecsOnPool(ctx context.Context, pool *pgxpool.Pool, result recommendResult) error {
	if err := requirePostgresIdentity(result.OrgID, result.ClusterID); err != nil {
		return err
	}
	if err := pgrec.EnsureAccountCluster(ctx, pool, result.OrgID, result.ClusterID, result.Now); err != nil {
		return err
	}
	cycleStart := time.Now()
	if err := pgrec.WriteRecommendations(ctx, pool, result.Recs); err != nil {
		return err
	}
	if err := pgrec.RefreshOrgMetadata(ctx, pool, result.OrgID); err != nil {
		return err
	}
	_, err := pgrec.MarkUnreportedContainersStale(ctx, pool, result.OrgID, result.ClusterID, cycleStart)
	return err
}

func executePathA(ctx context.Context, f commonFlags) (recommendResult, error) {
	var out recommendResult
	loaded, err := loadRecommendConfig(f)
	if err != nil {
		return out, err
	}
	plugins := resolvedPlugins(loaded.cfg, f.plugins)
	if err := rejectNamespacePostgresInput(plugins, true); err != nil {
		return out, err
	}
	if shouldWarnNamespaceSkippedOnPostgres(plugins, true) {
		_, _ = fmt.Fprint(os.Stderr, "namespace plugin ignored for --input postgres:// (stored namespace digests are not selected yet; see #473)\n")
	}
	if err := requirePostgresIdentity(loaded.cfg.OrgID, loaded.cfg.ClusterUUID); err != nil {
		return out, err
	}
	dsn, err := pathADSN(f)
	if err != nil {
		return out, err
	}
	pool, err := openCLIPool(ctx, dsn, false)
	if err != nil {
		return out, err
	}
	defer pool.Close()

	maxDate, maxErr := pgdigest.MaxBucketDate(ctx, pool, loaded.cfg.OrgID, loaded.cfg.ClusterUUID)
	now, err := parseNow(f.now, loaded.cfg, maxDate)
	if err != nil {
		if maxErr != nil {
			return out, maxErr
		}
		return out, err
	}
	ec := engineConfigFromFile(loaded.cfg, loaded.cfg.OrgID, loaded.cfg.ClusterUUID, now)
	start, end := digestWindow(ec.Terms, now)
	digests, err := pgdigest.ReadContainerDigests(ctx, pool, loaded.cfg.OrgID, loaded.cfg.ClusterUUID, start, end)
	if err != nil {
		return out, err
	}
	if len(digests) == 0 {
		return out, fmt.Errorf("no digest rows for org_id=%s cluster_uuid=%s", loaded.cfg.OrgID, loaded.cfg.ClusterUUID)
	}
	out, err = recommendFromDigests(f, loaded.cfg, loaded.cfg.OrgID, loaded.cfg.ClusterUUID, now, digests, nil, 0)
	if err != nil {
		return out, err
	}
	out.plugins = plugins
	if f.output != "" || f.pgURLFile != "" {
		if err := persistRecsOnPool(ctx, pool, out); err != nil {
			return out, err
		}
	}
	return out, nil
}

func executePathB(ctx context.Context, f commonFlags) (recommendResult, error) {
	var out recommendResult
	fl, err := loadFiles(f)
	if err != nil {
		return out, err
	}
	if shouldWarnNamespaceNotPersisted(fl.plugins, true) {
		_, _ = fmt.Fprint(os.Stderr, "namespace recommendations are written to stdout only; --output postgres:// still persists container recs only (see #473)\n")
	}
	if err := requirePostgresIdentity(fl.orgID, fl.clusterID); err != nil {
		return out, err
	}
	dsn, err := resolvePostgresDSN(f.output, f.pgURLFile)
	if err != nil {
		return out, err
	}
	pool, err := openCLIPool(ctx, dsn, f.applySchema)
	if err != nil {
		return out, err
	}
	defer pool.Close()

	fileOut := recommendResult{
		Digests:     fl.containerDigests,
		ClusterID:   fl.clusterID,
		OrgID:       fl.orgID,
		Now:         fl.now,
		SkippedRows: fl.skipped,
		plugins:     fl.plugins,
	}
	if pluginEnabled(fl.plugins, "container") && len(fl.containerDigests) > 0 {
		if err := persistDigestsOnPool(ctx, pool, fileOut); err != nil {
			return out, err
		}
		maxDate, err := pgdigest.MaxBucketDate(ctx, pool, fl.orgID, fl.clusterID)
		if err != nil {
			return out, err
		}
		now := fl.now
		if !maxDate.IsZero() && f.now == "" {
			now = maxDate
		}
		ec := engineConfigFromFile(fl.cfg, fl.orgID, fl.clusterID, now)
		start, end := digestWindow(ec.Terms, now)
		digests, err := pgdigest.ReadContainerDigests(ctx, pool, fl.orgID, fl.clusterID, start, end)
		if err != nil {
			return out, err
		}
		if len(digests) == 0 {
			return out, fmt.Errorf("no digest rows for org_id=%s cluster_uuid=%s", fl.orgID, fl.clusterID)
		}
		out, err = recommendFromDigests(f, fl.cfg, fl.orgID, fl.clusterID, now, digests, fl.containerMeta, fl.skipped)
		if err != nil {
			return out, err
		}
	} else {
		out.ClusterID = fl.clusterID
		out.OrgID = fl.orgID
		out.Now = fl.now
		out.SkippedRows = fl.skipped
	}
	out.plugins = fl.plugins
	if pluginEnabled(fl.plugins, "namespace") {
		nsRecs, err := recommendNamespaces(fl)
		if err != nil {
			return out, err
		}
		out.NamespaceRecs = nsRecs
	}
	if pluginEnabled(fl.plugins, "container") && len(out.Recs) > 0 {
		if err := persistRecsOnPool(ctx, pool, out); err != nil {
			return out, err
		}
	}
	return out, nil
}

type loadedConfig struct {
	cfg fileConfig
}

func loadRecommendConfig(f commonFlags) (loadedConfig, error) {
	var out loadedConfig
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
	out.cfg = cfg
	return out, nil
}

func loadFiles(f commonFlags) (fileLoad, error) {
	var out fileLoad
	loaded, err := loadRecommendConfig(f)
	if err != nil {
		return out, err
	}
	out.cfg = loaded.cfg
	out.plugins = resolvedPlugins(out.cfg, f.plugins)
	csvLoaded, err := csv.Load(f.input)
	if err != nil {
		return out, err
	}
	reportUnparseableRows(csvLoaded.RowsSkipped)
	wantC := pluginEnabled(out.plugins, "container")
	wantNS := pluginEnabled(out.plugins, "namespace")
	if wantC && len(csvLoaded.Rows) == 0 {
		return out, fmt.Errorf("no ROS container CSV found (namespace files require --plugins namespace)")
	}
	if wantNS && len(csvLoaded.NamespaceRows) == 0 {
		return out, fmt.Errorf("no ROS namespace CSV found")
	}
	clusterID, err := resolveClusterIDFromLoad(out.cfg, csvLoaded)
	if err != nil {
		return out, err
	}
	var maxEnd time.Time
	if wantC {
		digests, ds, err := csv.DailyDigests(csvLoaded.Rows)
		if err != nil {
			return out, err
		}
		out.containerDigests = digests
		out.containerMeta = ds.Meta
		maxEnd = ds.MaxEnd
	} else {
		out.containerMeta = map[types.ContainerKey]csv.RowMeta{}
	}
	if wantNS {
		grouped, ds, err := csv.DailyNamespaceDigests(csvLoaded.NamespaceRows)
		if err != nil {
			return out, err
		}
		out.namespaceGrouped = toNamespaceKeys(grouped)
		if ds.MaxEnd.After(maxEnd) {
			maxEnd = ds.MaxEnd
		}
	}
	now, err := parseNow(f.now, out.cfg, maxEnd)
	if err != nil {
		return out, err
	}
	out.clusterID = clusterID
	out.orgID = out.cfg.OrgID
	out.now = now
	out.skipped = csvLoaded.RowsSkipped
	return out, nil
}

type fileLoad struct {
	cfg              fileConfig
	plugins          []string
	clusterID        string
	orgID            string
	now              time.Time
	skipped          int
	containerDigests []types.KeyedDigest
	containerMeta    map[types.ContainerKey]csv.RowMeta
	namespaceGrouped map[namespace.NamespaceKey][]types.DigestRow
}

func toNamespaceKeys(grouped map[string][]types.DigestRow) map[namespace.NamespaceKey][]types.DigestRow {
	out := make(map[namespace.NamespaceKey][]types.DigestRow, len(grouped))
	for ns, rows := range grouped {
		out[namespace.NamespaceKey{Namespace: ns}] = rows
	}
	return out
}

func recommendFromDigests(
	f commonFlags,
	cfg fileConfig,
	orgID, clusterID string,
	now time.Time,
	digests []types.KeyedDigest,
	meta map[types.ContainerKey]csv.RowMeta,
	skipped int,
) (recommendResult, error) {
	var out recommendResult
	ec := engineConfigFromFile(cfg, orgID, clusterID, now)
	if len(digests) > 0 {
		ec.ClusterLastReported = digests[len(digests)-1].Row.BucketDate
		for _, d := range digests {
			if d.Row.BucketDate.After(ec.ClusterLastReported) {
				ec.ClusterLastReported = d.Row.BucketDate
			}
		}
	}
	var recs []types.ContainerRec
	err := engine.RecommendWorkloads(context.Background(), digests, ec, func(batch []types.ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	if err != nil {
		return out, err
	}
	env, err := overlayEnvFromOS(f.noUserConfig)
	if err != nil {
		return out, err
	}
	cardFile, err := loadRateCardFile(env, f.rateCardPath)
	if err != nil {
		return out, err
	}
	hours := types.HoursInMonth(now.Year(), now.Month())
	if err := applySavings(recs, cardFile, clusterID, meta, hours); err != nil {
		return out, err
	}
	out.Recs = recs
	out.Digests = digests
	out.ClusterID = clusterID
	out.OrgID = orgID
	out.Now = now
	out.SkippedRows = skipped
	return out, nil
}

func computeRecommendations(f commonFlags) (recommendResult, error) {
	var out recommendResult
	fl, err := loadFiles(f)
	if err != nil {
		return out, err
	}
	if pluginEnabled(fl.plugins, "container") {
		out, err = recommendFromDigests(f, fl.cfg, fl.orgID, fl.clusterID, fl.now, fl.containerDigests, fl.containerMeta, fl.skipped)
		if err != nil {
			return out, err
		}
	} else {
		out.ClusterID = fl.clusterID
		out.OrgID = fl.orgID
		out.Now = fl.now
		out.SkippedRows = fl.skipped
	}
	out.plugins = fl.plugins
	if pluginEnabled(fl.plugins, "namespace") {
		nsRecs, err := recommendNamespaces(fl)
		if err != nil {
			return out, err
		}
		out.NamespaceRecs = nsRecs
	}
	return out, nil
}

func recommendNamespaces(fl fileLoad) ([]namespace.NamespaceRec, error) {
	ec := engineConfigFromFile(fl.cfg, fl.orgID, fl.clusterID, fl.now)
	cfg := namespace.NamespaceEngineConfig{
		OrgID:              fl.orgID,
		ClusterUUID:        fl.clusterID,
		End:                fl.now,
		ScheduleType:       namespace.ScheduleAllHours,
		Terms:              ec.Terms,
		Sizing:             ec.Sizing,
		Now:                fl.now,
		StalenessThreshold: ec.StalenessThreshold,
	}
	for _, rows := range fl.namespaceGrouped {
		for _, row := range rows {
			if cfg.ClusterLastReported.IsZero() || row.BucketDate.After(cfg.ClusterLastReported) {
				cfg.ClusterLastReported = row.BucketDate
			}
		}
	}
	return namespace.RecommendNamespaces(context.Background(), fl.namespaceGrouped, cfg)
}

func rejectNamespacePostgresInput(plugins []string, postgresInput bool) error {
	if !postgresInput {
		return nil
	}
	if pluginEnabled(plugins, "namespace") && !pluginEnabled(plugins, "container") {
		return fmt.Errorf("namespace recompute from postgres:// is not supported yet (see #473); use namespace CSV files")
	}
	return nil
}

func shouldWarnNamespaceNotPersisted(plugins []string, persist bool) bool {
	return persist && pluginEnabled(plugins, "namespace")
}

func shouldWarnNamespaceSkippedOnPostgres(plugins []string, postgresInput bool) bool {
	return postgresInput && pluginEnabled(plugins, "namespace") && pluginEnabled(plugins, "container")
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
		Short: "Validate ROS CSV input without computing recommendations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runValidate(f)
		},
	}
	bindCommonFlags(cmd, &f, false, false)
	return cmd
}

func runValidate(f commonFlags) error {
	if isPostgresURL(f.input) {
		return fmt.Errorf("validate reads files only (directory, .csv, or .tar.gz), not a PostgreSQL URL")
	}
	env, err := overlayEnvFromOS(f.noUserConfig)
	if err != nil {
		return err
	}
	cfg, err := loadFileConfig(env, f.configPath)
	if err != nil {
		return err
	}
	if err := validatePlugins(cfg, f.plugins); err != nil {
		return err
	}
	plugins := resolvedPlugins(cfg, f.plugins)
	loaded, err := csv.Load(f.input)
	if err != nil {
		return err
	}
	reportUnparseableRows(loaded.RowsSkipped)
	wantC := pluginEnabled(plugins, "container")
	wantNS := pluginEnabled(plugins, "namespace")
	if wantC && len(loaded.Rows) == 0 {
		return fmt.Errorf("no ROS container CSV found (namespace files require --plugins namespace)")
	}
	if wantNS && len(loaded.NamespaceRows) == 0 {
		return fmt.Errorf("no ROS namespace CSV found")
	}
	clusterID, err := resolveClusterIDFromLoad(cfg, loaded)
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
	if wantNS {
		if _, err := fmt.Fprintf(os.Stdout, "namespace_rows: %d\n", len(loaded.NamespaceRows)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(os.Stdout, "cluster_id: %s\n", clusterID); err != nil {
		return err
	}
	if wantC {
		_, ds, err := csv.DailyDigests(loaded.Rows)
		if err != nil {
			return err
		}
		if !ds.MaxEnd.IsZero() {
			if _, err := fmt.Fprintf(os.Stdout, "max_interval_end: %s\n", ds.MaxEnd.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
				return err
			}
		}
	} else if wantNS {
		_, ds, err := csv.DailyNamespaceDigests(loaded.NamespaceRows)
		if err != nil {
			return err
		}
		if !ds.MaxEnd.IsZero() {
			if _, err := fmt.Fprintf(os.Stdout, "max_interval_end: %s\n", ds.MaxEnd.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
				return err
			}
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
