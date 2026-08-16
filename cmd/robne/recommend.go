package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/librobne/container"
	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/engine"
	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgdigest"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
	"github.com/spf13/cobra"
)

func newRecommendCmd() *cobra.Command {
	var f commonFlags
	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Compute container, namespace, node, GPU, PVC, VM, quota, and cluster_quota recommendations from ROS CSV or stored digests",
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
	if err := rejectFileOnlyPostgresInput(plugins); err != nil {
		return out, err
	}
	if names := fileOnlyPluginNames(plugins); pluginEnabled(plugins, "container") && len(names) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "%s plugin ignored for --input postgres:// (stored digests are not selected yet; see #473)\n", strings.Join(names, ", "))
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
	if names := fileOnlyPluginNames(fl.plugins); len(names) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "%s recommendations are written to stdout only; --output postgres:// still persists container recs only (see #473)\n", strings.Join(names, ", "))
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
	if err := attachFileOnlyRecs(&out, fl); err != nil {
		return out, err
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
	wantNode := pluginEnabled(out.plugins, "node")
	wantGPU := pluginEnabled(out.plugins, "gpu")
	wantPVC := pluginEnabled(out.plugins, "pvc")
	wantVM := pluginEnabled(out.plugins, "vm")
	wantQuota := pluginEnabled(out.plugins, "quota")
	wantCRQ := pluginEnabled(out.plugins, "cluster_quota")
	needContainerRows := wantC || wantNode || wantGPU
	if needContainerRows && len(csvLoaded.Rows) == 0 {
		if wantNode || wantGPU {
			return out, fmt.Errorf("no ROS container CSV found (node and gpu plugins read container ROS rows)")
		}
		return out, fmt.Errorf("no ROS container CSV found (namespace files require --plugins namespace)")
	}
	if (wantNS || wantQuota) && len(csvLoaded.NamespaceRows) == 0 {
		return out, fmt.Errorf("no ROS namespace CSV found")
	}
	if wantCRQ && len(csvLoaded.ClusterQuotaRows) == 0 {
		return out, fmt.Errorf("no cluster-quota CSV found (cluster_quota plugin reads ocp_ros_cluster_quota / ros-openshift-cluster-quota)")
	}
	if wantPVC && len(csvLoaded.PVCRows) == 0 {
		return out, fmt.Errorf("no storage CSV found (pvc plugin reads ocp_storage_usage / ros-openshift-storage)")
	}
	if wantVM && len(csvLoaded.VMRows) == 0 {
		return out, fmt.Errorf("no VM usage CSV found (vm plugin reads ocp_ros_vm_usage / ros-openshift-vm-usage)")
	}
	clusterID, err := resolveClusterIDFromLoad(out.cfg, csvLoaded)
	if err != nil {
		return out, err
	}
	var maxEnd time.Time
	needContainerDigests := wantC || ((wantQuota || wantCRQ) && len(csvLoaded.Rows) > 0)
	if needContainerDigests {
		digests, ds, err := csv.DailyDigests(csvLoaded.Rows)
		if err != nil {
			return out, err
		}
		out.containerDigests = digests
		if wantC {
			out.containerMeta = ds.Meta
		} else {
			out.containerMeta = map[types.ContainerKey]csv.RowMeta{}
		}
		maxEnd = ds.MaxEnd
	} else {
		out.containerMeta = map[types.ContainerKey]csv.RowMeta{}
	}
	if wantNS || wantQuota || (wantCRQ && len(csvLoaded.NamespaceRows) > 0) {
		grouped, ds, err := csv.DailyNamespaceDigests(csvLoaded.NamespaceRows)
		if err != nil {
			return out, err
		}
		if wantNS {
			out.namespaceGrouped = toNamespaceKeys(grouped)
		}
		if ds.MaxEnd.After(maxEnd) {
			maxEnd = ds.MaxEnd
		}
	}
	if wantQuota || (wantCRQ && len(csvLoaded.NamespaceRows) > 0) {
		out.quotaSnapshots = csv.LatestNamespaceQuotaSnapshots(csvLoaded.NamespaceRows)
	}
	if wantCRQ {
		out.clusterQuotaSnapshots = csv.LatestClusterQuotaSnapshots(csvLoaded.ClusterQuotaRows)
		for _, r := range csvLoaded.ClusterQuotaRows {
			end := r.IntervalEnd
			if end.IsZero() {
				end = r.IntervalStart
			}
			if maxEnd.IsZero() || end.After(maxEnd) {
				maxEnd = end
			}
		}
	}
	if wantNode || wantGPU {
		if maxEnd.IsZero() {
			for _, r := range csvLoaded.Rows {
				end := r.IntervalEnd
				if end.IsZero() {
					end = r.IntervalStart
				}
				if maxEnd.IsZero() || end.After(maxEnd) {
					maxEnd = end
				}
			}
		}
	}
	if wantNode {
		th := node.DefaultThresholdSettings()
		out.nodeDigests = csv.DailyNodeDigests(csvLoaded.Rows, th.AllocatableFactor)
	}
	if wantGPU {
		ds := csv.DailyGPUDigests(csvLoaded.Rows)
		out.gpuGrouped = ds.Grouped
		out.gpuNodeMap = ds.NodeMap
		out.gpuNodeLastSeen = ds.NodeLastSeen
	}
	if wantPVC {
		grouped, ds := csv.DailyPVCDigests(csvLoaded.PVCRows)
		out.pvcGrouped = grouped
		if ds.MaxEnd.After(maxEnd) {
			maxEnd = ds.MaxEnd
		}
	}
	if wantVM {
		digests, ds := csv.DailyVMDigests(csvLoaded.VMRows, csvLoaded.VMPVCRows, csvLoaded.VMGPURows)
		out.vmDigests = digests
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
	cfg                   fileConfig
	plugins               []string
	clusterID             string
	orgID                 string
	now                   time.Time
	skipped               int
	containerDigests      []types.KeyedDigest
	containerMeta         map[types.ContainerKey]csv.RowMeta
	namespaceGrouped      map[namespace.NamespaceKey][]types.DigestRow
	nodeDigests           []node.DigestRow
	gpuGrouped            map[gpu.GPUContainerKey][]gpu.GPUDigestRow
	gpuNodeMap            map[gpu.GPUContainerKey]string
	gpuNodeLastSeen       map[string]time.Time
	pvcGrouped            map[pvc.PVCKey][]pvc.PVCDigestRow
	vmDigests             []vm.DailyVMDigest
	quotaSnapshots        []quota.NamespaceQuotaSnapshot
	clusterQuotaSnapshots []quota.ClusterQuotaSnapshot
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
	if err := attachFileOnlyRecs(&out, fl); err != nil {
		return out, err
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

func attachFileOnlyRecs(out *recommendResult, fl fileLoad) error {
	if pluginEnabled(fl.plugins, "namespace") {
		nsRecs, err := recommendNamespaces(fl)
		if err != nil {
			return err
		}
		out.NamespaceRecs = nsRecs
	}
	if pluginEnabled(fl.plugins, "node") {
		out.NodeRecs = recommendNodes(fl)
	}
	if pluginEnabled(fl.plugins, "gpu") {
		gpuRecs, ts := recommendGPUs(fl)
		out.GPURecs = gpuRecs
		out.GPUTimeslicing = ts
	}
	if pluginEnabled(fl.plugins, "pvc") {
		recs, err := recommendPVCs(fl)
		if err != nil {
			return err
		}
		out.PVCRecs = recs
	}
	if pluginEnabled(fl.plugins, "vm") {
		recs, err := recommendVMs(fl)
		if err != nil {
			return err
		}
		out.VMRecs = recs
	}
	if pluginEnabled(fl.plugins, "quota") {
		recs, err := recommendQuotas(fl, out.Recs)
		if err != nil {
			return err
		}
		out.QuotaRecs = recs
	}
	if pluginEnabled(fl.plugins, "cluster_quota") {
		quotaRecs := out.QuotaRecs
		if !pluginEnabled(fl.plugins, "quota") {
			recs, err := recommendQuotas(fl, out.Recs)
			if err != nil {
				return err
			}
			quotaRecs = recs
		}
		out.ClusterQuotaRecs = recommendClusterQuotas(fl, quotaRecs)
	}
	return nil
}

func recommendNodes(fl fileLoad) []node.Rec {
	th := node.DefaultThresholdSettings()
	ec := engineConfigFromFile(fl.cfg, fl.orgID, fl.clusterID, fl.now)
	recs := node.RecommendNodes(fl.nodeDigests, node.RecConfigFromThresholds(th), th, ec.Terms)
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Node != recs[j].Node {
			return recs[i].Node < recs[j].Node
		}
		if recs[i].Term != recs[j].Term {
			return recs[i].Term < recs[j].Term
		}
		return recs[i].Engine < recs[j].Engine
	})
	return recs
}

func recommendGPUs(fl fileLoad) ([]gpuRecRow, []gpu.TimeslicingRec) {
	settings := gpu.DefaultGPUThresholdSettings()
	ec := engineConfigFromFile(fl.cfg, fl.orgID, fl.clusterID, fl.now)
	byKey := make(map[gpu.GPUContainerKey][]*gpu.GPURec, len(fl.gpuGrouped))
	var rows []gpuRecRow
	for key, days := range fl.gpuGrouped {
		if len(days) == 0 {
			continue
		}
		latest := days[len(days)-1].IntervalStart
		for _, d := range days {
			if d.IntervalStart.After(latest) {
				latest = d.IntervalStart
			}
		}
		for _, tc := range ec.Terms {
			window := gpu.FilterGPUByWindow(days, latest, tc.WindowDays)
			if len(window) < tc.MinDataDays {
				continue
			}
			rec := gpu.RecommendGPUWithSettings(window, settings)
			if rec == nil {
				continue
			}
			rec.Term = tc.Name
			byKey[key] = append(byKey[key], rec)
			rows = append(rows, gpuRecRow{
				Namespace:     key.Namespace,
				Workload:      key.Workload,
				ContainerName: key.ContainerName,
				Rec:           *rec,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		if rows[i].Workload != rows[j].Workload {
			return rows[i].Workload < rows[j].Workload
		}
		if rows[i].ContainerName != rows[j].ContainerName {
			return rows[i].ContainerName < rows[j].ContainerName
		}
		return rows[i].Rec.Term < rows[j].Rec.Term
	})
	groups := gpu.GroupGPURecsByNodeAndModel(byKey, fl.gpuNodeMap, fl.gpuNodeLastSeen, fl.clusterID)
	var ts []gpu.TimeslicingRec
	for _, g := range groups {
		rec := gpu.ComputeNodeTimeslicingRecWithSettings(g, nil, fl.now, settings)
		if rec != nil {
			ts = append(ts, *rec)
		}
	}
	sort.Slice(ts, func(i, j int) bool {
		if ts[i].NodeName != ts[j].NodeName {
			return ts[i].NodeName < ts[j].NodeName
		}
		if ts[i].GPUModel != ts[j].GPUModel {
			return ts[i].GPUModel < ts[j].GPUModel
		}
		return ts[i].Term < ts[j].Term
	})
	return rows, ts
}

func recommendPVCs(fl fileLoad) ([]pvc.PVCRec, error) {
	ec := engineConfigFromFile(fl.cfg, fl.orgID, fl.clusterID, fl.now)
	recs, err := pvc.RecommendPVCs(context.Background(), fl.pvcGrouped, pvc.EngineConfig{
		OrgID:           fl.orgID,
		ClusterUUID:     fl.clusterID,
		Terms:           ec.Terms,
		Settings:        pvc.DefaultThresholdSettings(),
		NotifThresholds: types.NotificationThresholdsFromSizing(ec.Sizing),
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Namespace != recs[j].Namespace {
			return recs[i].Namespace < recs[j].Namespace
		}
		if recs[i].PVC != recs[j].PVC {
			return recs[i].PVC < recs[j].PVC
		}
		return recs[i].Term < recs[j].Term
	})
	return recs, nil
}

func recommendVMs(fl fileLoad) ([]vm.VMRecommendation, error) {
	if len(fl.vmDigests) == 0 {
		return nil, nil
	}
	cfg := vm.DefaultVMRecConfig()
	ec := engineConfigFromFile(fl.cfg, fl.orgID, fl.clusterID, fl.now)
	terms := vm.VMTermWindowsFromConfig(ec.Terms)
	clusterUUID := parseClusterUUID(fl.clusterID)
	all := make([]vm.Digest, len(fl.vmDigests))
	for i, d := range fl.vmDigests {
		d.OrgID = fl.orgID
		d.ClusterUUID = clusterUUID
		all[i] = d
	}
	clusterCtx := vm.NewClusterContext(vm.BuildClusterLatestDigests(all))
	grouped := make(map[vmIdentity][]vm.Digest)
	for _, d := range all {
		k := vmIdentity{Namespace: d.Namespace, VMName: d.VMName}
		grouped[k] = append(grouped[k], d)
	}
	engines := []string{"cost", "performance"}
	var recs []vm.VMRecommendation
	for _, days := range grouped {
		for _, term := range terms {
			for _, eng := range engines {
				rec, err := vm.RecommendVM(days, cfg, term, eng, nil, nil, clusterCtx, nil)
				if err != nil {
					return nil, err
				}
				if rec == nil {
					continue
				}
				recs = append(recs, *rec)
			}
		}
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Namespace != recs[j].Namespace {
			return recs[i].Namespace < recs[j].Namespace
		}
		if recs[i].VMName != recs[j].VMName {
			return recs[i].VMName < recs[j].VMName
		}
		if recs[i].Term != recs[j].Term {
			return recs[i].Term < recs[j].Term
		}
		return recs[i].Engine < recs[j].Engine
	})
	return recs, nil
}

func recommendQuotas(fl fileLoad, containerRecs []types.ContainerRec) ([]quota.QuotaRec, error) {
	recs := containerRecs
	if len(recs) == 0 && len(fl.containerDigests) > 0 {
		computed, err := recommendContainerRecsNoSavings(fl)
		if err != nil {
			return nil, err
		}
		recs = computed
	}
	out := quota.RecommendQuotas(
		fl.quotaSnapshots,
		containerQuotaAggregates(recs),
		fl.orgID,
		fl.clusterID,
		quota.DefaultQuotaRecConfig(),
	)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].QuotaName < out[j].QuotaName
	})
	return out, nil
}

func recommendContainerRecsNoSavings(fl fileLoad) ([]types.ContainerRec, error) {
	ec := engineConfigFromFile(fl.cfg, fl.orgID, fl.clusterID, fl.now)
	if len(fl.containerDigests) > 0 {
		ec.ClusterLastReported = fl.containerDigests[len(fl.containerDigests)-1].Row.BucketDate
		for _, d := range fl.containerDigests {
			if d.Row.BucketDate.After(ec.ClusterLastReported) {
				ec.ClusterLastReported = d.Row.BucketDate
			}
		}
	}
	var recs []types.ContainerRec
	err := engine.RecommendWorkloads(context.Background(), fl.containerDigests, ec, func(batch []types.ContainerRec) error {
		recs = append(recs, batch...)
		return nil
	})
	return recs, err
}

func recommendClusterQuotas(fl fileLoad, quotaRecs []quota.QuotaRec) []quota.ClusterQuotaRec {
	nsAggs := make(map[string]quota.NamespaceQuotaClusterAggregate, len(fl.clusterQuotaSnapshots))
	for _, snap := range fl.clusterQuotaSnapshots {
		if !snap.HasHardLimits() {
			continue
		}
		nsAggs[snap.ClusterQuotaName] = namespaceQuotaClusterAggregates(quotaRecs, parseClusterQuotaNamespaces(snap.Namespaces))
	}
	out := quota.RecommendClusterQuotas(
		fl.clusterQuotaSnapshots,
		nsAggs,
		fl.orgID,
		fl.clusterID,
		quota.DefaultQuotaRecConfig(),
	)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ClusterQuotaName < out[j].ClusterQuotaName
	})
	return out
}

func parseClusterQuotaNamespaces(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func namespaceQuotaClusterAggregates(recs []quota.QuotaRec, namespaces []string) quota.NamespaceQuotaClusterAggregate {
	var agg quota.NamespaceQuotaClusterAggregate
	filter := map[string]struct{}{}
	for _, ns := range namespaces {
		filter[ns] = struct{}{}
	}
	for _, r := range recs {
		if len(filter) > 0 {
			if _, ok := filter[r.Namespace]; !ok {
				continue
			}
		}
		agg.CPURequestRecommendedMC += r.Recommended.CPURequestMillicores
		agg.CPULimitRecommendedMC += r.Recommended.CPULimitMillicores
		agg.MemoryRequestRecommendedBytes += r.Recommended.MemoryRequestBytes
		agg.MemoryLimitRecommendedBytes += r.Recommended.MemoryLimitBytes
	}
	return agg
}

func containerQuotaAggregates(recs []types.ContainerRec) map[string]quota.ContainerQuotaAggregate {
	aggs := make(map[string]quota.ContainerQuotaAggregate)
	for _, r := range recs {
		if r.Term != quota.QuotaContainerTerm || r.Engine != quota.QuotaContainerEngine {
			continue
		}
		a := aggs[r.Namespace]
		a.CPURequestSumMC += r.RecCPURequestMC
		a.CPULimitSumMC += r.RecCPULimitMC
		a.MemoryRequestSumBytes += r.RecMemRequestKiB * 1024
		a.MemoryLimitSumBytes += r.RecMemLimitKiB * 1024
		aggs[r.Namespace] = a
	}
	return aggs
}

type vmIdentity struct {
	Namespace string
	VMName    string
}

func parseClusterUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}

var fileOnlyPlugins = []string{"namespace", "node", "gpu", "pvc", "vm", "quota", "cluster_quota"}

func fileOnlyPluginNames(plugins []string) []string {
	var out []string
	for _, p := range fileOnlyPlugins {
		if pluginEnabled(plugins, p) {
			out = append(out, p)
		}
	}
	return out
}

func rejectFileOnlyPostgresInput(plugins []string) error {
	names := fileOnlyPluginNames(plugins)
	if len(names) == 0 {
		return nil
	}
	if !pluginEnabled(plugins, "container") {
		return fmt.Errorf("%s recompute from postgres:// is not supported yet (see #473); use CSV files", strings.Join(names, ", "))
	}
	return nil
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
	wantNode := pluginEnabled(plugins, "node")
	wantGPU := pluginEnabled(plugins, "gpu")
	needContainerRows := wantC || wantNode || wantGPU
	if needContainerRows && len(loaded.Rows) == 0 {
		if wantNode || wantGPU {
			return fmt.Errorf("no ROS container CSV found (node and gpu plugins read container ROS rows)")
		}
		return fmt.Errorf("no ROS container CSV found (namespace files require --plugins namespace)")
	}
	wantPVC := pluginEnabled(plugins, "pvc")
	if wantPVC && len(loaded.PVCRows) == 0 {
		return fmt.Errorf("no storage CSV found (pvc plugin reads ocp_storage_usage / ros-openshift-storage)")
	}
	wantVM := pluginEnabled(plugins, "vm")
	if wantVM && len(loaded.VMRows) == 0 {
		return fmt.Errorf("no VM usage CSV found (vm plugin reads ocp_ros_vm_usage / ros-openshift-vm-usage)")
	}
	wantQuota := pluginEnabled(plugins, "quota")
	if (wantNS || wantQuota) && len(loaded.NamespaceRows) == 0 {
		return fmt.Errorf("no ROS namespace CSV found")
	}
	wantCRQ := pluginEnabled(plugins, "cluster_quota")
	if wantCRQ && len(loaded.ClusterQuotaRows) == 0 {
		return fmt.Errorf("no cluster-quota CSV found (cluster_quota plugin reads ocp_ros_cluster_quota / ros-openshift-cluster-quota)")
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
	if wantNS || wantQuota {
		if _, err := fmt.Fprintf(os.Stdout, "namespace_rows: %d\n", len(loaded.NamespaceRows)); err != nil {
			return err
		}
	}
	if wantPVC {
		if _, err := fmt.Fprintf(os.Stdout, "pvc_rows: %d\n", len(loaded.PVCRows)); err != nil {
			return err
		}
	}
	if wantVM {
		if _, err := fmt.Fprintf(os.Stdout, "vm_rows: %d\n", len(loaded.VMRows)); err != nil {
			return err
		}
	}
	if wantCRQ {
		if _, err := fmt.Fprintf(os.Stdout, "cluster_quota_rows: %d\n", len(loaded.ClusterQuotaRows)); err != nil {
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
	} else if wantNS || wantQuota {
		_, ds, err := csv.DailyNamespaceDigests(loaded.NamespaceRows)
		if err != nil {
			return err
		}
		if !ds.MaxEnd.IsZero() {
			if _, err := fmt.Fprintf(os.Stdout, "max_interval_end: %s\n", ds.MaxEnd.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
				return err
			}
		}
	} else if wantNode || wantGPU {
		var maxEnd time.Time
		for _, r := range loaded.Rows {
			end := r.IntervalEnd
			if end.IsZero() {
				end = r.IntervalStart
			}
			if maxEnd.IsZero() || end.After(maxEnd) {
				maxEnd = end
			}
		}
		if !maxEnd.IsZero() {
			if _, err := fmt.Fprintf(os.Stdout, "max_interval_end: %s\n", maxEnd.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
				return err
			}
		}
	} else if wantPVC {
		_, ds := csv.DailyPVCDigests(loaded.PVCRows)
		if !ds.MaxEnd.IsZero() {
			if _, err := fmt.Fprintf(os.Stdout, "max_interval_end: %s\n", ds.MaxEnd.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
				return err
			}
		}
	} else if wantVM {
		_, ds := csv.DailyVMDigests(loaded.VMRows, loaded.VMPVCRows, loaded.VMGPURows)
		if !ds.MaxEnd.IsZero() {
			if _, err := fmt.Fprintf(os.Stdout, "max_interval_end: %s\n", ds.MaxEnd.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
				return err
			}
		}
	} else if wantCRQ {
		var maxEnd time.Time
		for _, r := range loaded.ClusterQuotaRows {
			end := r.IntervalEnd
			if end.IsZero() {
				end = r.IntervalStart
			}
			if maxEnd.IsZero() || end.After(maxEnd) {
				maxEnd = end
			}
		}
		if !maxEnd.IsZero() {
			if _, err := fmt.Fprintf(os.Stdout, "max_interval_end: %s\n", maxEnd.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
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
