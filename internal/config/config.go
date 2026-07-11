package config

import (
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/joho/godotenv"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"

	clowder "github.com/redhatinsights/app-common-go/pkg/api/v1"
)

// defaultDBMaxConns is the pgxpool max per process when ROS_DB_MAX_CONNS is unset.
// Must satisfy: ManifestDownloadWorkers × KafkaWorkers <= DBMaxConns - 2.
// Coordinate replica count × this value against PostgreSQL max_connections.
// See ADR-0320 and ADR-0321.
const defaultDBMaxConns = 10

type Config struct {
	// Application config
	ServiceName                     string `mapstructure:"SERVICE_NAME"`
	LogFormatter                    string `mapstructure:"LogFormater"`
	LogLevel                        string `mapstructure:"LOG_LEVEL"`
	RecommendationPollIntervalHours int    `mapstructure:"RECOMMENDATION_POLL_INTERVAL_HOURS"`
	DataRetentionPeriod             int    `mapstructure:"DATA_RETENTION_PERIOD"`
	ReadHeaderTimeout               int    `mapstructure:"READ_HEADER_TIMEOUT"`
	ReadTimeout                     int    `mapstructure:"ROS_API_READ_TIMEOUT"`
	WriteTimeout                    int    `mapstructure:"ROS_API_WRITE_TIMEOUT"`
	IdleTimeout                     int    `mapstructure:"ROS_API_IDLE_TIMEOUT"`
	RecordLimitCSV                  int    `mapstructure:"RECORD_LIMIT_CSV"`
	CSVStreamInterval               int    `mapstructure:"CSV_STREAM_INTERVAL"`
	MaxCountPerQueryParam           int    `mapstructure:"MAXIMUM_COUNT_PER_QUERY_PARAM"`
	UpdateKruizePerfProfile         bool   `mapstructure:"UPDATE_KRUIZE_PERF_PROFILE"`
	APIMaxOffset                    int    `mapstructure:"ROS_API_MAX_OFFSET"`
	Development                     bool   `mapstructure:"DEVELOPMENT"`
	CORSAllowedOrigins              string `mapstructure:"ROS_CORS_ALLOWED_ORIGINS"`

	// Kafka config
	KafkaBootstrapServers string `mapstructure:"KAFKA_BOOTSTRAP_SERVERS"`
	KafkaConsumerGroupId  string `mapstructure:"KAFKA_CONSUMER_GROUP_ID"`
	KafkaAutoCommit          bool   `mapstructure:"KAFKA_AUTO_COMMIT"`
	KafkaMaxTransientRetries int    `mapstructure:"ROS_KAFKA_MAX_TRANSIENT_RETRIES"`
	KafkaDLQTopic            string `mapstructure:"ROS_KAFKA_DLQ_TOPIC"`
	UploadTopic              string `mapstructure:"UPLOAD_TOPIC"`
	RecommendationTopic      string `mapstructure:"RECOMMENDATION_TOPIC"`
	SourcesEventTopic        string `mapstructure:"SOURCES_EVENT_TOPIC"`
	KafkaUsername         string
	KafkaPassword         string
	KafkaSASLMechanism    string
	KafkaSecurityProtocol string
	KafkaCA               string

	// HTTP client config
	GlobalHTTPClientTimeoutSecs int `mapstructure:"GLOBAL_HTTP_CLIENT_TIMEOUT_SECS"`

	// Kruize config
	KruizeUrl                       string `mapstructure:"KRUIZE_URL"`
	KruizeWaitTime                  string `mapstructure:"KRUIZE_WAIT_TIME"`
	KruizeMaxBulkChunkSize          int    `mapstructure:"KRUIZE_MAX_BULK_CHUNK_SIZE"`
	KruizePerformanceProfileVersion string `mapstructure:"KRUIZE_PERFORMANCE_PROFILE_VERSION"`

	// Database config
	DBName     string
	DBUser     string
	DBPassword string
	DBHost     string
	DBPort     string
	DBssl      string
	DBCACert   string

	// pgxpool tuning (shared by pgxpool and GORM via stdlib.OpenDBFromPool).
	// ROS_DB_MAX_CONNS defaults to defaultDBMaxConns; ROS_DB_ACQUIRE_TIMEOUT_SECS sets
	// ContextWithAcquireTimeout (0 = no limit). Legacy alias: DB_POOL_SIZE.
	DBMaxConns            int    `mapstructure:"ROS_DB_MAX_CONNS"`
	DBMinConns            int    `mapstructure:"ROS_DB_MIN_CONNS"`
	DBMaxConnLifetimeMins int    `mapstructure:"ROS_DB_MAX_CONN_LIFETIME"`
	DBMaxConnIdleTimeMins int    `mapstructure:"ROS_DB_MAX_CONN_IDLE_TIME"`
	DBStatementCacheMode         string `mapstructure:"ROS_DB_STATEMENT_CACHE_MODE"`
	DBAcquireTimeoutSecs         int    `mapstructure:"ROS_DB_ACQUIRE_TIMEOUT_SECS"`
	DBStatementTimeoutSecs       int `mapstructure:"ROS_DB_STATEMENT_TIMEOUT"`
	DBAPIStatementTimeoutMS      int `mapstructure:"ROS_API_STATEMENT_TIMEOUT_MS"`
	DBHeavyAPIStatementTimeoutMS int `mapstructure:"ROS_HEAVY_API_STATEMENT_TIMEOUT_MS"`
	DBIngestStatementTimeoutSecs int `mapstructure:"ROS_DB_INGEST_STATEMENT_TIMEOUT"`
	IngestFlushBatchSize         int    `mapstructure:"ROS_INGEST_FLUSH_BATCH_SIZE"`
	// ADR-0096: Strict analytics mode default true; set false for degraded ingest.
	IngestStrictAnalytics bool `mapstructure:"ROS_INGEST_STRICT_ANALYTICS"`

	// RBAC config
	RBACHost     string
	RBACPort     string
	RBACProtocol string
	RBACEnabled  bool `mapstructure:"RBAC_ENABLE"`
	// RBACCacheTTLSecs caches RBAC permissions in-memory (0 disables). Default 60.
	RBACCacheTTLSecs int `mapstructure:"ROS_RBAC_CACHE_TTL"`
	// RBACCacheMaxEntries caps the in-memory RBAC permission LRU cache (default 500).
	RBACCacheMaxEntries int `mapstructure:"ROS_RBAC_CACHE_MAX_ENTRIES"`

	// KafkaLagPollIntervalSecs is the interval (seconds) between Kafka consumer lag metric updates (default 30).
	KafkaLagPollIntervalSecs int `mapstructure:"KAFKA_LAG_POLL_INTERVAL_SECONDS"`

	// KafkaSessionTimeoutMS is the Kafka consumer session.timeout.ms (default 120000).
	// Must exceed the longest single-message processing time to avoid unnecessary rebalances.
	KafkaSessionTimeoutMS int `mapstructure:"ROS_KAFKA_SESSION_TIMEOUT_MS"`
	// KafkaHeartbeatIntervalMS is the Kafka consumer heartbeat.interval.ms (default 30000).
	// Should be no more than 1/3 of KafkaSessionTimeoutMS per Kafka documentation.
	KafkaHeartbeatIntervalMS int `mapstructure:"ROS_KAFKA_HEARTBEAT_INTERVAL_MS"`

	// KafkaWorkers is the worker pool size when ROS_KAFKA_PARALLEL is enabled (default 3).
	KafkaWorkers int `mapstructure:"ROS_KAFKA_WORKERS"`
	// KafkaParallel enables parallel Kafka message processing (default true).
	KafkaParallel bool `mapstructure:"ROS_KAFKA_PARALLEL"`

	// ManifestDownloadWorkers bounds concurrent CSV file downloads within a single manifest (default 2).
	// Coordinate with ROS_KAFKA_WORKERS and ROS_DB_MAX_CONNS: each in-flight download holds a DB
	// connection during ingest, so ManifestDownloadWorkers × KafkaWorkers should not exceed
	// DBMaxConns - 2 (reserves 2 connections for recommendations and health checks).
	ManifestDownloadWorkers int `mapstructure:"ROS_MANIFEST_DOWNLOAD_WORKERS"`

	// ThresholdRecalcConcurrency limits parallel cluster recalculations (default 3).
	ThresholdRecalcConcurrency int `mapstructure:"ROS_THRESHOLD_RECALC_CONCURRENCY"`

	API_PORT string

	// Cloudwatch config
	CwLogGroup  string
	CwRegion    string
	CwAccessKey string
	CwSecretKey string
	CwLogStream string `mapstructure:"CW_LOG_STREAM_NAME"`

	// Prometheus config
	PrometheusPort string `mapstructure:"PROMETHEUS_PORT"`
	// EnablePprof exposes Go pprof endpoints on the Prometheus/health port (default false).
	// Only enable for profiling sessions — never in production.
	EnablePprof bool `mapstructure:"ROS_ENABLE_PPROF"`

	// Sources-api-go config
	SourceApiBaseUrl string `mapstructure:"SOURCES_API_BASE_URL"`
	SourceApiPrefix  string `mapstructure:"SOURCES_API_PREFIX"`

	OOMBaseBump float64 `mapstructure:"ROS_OOM_BASE_BUMP"`
	OOMMaxBump      float64 `mapstructure:"ROS_OOM_MAX_BUMP"`
	RetentionMonths int `mapstructure:"ROS_RETENTION_MONTHS"`
	MaxLookbackDays int `mapstructure:"ROS_MAX_LOOKBACK_DAYS"`

	// History/quality data retention (days). Defaults to 90.
	// Separate from RetentionMonths because history tables grow faster
	// (one row per container per term per engine per run).
	HistoryRetentionDays int `mapstructure:"ROS_HISTORY_RETENTION_DAYS"`

	// Staleness threshold in hours. Recommendations with no new data beyond
	// this threshold are marked stale. Defaults to 48 (2 days).
	StalenessThresholdHours int `mapstructure:"ROS_STALENESS_THRESHOLD_HOURS"`

	// Stale cleanup days. Stale recommendations older than this are deleted
	// during the retention sweep. Defaults to 30.
	StaleCleanupDays int `mapstructure:"ROS_STALE_CLEANUP_DAYS"`

	// Koku masu API URL for fetching cost data (savings estimates)
	KokuMasuURL string `mapstructure:"KOKU_MASU_URL"`

	// OnPrem indicates the on-premise deployment mode (no Trino, OCP-only).
	// Affects cache size defaults: smaller caches for single-tenant on-prem vs multi-tenant SaaS.
	OnPrem bool `mapstructure:"ONPREM"`

	// TermConfigCacheMaxEntries caps the in-memory term config LRU cache.
	// Default: 5 (on-prem) or 1000 (SaaS). Override with ROS_TERM_CONFIG_CACHE_MAX_ENTRIES.
	TermConfigCacheMaxEntries int `mapstructure:"ROS_TERM_CONFIG_CACHE_MAX_ENTRIES"`

	// CostCacheMaxEntries caps the in-memory effective-rates LRU cache (default 1000).
	CostCacheMaxEntries int `mapstructure:"ROS_COST_CACHE_MAX_ENTRIES"`

	// SavingsEstimatesEnabled gates Masu effective-rates fetches for container and GPU savings.
	// Default true; set ROS_SAVINGS_ESTIMATES_ENABLED=false to disable all savings calculations.
	SavingsEstimatesEnabled bool `mapstructure:"ROS_SAVINGS_ESTIMATES_ENABLED"`

	// BusinessHoursEnabled gates business-hours settings routes, OpenAPI paths, capabilities,
	// and dual-stream ingestion. Default true; set ROS_BUSINESS_HOURS_ENABLED=false to disable.
	BusinessHoursEnabled bool `mapstructure:"ROS_BUSINESS_HOURS_ENABLED"`

	// VisualInsightsEnabled gates the OOM timeline endpoint and cpuThrottle field in boxplot
	// responses. Default true; set ROS_VISUAL_INSIGHTS_ENABLED=false to disable both.
	VisualInsightsEnabled bool `mapstructure:"ROS_VISUAL_INSIGHTS_ENABLED"`

	// HourlyVMDigestsEnabled gates hourly VM activity heatmap ingestion and API.
	// Default true; set ROS_HOURLY_VM_DIGESTS_ENABLED=false to skip hourly aggregation.
	HourlyVMDigestsEnabled bool `mapstructure:"ROS_HOURLY_VM_DIGESTS_ENABLED"`

	// HourlyVMDigestsRetentionDays controls how many days of hourly VM data to retain (default 90).
	HourlyVMDigestsRetentionDays int `mapstructure:"ROS_HOURLY_VM_DIGESTS_RETENTION_DAYS"`

	// HourlyNodeDigestsEnabled gates hourly node utilization heatmap ingestion and API.
	// Default true; set ROS_HOURLY_NODE_DIGESTS_ENABLED=false to skip hourly aggregation.
	HourlyNodeDigestsEnabled bool `mapstructure:"ROS_HOURLY_NODE_DIGESTS_ENABLED"`

	// HourlyNodeDigestsRetentionDays controls how many days of hourly node data to retain (default 90).
	HourlyNodeDigestsRetentionDays int `mapstructure:"ROS_HOURLY_NODE_DIGESTS_RETENTION_DAYS"`

	// ThresholdRecalculationEnabled triggers async recommendation recalculation when tenant
	// threshold settings change via the Settings API PUT. Default true.
	ThresholdRecalculationEnabled bool `mapstructure:"ROS_THRESHOLD_RECALCULATION_ENABLED"`

	// SavingsRecalculationEnabled allows POST /internal/recalculate-savings after Koku cost model
	// rate changes. Default true; requires SavingsEstimatesEnabled and KOKU_MASU_URL.
	SavingsRecalculationEnabled bool `mapstructure:"ROS_SAVINGS_RECALCULATION_ENABLED"`

	// ReshipPollerIntervalSecs is the background retry interval for pending masu reshships (default 60).
	ReshipPollerIntervalSecs int `mapstructure:"ROS_RESHIP_POLLER_INTERVAL_SECS"`

	// ReshipMaxRetries is the consecutive poller retry budget before ros_reship_failures_total increments (default 10).
	ReshipMaxRetries int `mapstructure:"ROS_RESHIP_MAX_RETRIES"`

	// ReshipConcurrency caps parallel masu reship_ros calls per org fan-out (default 2).
	// Coordinate with masu rate limits when raising this value.
	ReshipConcurrency int `mapstructure:"ROS_RESHIP_CONCURRENCY"`

	// ReshipForwardOnlyFallback enables forward-only BH recommendations after max reship retries (default false).
	ReshipForwardOnlyFallback bool `mapstructure:"ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK"`

	// Container sizing and classification thresholds (tenant-configurable via Settings API).
	// ReplicaTargetUtilizationPct is the target CPU/memory utilization percentage for
	// replica count optimization. Recommendations scale replicas to keep per-replica
	// resource utilization near this target. Valid range: 10-95, default 70.
	ReplicaTargetUtilizationPct int `mapstructure:"ROS_REPLICA_TARGET_UTILIZATION_PCT"`

	ContainerCPUCostPercentile      float64 `mapstructure:"ROS_CONTAINER_CPU_COST_PERCENTILE"`
	ContainerCPUPerfPercentile      float64 `mapstructure:"ROS_CONTAINER_CPU_PERF_PERCENTILE"`
	ContainerMemCostPercentile      float64 `mapstructure:"ROS_CONTAINER_MEM_COST_PERCENTILE"`
	ContainerMemPerfPercentile      float64 `mapstructure:"ROS_CONTAINER_MEM_PERF_PERCENTILE"`
	ContainerMinMargin              float64 `mapstructure:"ROS_CONTAINER_MIN_MARGIN"`
	ContainerMaxMargin              float64 `mapstructure:"ROS_CONTAINER_MAX_MARGIN"`
	ContainerLimitMultiplier        float64 `mapstructure:"ROS_CONTAINER_LIMIT_MULTIPLIER"`
	ContainerCPUFloorMC             int64   `mapstructure:"ROS_CONTAINER_CPU_FLOOR_MC"`
	ContainerMemFloorKiB            int64   `mapstructure:"ROS_CONTAINER_MEM_FLOOR_KIB"`
	ContainerIdleCPUThresholdMC     int64   `mapstructure:"ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC"`
	ContainerIdleMemThresholdKiB    int64   `mapstructure:"ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB"`
	ContainerMemTrendSlopeThreshold float64 `mapstructure:"ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD"`
	ContainerLowConfidenceThreshold float32 `mapstructure:"ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD"`
	ContainerSparseDataThreshold  int     `mapstructure:"ROS_CONTAINER_SPARSE_DATA_THRESHOLD"`

	// Namespace sizing and classification thresholds (same shape as container).
	NamespaceCPUCostPercentile      float64 `mapstructure:"ROS_NAMESPACE_CPU_COST_PERCENTILE"`
	NamespaceCPUPerfPercentile      float64 `mapstructure:"ROS_NAMESPACE_CPU_PERF_PERCENTILE"`
	NamespaceMemCostPercentile      float64 `mapstructure:"ROS_NAMESPACE_MEM_COST_PERCENTILE"`
	NamespaceMemPerfPercentile      float64 `mapstructure:"ROS_NAMESPACE_MEM_PERF_PERCENTILE"`
	NamespaceMinMargin              float64 `mapstructure:"ROS_NAMESPACE_MIN_MARGIN"`
	NamespaceMaxMargin              float64 `mapstructure:"ROS_NAMESPACE_MAX_MARGIN"`
	NamespaceLimitMultiplier        float64 `mapstructure:"ROS_NAMESPACE_LIMIT_MULTIPLIER"`
	NamespaceCPUFloorMC             int64   `mapstructure:"ROS_NAMESPACE_CPU_FLOOR_MC"`
	NamespaceMemFloorKiB            int64   `mapstructure:"ROS_NAMESPACE_MEM_FLOOR_KIB"`
	NamespaceIdleCPUThresholdMC     int64   `mapstructure:"ROS_NAMESPACE_IDLE_CPU_THRESHOLD_MC"`
	NamespaceIdleMemThresholdKiB    int64   `mapstructure:"ROS_NAMESPACE_IDLE_MEM_THRESHOLD_KIB"`
	NamespaceMemTrendSlopeThreshold float64 `mapstructure:"ROS_NAMESPACE_MEM_TREND_SLOPE_THRESHOLD"`
	NamespaceLowConfidenceThreshold float32 `mapstructure:"ROS_NAMESPACE_LOW_CONFIDENCE_THRESHOLD"`
	NamespaceSparseDataThreshold  int     `mapstructure:"ROS_NAMESPACE_SPARSE_DATA_THRESHOLD"`

	// Node right-sizing (Tier 1) configuration
	NodeUnderutilThreshold                  float64 `mapstructure:"ROS_NODE_UNDERUTIL_THRESHOLD"`
	NodeOvercommitThreshold                 float64 `mapstructure:"ROS_NODE_OVERCOMMIT_THRESHOLD"`
	NodeAllocatableFactor                   float64 `mapstructure:"ROS_NODE_ALLOCATABLE_FACTOR"`
	NodeStrandedImbalanceThreshold          float64 `mapstructure:"ROS_NODE_STRANDED_IMBALANCE_THRESHOLD"`
	NodeEMAAlpha                            float64 `mapstructure:"ROS_NODE_EMA_ALPHA"`
	NodeCostTargetUtilization               float64 `mapstructure:"ROS_NODE_COST_TARGET_UTILIZATION"`
	NodePerfTargetUtilization               float64 `mapstructure:"ROS_NODE_PERF_TARGET_UTILIZATION"`
	NodePerfConsolidationHeadroomMultiplier float64 `mapstructure:"ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER"`
	NodeTrendMinDays                        int     `mapstructure:"ROS_NODE_TREND_MIN_DAYS"`
	NodeZombieCPUP95MC                      int64   `mapstructure:"ROS_NODE_ZOMBIE_CPU_MC"`
	NodeZombieMaxPods                       int64   `mapstructure:"ROS_NODE_ZOMBIE_MAX_PODS"`
	NodeIdleCPUUtilPct                      int64   `mapstructure:"ROS_NODE_IDLE_CPU_UTIL_PCT"`
	NodeIdleMemUtilPct                      int64   `mapstructure:"ROS_NODE_IDLE_MEM_UTIL_PCT"`
	NodeIdleMaxPods                         int64   `mapstructure:"ROS_NODE_IDLE_MAX_PODS"`
	// PodHeadroomConsolidationGate is the minimum pod scheduling headroom (0.0–1.0) before consolidation is suppressed.
	NodePodHeadroomConsolidationGate float64 `mapstructure:"ROS_NODE_POD_HEADROOM_CONSOLIDATION_GATE"`
	// PodHeadroomNotificationThreshold is the headroom (0.0–1.0) below which notification code 74 is emitted.
	NodePodHeadroomNotificationThreshold float64 `mapstructure:"ROS_NODE_POD_HEADROOM_NOTIFICATION_THRESHOLD"`

	// GPU recommendation engine thresholds (Classification / MIG sizing).
	GPUIdleThreshold                float64 `mapstructure:"ROS_GPU_IDLE_THRESHOLD"`
	GPUUnderutilizedSMThreshold     float64 `mapstructure:"ROS_GPU_UNDERUTILIZED_SM_THRESHOLD"`
	GPUUnderutilizedTensorThreshold float64 `mapstructure:"ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD"`
	GPUMemBoundDRAMThreshold        float64 `mapstructure:"ROS_GPU_MEMBOUND_DRAM_THRESHOLD"`
	GPUMemBoundTensorThreshold      float64 `mapstructure:"ROS_GPU_MEMBOUND_TENSOR_THRESHOLD"`
	GPUFBHeadroomFactor             float64 `mapstructure:"ROS_GPU_FB_HEADROOM_FACTOR"`
	GPUComputeBoundDRAMThreshold    float64 `mapstructure:"ROS_GPU_COMPUTE_BOUND_DRAM_THRESHOLD"`
	GPUMIGFBPercentile              float64 `mapstructure:"ROS_GPU_MIG_FB_PERCENTILE"`
	GPUConfidenceDaysTier1          int     `mapstructure:"ROS_GPU_CONFIDENCE_DAYS_TIER1"`
	GPUConfidenceDaysTier2          int     `mapstructure:"ROS_GPU_CONFIDENCE_DAYS_TIER2"`
	GPUConfidenceDaysTier3          int     `mapstructure:"ROS_GPU_CONFIDENCE_DAYS_TIER3"`
	GPUSpikeRatioThreshold          float64 `mapstructure:"ROS_GPU_SPIKE_RATIO_THRESHOLD"`
	GPUSpikeConfidencePenalty       float64 `mapstructure:"ROS_GPU_SPIKE_CONFIDENCE_PENALTY"`
	GPUNoProfilingConfidenceFactor  float64 `mapstructure:"ROS_GPU_NO_PROFILING_CONFIDENCE_FACTOR"`
	GPUTimeslicingMajorityThreshold float64 `mapstructure:"ROS_GPU_TIMESLICING_MAJORITY_THRESHOLD"`
	GPUTimeslicingMinReplicas       int     `mapstructure:"ROS_GPU_TIMESLICING_MIN_REPLICAS"`
	GPUTimeslicingMaxReplicas       int     `mapstructure:"ROS_GPU_TIMESLICING_MAX_REPLICAS"`
	GPUTimeslicingBasePenalty       float64 `mapstructure:"ROS_GPU_TIMESLICING_BASE_PENALTY"`
	GPUTimeslicingImpactedWeight    float64 `mapstructure:"ROS_GPU_TIMESLICING_IMPACTED_WEIGHT"`
	GPUNodeFreshnessDays            int     `mapstructure:"ROS_GPU_NODE_FRESHNESS_DAYS"`

	// PVC right-sizing thresholds.
	PVCOversizedThreshold        float64 `mapstructure:"ROS_PVC_OVERSIZED_THRESHOLD"`
	PVCNearFullThreshold         float64 `mapstructure:"ROS_PVC_NEAR_FULL_THRESHOLD"`
	PVCMinTrendDays              int     `mapstructure:"ROS_PVC_MIN_TREND_DAYS"`
	PVCRecommendedSizeMultiplier int     `mapstructure:"ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER"`
	PVCMinRecommendedGiB         int     `mapstructure:"ROS_PVC_MIN_RECOMMENDED_GIB"`
	PVCDaysToFullAlert           int     `mapstructure:"ROS_PVC_DAYS_TO_FULL_ALERT"`

	// Quota recommendation thresholds (percent values; basis points = percent * 100).
	QuotaHeadroomPercent            int `mapstructure:"ROS_QUOTA_HEADROOM_PERCENT"`
	QuotaHighRiskThresholdPercent   int `mapstructure:"ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT"`
	QuotaMediumRiskThresholdPercent int `mapstructure:"ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT"`

	ClusterQuotaHeadroomPercent            int `mapstructure:"ROS_CLUSTER_QUOTA_HEADROOM_PERCENT"`
	ClusterQuotaHighRiskThresholdPercent   int `mapstructure:"ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT"`
	ClusterQuotaMediumRiskThresholdPercent int `mapstructure:"ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT"`

	// VM (OpenShift Virtualization) recommendation thresholds.
	VMCPUPercentileCost float64 `mapstructure:"ROS_VM_CPU_PERCENTILE_COST"`
	VMCPUPercentilePerf          float64 `mapstructure:"ROS_VM_CPU_PERCENTILE_PERF"`
	VMCPUMarginMin               float64 `mapstructure:"ROS_VM_CPU_MARGIN_MIN"`
	VMCPUMarginMax               float64 `mapstructure:"ROS_VM_CPU_MARGIN_MAX"`
	VMCPUAdaptiveMarginEnabled   bool    `mapstructure:"ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED"`
	VMMemMarginMin               float64 `mapstructure:"ROS_VM_MEM_MARGIN_MIN"`
	VMDownsizeHysteresisRatio    float64 `mapstructure:"ROS_VM_DOWNSIZE_HYSTERESIS_RATIO"`
	VMMinVCPUChange              int32   `mapstructure:"ROS_VM_MIN_VCPU_CHANGE"`
	VMMinGiBChange               int32   `mapstructure:"ROS_VM_MIN_GIB_CHANGE"`
	VMIdleCPUMC                  int64   `mapstructure:"ROS_VM_IDLE_CPU_MC"`
	VMIdleMemoryMiB              int64   `mapstructure:"ROS_VM_IDLE_MEMORY_MIB"`
	VMIdleCPUMCWindows           int64   `mapstructure:"ROS_VM_IDLE_CPU_MC_WINDOWS"`
	VMIdleMemoryMiBWindows       int64   `mapstructure:"ROS_VM_IDLE_MEMORY_MIB_WINDOWS"`
	VMLinuxMemoryFloorGiB        int32   `mapstructure:"ROS_VM_LINUX_MEMORY_FLOOR_GIB"`
	VMWindowsMemoryFloorGiB      int32   `mapstructure:"ROS_VM_WINDOWS_MEMORY_FLOOR_GIB"`
	VMDiskProjectionDays         int32   `mapstructure:"ROS_VM_DISK_PROJECTION_DAYS"`
	VMDiskHeadroomPct            float64 `mapstructure:"ROS_VM_DISK_HEADROOM_PCT"`
	VMDiskRoundStepGiB           int32   `mapstructure:"ROS_VM_DISK_ROUND_STEP_GIB"`
	VMDiskMinGrowthMiBPerDay     int64   `mapstructure:"ROS_VM_DISK_MIN_GROWTH_MIB_PER_DAY"`
	VMHighIOPSThreshold            int64 `mapstructure:"ROS_VM_HIGH_IOPS_THRESHOLD"`
	VMIOSequentialThresholdBytes   int64 `mapstructure:"ROS_VM_IO_SEQUENTIAL_THRESHOLD_BYTES"`
	VMIORandomThresholdBytes       int64 `mapstructure:"ROS_VM_IO_RANDOM_THRESHOLD_BYTES"`
	VMIOMinIOPSClassification     int64 `mapstructure:"ROS_VM_IO_MIN_IOPS_CLASSIFICATION"`
	VMEnableInstanceTypeMatching bool    `mapstructure:"ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING"`
	VMAbandonedMinDays           int32   `mapstructure:"ROS_VM_ABANDONED_MIN_DAYS"`
	VMWindowsKernelReserveGiB    float64 `mapstructure:"ROS_VM_WINDOWS_KERNEL_RESERVE_GIB"`
	VMDownsizeStabilityDays      int     `mapstructure:"ROS_VM_DOWNSIZE_STABILITY_DAYS"`
	VMCrashLoopRestartThreshold  int32   `mapstructure:"ROS_VM_CRASH_LOOP_RESTART_THRESHOLD"`
	VMGPUIdleThreshold              float64 `mapstructure:"ROS_VM_GPU_IDLE_THRESHOLD"`
	VMGPUUnderutilThreshold         float64 `mapstructure:"ROS_VM_GPU_UNDERUTIL_THRESHOLD"`
	VMGPUFBSaturationMiB            float64 `mapstructure:"ROS_VM_GPU_FB_SATURATION_MIB"`
	VMGPUComputeSaturationThreshold float64 `mapstructure:"ROS_VM_GPU_COMPUTE_SATURATION_THRESHOLD"`
	VMGPUTimeSliceMinReplicas       int32   `mapstructure:"ROS_VM_GPU_TIMESLICE_MIN_REPLICAS"`
	VMGPUTimeSliceMaxReplicas       int32   `mapstructure:"ROS_VM_GPU_TIMESLICE_MAX_REPLICAS"`
	VMGPUTimeSliceFBSafetyBP        int32   `mapstructure:"ROS_VM_GPU_TIMESLICE_FB_SAFETY_BP"`
	VMGPUTimeSliceDRAMPenaltyBP     int32   `mapstructure:"ROS_VM_GPU_TIMESLICE_DRAM_PENALTY_BP"`
	VMNetworkThroughputThresholdBPS int64   `mapstructure:"ROS_VM_NETWORK_THROUGHPUT_THRESHOLD_BPS"`
	VMNetworkPPSThreshold           int64   `mapstructure:"ROS_VM_NETWORK_PPS_THRESHOLD"`
	VMNetworkDropRatioBP            int32   `mapstructure:"ROS_VM_NETWORK_DROP_RATIO_BP"`
	VMNetworkSustainedDays          int     `mapstructure:"ROS_VM_NETWORK_SUSTAINED_DAYS"`
	VMEnableNetworkSeries           bool    `mapstructure:"ROS_VM_ENABLE_NETWORK_SERIES"`
	VMRecHistoryRetentionDays       int     `mapstructure:"ROS_VM_REC_HISTORY_RETENTION_DAYS"`
	VMEnablePlacementChecks         bool    `mapstructure:"ROS_VM_ENABLE_PLACEMENT_CHECKS"`
	VMPlacementSkewRatio            int     `mapstructure:"ROS_VM_PLACEMENT_SKEW_RATIO"`
	VMEnableSharedPVCCorrelation    bool    `mapstructure:"ROS_VM_ENABLE_SHARED_PVC_CORRELATION"`
	VMNUMANodeMemoryGiB             float64 `mapstructure:"ROS_VM_NUMA_NODE_MEMORY_GIB"`
	VMNUMAAssumedSockets            int     `mapstructure:"ROS_VM_NUMA_ASSUMED_SOCKETS"`
	VMEnablePowerSchedule           bool    `mapstructure:"ROS_VM_ENABLE_POWER_SCHEDULE"`
	VMPowerOffMinIdleDays           int32   `mapstructure:"ROS_VM_POWER_OFF_MIN_IDLE_DAYS"`
	VMPowerOffIdleRatioThreshold    float64 `mapstructure:"ROS_VM_POWER_OFF_IDLE_RATIO_THRESHOLD"`
	VMNetworkQoSEnabled             bool    `mapstructure:"ROS_VM_NETWORK_QOS_ENABLED"`
	VMNetworkQoSSRIOVDropThreshold  float64 `mapstructure:"ROS_VM_NETWORK_QOS_SRIOV_DROP_THRESHOLD"`
	VMNetworkQoSSRIOVThroughputBPS  int64   `mapstructure:"ROS_VM_NETWORK_QOS_SRIOV_THROUGHPUT_BPS"`
	VMNetworkQoSDPDKPPSThreshold    int64   `mapstructure:"ROS_VM_NETWORK_QOS_DPDK_PPS_THRESHOLD"`
	VMStorageTieringEnabled           bool  `mapstructure:"ROS_VM_STORAGE_TIERING_ENABLED"`
	VMStorageTieringMinDays           int   `mapstructure:"ROS_VM_STORAGE_TIERING_MIN_DAYS"`
	VMStorageTieringColdMinDays       int   `mapstructure:"ROS_VM_STORAGE_TIERING_COLD_MIN_DAYS"`
	VMStorageTieringIOPSMinDays       int   `mapstructure:"ROS_VM_STORAGE_TIERING_IOPS_MIN_DAYS"`
	VMStorageTieringThroughputMinDays int   `mapstructure:"ROS_VM_STORAGE_TIERING_THROUGHPUT_MIN_DAYS"`
	VMStorageTieringHighIOPSThreshold int64 `mapstructure:"ROS_VM_STORAGE_TIERING_HIGH_IOPS_THRESHOLD"`
	VMStorageTieringHighThroughputBPS int64 `mapstructure:"ROS_VM_STORAGE_TIERING_HIGH_THROUGHPUT_BPS"`

	// Snapshot staleness detection thresholds. When set via env var, the
	// corresponding field is locked (read-only via the settings API).
	SnapshotOrphanAgeDays       int     `mapstructure:"ROS_SNAPSHOT_ORPHAN_AGE_DAYS"`
	SnapshotNeverRestoredDays   int     `mapstructure:"ROS_SNAPSHOT_NEVER_RESTORED_DAYS"`
	SnapshotStaleDays           int     `mapstructure:"ROS_SNAPSHOT_STALE_DAYS"`
	SnapshotRedundantThreshold  int     `mapstructure:"ROS_SNAPSHOT_REDUNDANT_THRESHOLD"`
	SnapshotCostPerGiBMonth     float64 `mapstructure:"ROS_SNAPSHOT_COST_PER_GIB_MONTH"`
	SnapshotInventoryFreshHours int     `mapstructure:"ROS_SNAPSHOT_INVENTORY_FRESH_HOURS"`
	SnapshotInventoryRetentionH int     `mapstructure:"ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS"`
	// SnapshotStaleGraceHours bounds how long we tolerate missing *fresh* inventory
	// before treating the cluster as abandoned for snapshot_recommendation_sets cleanup.
	SnapshotStaleGraceHours int `mapstructure:"ROS_SNAPSHOT_STALE_GRACE_HOURS"`

	// Tag filtering and sync (Koku → ROS resolved tags on org_container_keys).
	// Enabled by default; gated by Koku's enabled tag keys in Settings > Tags.
	TagsEnabled                bool   `mapstructure:"ROS_TAGS_ENABLED"`
	TagsSource                 string `mapstructure:"ROS_TAGS_SOURCE"`
	TagsAllowedServiceAccounts string `mapstructure:"ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS"`
	TagsDevToken               string `mapstructure:"ROS_TAGS_DEV_TOKEN"`
	// TagsSyncMaxBodyMiB caps POST /internal/tags/sync request bodies in MiB (default 10).
	TagsSyncMaxBodyMiB int64 `mapstructure:"ROS_TAGS_SYNC_MAX_BODY_MIB"`
	// InternalTagsAuthRequired gates bearer auth on /internal/tags/* regardless of ROS_TAGS_SOURCE (default true).
	InternalTagsAuthRequired bool `mapstructure:"ROS_INTERNAL_TAGS_AUTH_REQUIRED"`
	// InternalAllowedOrgs restricts which org IDs internal endpoints may target (comma-separated).
	// Empty means all orgs are allowed (default for backward compatibility).
	InternalAllowedOrgs string `mapstructure:"ROS_INTERNAL_ALLOWED_ORGS"`
	// SynthManifestQuietPeriodSecs debounces recommendation runs for synthesized manifest IDs (default 30).
	SynthManifestQuietPeriodSecs int `mapstructure:"ROS_SYNTH_MANIFEST_QUIET_PERIOD"`

	// HistoryDefaultDays is the lookback window when start_date/end_date are omitted (default 30).
	HistoryDefaultDays int `mapstructure:"ROS_HISTORY_DEFAULT_DAYS"`

	// FleetSummaryCacheTTLSecs TTL for in-memory fleet summary cache (default 300 = 5m).
	FleetSummaryCacheTTLSecs int `mapstructure:"ROS_FLEET_SUMMARY_CACHE_TTL"`
	// FleetSummaryCacheMaxEntries caps the in-memory fleet summary LRU cache (default 256).
	FleetSummaryCacheMaxEntries int `mapstructure:"ROS_FLEET_SUMMARY_CACHE_CAPACITY"`
	// FleetHeatmapCacheMaxEntries caps the in-memory fleet heatmap LRU cache (default 128).
	// Heatmap entries are larger than fleet summary entries (~200 bytes × FleetHeatmapMaxNodes
	// per entry), so this defaults lower. At 128 entries × 1000 nodes × 200 bytes ≈ 25 MB.
	FleetHeatmapCacheMaxEntries int `mapstructure:"ROS_FLEET_HEATMAP_CACHE_CAPACITY"`

	// Idle / zombie workload classification (inline engine helper; env tier of 3-tier config).
	IdleDetectionEnabled     bool   `mapstructure:"ROS_IDLE_DETECTION_ENABLED"`
	IdleZombieCPUMillicores  int64  `mapstructure:"ROS_IDLE_ZOMBIE_CPU_MILLICORES"`
	IdleZombiePeakMillicores int64  `mapstructure:"ROS_IDLE_ZOMBIE_PEAK_MILLICORES"`
	IdleCPUUtilizationPct    int64  `mapstructure:"ROS_IDLE_CPU_UTILIZATION_PCT"`
	IdleMemUtilizationPct    int64  `mapstructure:"ROS_IDLE_MEMORY_UTILIZATION_PCT"`
	IdleBurstRatio           int64  `mapstructure:"ROS_IDLE_BURST_RATIO"`
	IdleMinObservationDays   int    `mapstructure:"ROS_IDLE_MIN_OBSERVATION_DAYS"`
	IdleExcludeNamespaces    string `mapstructure:"ROS_IDLE_EXCLUDE_NAMESPACES"`
	IdleExcludeWorkloadTypes string `mapstructure:"ROS_IDLE_EXCLUDE_WORKLOAD_TYPES"`
	IdleGPUSMActiveBP        int64  `mapstructure:"ROS_IDLE_GPU_SM_ACTIVE_BP"`
	IdleGPUDRAMActiveBP      int64  `mapstructure:"ROS_IDLE_GPU_DRAM_ACTIVE_BP"`

	// Global settings lock (ROS_SETTINGS_LOCKED): tenant Settings API writes disabled;
	// per-feature ROS_SETTINGS_LOCKED_* vars opt out when the global lock is true.
	SettingsLocked              bool `mapstructure:"ROS_SETTINGS_LOCKED"`
	SettingsLockedContainer     bool `mapstructure:"ROS_SETTINGS_LOCKED_CONTAINER"`
	SettingsLockedGPU           bool `mapstructure:"ROS_SETTINGS_LOCKED_GPU"`
	SettingsLockedNode          bool `mapstructure:"ROS_SETTINGS_LOCKED_NODE"`
	SettingsLockedNamespace     bool `mapstructure:"ROS_SETTINGS_LOCKED_NAMESPACE"`
	SettingsLockedPVC           bool `mapstructure:"ROS_SETTINGS_LOCKED_PVC"`
	SettingsLockedVM            bool `mapstructure:"ROS_SETTINGS_LOCKED_VM"`
	SettingsLockedQuota         bool `mapstructure:"ROS_SETTINGS_LOCKED_QUOTA"`
	SettingsLockedClusterQuota  bool `mapstructure:"ROS_SETTINGS_LOCKED_CLUSTER_QUOTA"`
	SettingsLockedIdle          bool `mapstructure:"ROS_SETTINGS_LOCKED_IDLE"`
	SettingsLockedSnapshot      bool `mapstructure:"ROS_SETTINGS_LOCKED_SNAPSHOT"`
	SettingsLockedBusinessHours bool `mapstructure:"ROS_SETTINGS_LOCKED_BUSINESS_HOURS"`
	SettingsLockedTerms         bool `mapstructure:"ROS_SETTINGS_LOCKED_TERMS"`

	// Plugin configuration (see internal/plugin/registry.go).
	EnabledPlugins  string `mapstructure:"ROS_ENABLED_PLUGINS"`
	DisabledPlugins string `mapstructure:"ROS_DISABLED_PLUGINS"`

	// Kubernetes auth for tag sync API mode (TokenReview).
	KubernetesSATokenPath    string `mapstructure:"KUBERNETES_SA_TOKEN_PATH"`
	KubernetesTokenReviewURL string `mapstructure:"KUBERNETES_TOKEN_REVIEW_URL"`

	// CSV download limits for Kafka-triggered URL ingestion (internal/utils).
	CSVMaxBodyBytes        int64  `mapstructure:"ROS_CSV_MAX_BODY_BYTES"`
	CSVDownloadTimeoutSecs int    `mapstructure:"ROS_CSV_DOWNLOAD_TIMEOUT_SECS"`
	CSVAllowedHosts        string `mapstructure:"ROS_CSV_ALLOWED_HOSTS"`
	CSVDenyPrivateNetworks bool   `mapstructure:"ROS_CSV_DENY_PRIVATE_NETWORKS"`
	LogPoisonPayload       bool   `mapstructure:"ROS_LOG_POISON_PAYLOAD"`
	HousekeeperShutdownGraceSecs int `mapstructure:"ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS"`
	// ShutdownTimeoutSecs is how long the Kafka consumer waits for in-flight handlers after SIGTERM.
	ShutdownTimeoutSecs int `mapstructure:"ROS_SHUTDOWN_TIMEOUT_SECONDS"`

	// Readiness probe depth (opt-in; default shallow DB-only probe for API-only pods).
	ReadinessCheckKafka bool   `mapstructure:"ROS_READINESS_CHECK_KAFKA"`
	ReadinessCheckS3    bool   `mapstructure:"ROS_READINESS_CHECK_S3"`
	ReadinessS3Bucket   string `mapstructure:"ROS_READINESS_S3_BUCKET"`
	ReadinessS3Endpoint string `mapstructure:"ROS_READINESS_S3_ENDPOINT"`
	ReadinessS3AccessKey string `mapstructure:"ROS_READINESS_S3_ACCESS_KEY"`
	ReadinessS3SecretKey string `mapstructure:"ROS_READINESS_S3_SECRET_KEY"`
	ReadinessS3Region    string `mapstructure:"ROS_READINESS_S3_REGION"`

	// Healthz probe thresholds for runtime degradation detection.
	HealthzMaxGoroutines int `mapstructure:"ROS_HEALTHZ_MAX_GOROUTINES"`
	HealthzMaxGCPauseMs  int `mapstructure:"ROS_HEALTHZ_MAX_GC_PAUSE_MS"`

	// APIMaxNodeResults caps rows returned by node utilization and GPU time-slicing list endpoints.
	APIMaxNodeResults int `mapstructure:"ROS_API_MAX_NODE_RESULTS"`

	// FleetHeatmapMaxNodes caps the number of nodes returned by the fleet heatmap endpoint.
	// Default 1000. Set to 0 to disable (not recommended for large fleets).
	FleetHeatmapMaxNodes int `mapstructure:"ROS_FLEET_HEATMAP_MAX_NODES"`

	// RateLimitEnabled gates the per-org token bucket rate limiter (default false for safe rollout).
	RateLimitEnabled bool `mapstructure:"ROS_API_RATE_LIMIT_ENABLED"`
	// RateLimitRPM is the sustained rate in requests per minute per org (default 60).
	RateLimitRPM int `mapstructure:"ROS_API_RATE_LIMIT_RPM"`
	// RateLimitBurst is the maximum burst size allowed above steady-state (default 30).
	RateLimitBurst int `mapstructure:"ROS_API_RATE_LIMIT_BURST"`
	// RateLimitExpiresMinutes is the TTL for idle rate-limiter buckets (default 5).
	RateLimitExpiresMinutes int `mapstructure:"ROS_API_RATE_LIMIT_EXPIRES_MINUTES"`

	// Per-plugin term overrides use dynamic env keys (not struct fields):
	// ROS_TERMS_<PLUGIN>_<TERM>_{WINDOW_DAYS,MIN_DATA_DAYS,DECAY_HALFLIFE_HOURS}
	// e.g. ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS. Read via [TermEnvPrefix] and [EnvString].

	//Unleash config
	UnleashClientAccessToken string
	UnleashHostname          string
	UnleashPort              int
	UnleashScheme            string
	UnleashFullURL           string
}

var (
	cfgMu sync.Mutex
	cfg   *Config
)

// migrateLegacyStaleCleanupDaysEnv maps the deprecated ROS_STALE_ARCHIVE_DAYS env var
// to ROS_STALE_CLEANUP_DAYS when only the legacy name is set.
func migrateLegacyStaleCleanupDaysEnv() {
	if os.Getenv("ROS_STALE_CLEANUP_DAYS") != "" {
		return
	}
	if legacy := os.Getenv("ROS_STALE_ARCHIVE_DAYS"); legacy != "" {
		_ = os.Setenv("ROS_STALE_CLEANUP_DAYS", legacy)
	}
}

func initConfig() {
	_ = godotenv.Load() // loads .env into process environment if present; no-op otherwise
	migrateLegacyStaleCleanupDaysEnv()
	viper.AutomaticEnv()
	if clowder.IsClowderEnabled() {
		viper.SetDefault("LogFormater", "json")

		c := clowder.LoadedConfig
		broker := c.Kafka.Brokers[0]
		viper.SetDefault("KAFKA_BOOTSTRAP_SERVERS", strings.Join(clowder.KafkaServers, ","))
		viper.SetDefault("UPLOAD_TOPIC", clowder.KafkaTopics["hccm.ros.events"].Name)
		viper.SetDefault("RECOMMENDATION_TOPIC", clowder.KafkaTopics["rosocp.kruize.recommendations"].Name)
		viper.SetDefault("SOURCES_EVENT_TOPIC", clowder.KafkaTopics["platform.sources.event-stream"].Name)

		// Kafka SSL Config
		if broker.Authtype != nil {
			viper.Set("KafkaUsername", broker.Sasl.Username)
			viper.Set("KafkaPassword", broker.Sasl.Password)
			viper.Set("KafkaSASLMechanism", broker.Sasl.SaslMechanism)
			viper.Set("KafkaSecurityProtocol", broker.Sasl.SecurityProtocol) //nolint:all
		}

		if broker.Cacert != nil {
			caPath, err := c.KafkaCa(broker)
			if err != nil {
				log.Fatalf("config: Kafka CA failed to write: %v", err)
			}
			viper.Set("KafkaCA", caPath)
		}

		// clowder DB Config
		viper.SetDefault("DBName", c.Database.Name)
		viper.SetDefault("DBUser", c.Database.Username)
		viper.SetDefault("DBPassword", c.Database.Password)
		viper.SetDefault("DBHost", c.Database.Hostname)
		viper.SetDefault("DBPort", c.Database.Port)
		viper.SetDefault("DBssl", c.Database.SslMode)
		viper.SetDefault("DBCACert", c.Database.RdsCa)

		// clowder RBAC Config
		for _, endpoint := range c.Endpoints {
			switch endpoint.App {
			case "rbac":
				viper.SetDefault("RBACHost", endpoint.Hostname)
				viper.SetDefault("RBACPort", endpoint.Port)
				viper.SetDefault("RBACProtocol", "http")
				viper.SetDefault("RBAC_ENABLE", true)
			case "sources-api":
				viper.SetDefault("SOURCES_API_BASE_URL", fmt.Sprintf("http://%v:%v", endpoint.Hostname, endpoint.Port))
			}
		}

		// clowder cloudwatch config
		viper.SetDefault("CwLogGroup", c.Logging.Cloudwatch.LogGroup)
		viper.SetDefault("CwRegion", c.Logging.Cloudwatch.Region)
		viper.SetDefault("CwAccessKey", c.Logging.Cloudwatch.AccessKeyId)
		viper.SetDefault("CwSecretKey", c.Logging.Cloudwatch.SecretAccessKey)
		viper.SetDefault("CW_LOG_STREAM_NAME", "rosocp")

		// prometheus config
		viper.SetDefault("PROMETHEUS_PORT", c.MetricsPort)

		// Unleash config
		if c.FeatureFlags != nil {
			viper.SetDefault("UnleashClientAccessToken", *c.FeatureFlags.ClientAccessToken)
			viper.SetDefault("UnleashHostname", c.FeatureFlags.Hostname)
			viper.SetDefault("UnleashScheme", string(c.FeatureFlags.Scheme))
			viper.SetDefault("UnleashPort", c.FeatureFlags.Port)
			viper.SetDefault("UnleashFullURL",
				fmt.Sprintf(
					"%s://%s:%d/api/",
					viper.GetString("UnleashScheme"),
					viper.GetString("UnleashHostname"),
					viper.GetInt("UnleashPort")))
		}
	} else {
		viper.SetDefault("LogFormater", "text")

		// Enable automatic environment variable binding
		viper.AutomaticEnv()

		// Kafka Config
		viper.SetDefault("KAFKA_BOOTSTRAP_SERVERS", "localhost:29092")
		viper.SetDefault("UPLOAD_TOPIC", "hccm.ros.events")
		viper.SetDefault("RECOMMENDATION_TOPIC", "rosocp.kruize.recommendations")
		viper.SetDefault("SOURCES_EVENT_TOPIC", "platform.sources.event-stream")

		// Kafka SASL/TLS config (env vars set by Helm chart when kafka.sasl is configured)
		_ = viper.BindEnv("KafkaSecurityProtocol", "KAFKA_SECURITY_PROTOCOL")
		_ = viper.BindEnv("KafkaSASLMechanism", "KAFKA_SASL_MECHANISM")
		_ = viper.BindEnv("KafkaUsername", "KAFKA_SASL_USERNAME")
		_ = viper.BindEnv("KafkaPassword", "KAFKA_SASL_PASSWORD")
		_ = viper.BindEnv("KafkaCA", "KAFKA_SSL_CA_LOCATION")

		// DB Config
		_ = viper.BindEnv("DBHost", "DB_HOST")
		viper.SetDefault("DBHost", "localhost")
		_ = viper.BindEnv("DBPort", "DB_PORT")
		viper.SetDefault("DBPort", "15432")
		_ = viper.BindEnv("DBName", "DB_NAME")
		viper.SetDefault("DBName", "postgres")
		_ = viper.BindEnv("DBUser", "DB_USER")
		viper.SetDefault("DBUser", "postgres")
		_ = viper.BindEnv("DBPassword", "DB_PASSWORD")
		viper.SetDefault("DBPassword", "postgres")
		_ = viper.BindEnv("DBssl", "DB_SSL")
		viper.SetDefault("DBssl", "disable")
		_ = viper.BindEnv("DBCACert", "DB_CA_CERT")
		viper.SetDefault("DBCACert", "")

		// default RBAC Config
		viper.SetDefault("RBACHost", "localhost")
		viper.SetDefault("RBACPort", "9080")
		viper.SetDefault("RBACProtocol", "http")
		viper.SetDefault("RBAC_ENABLE", false)

		// prometheus config
		viper.SetDefault("PROMETHEUS_PORT", "5005")

		// Sources-api-go
		viper.SetDefault("SOURCES_API_BASE_URL", "http://127.0.0.1:8002")

		// Unleash config
		viper.SetDefault("UnleashClientAccessToken", "rosocp:dev.token")
		viper.SetDefault("UnleashHostname", "localhost")
		viper.SetDefault("UnleashScheme", "http")
		viper.SetDefault("UnleashPort", 3063)
		viper.SetDefault(
			"UnleashFullURL",
			fmt.Sprintf(
				"%s://%s:%d/api/",
				viper.GetString("UnleashScheme"),
				viper.GetString("UnleashHostname"),
				viper.GetInt("UnleashPort")),
		)
	}

	viper.SetDefault("SOURCES_API_PREFIX", "/api/sources/v3.1")
	viper.SetDefault("SERVICE_NAME", "rosocp")
	viper.SetDefault("API_PORT", "8000")
	viper.SetDefault("KRUIZE_WAIT_TIME", "30")
	viper.SetDefault("KRUIZE_MAX_BULK_CHUNK_SIZE", 100)
	viper.SetDefault("KAFKA_CONSUMER_GROUP_ID", "ros-ocp")
	viper.SetDefault("KAFKA_AUTO_COMMIT", false)
	viper.SetDefault("ROS_KAFKA_MAX_TRANSIENT_RETRIES", 5)
	viper.SetDefault("ROS_KAFKA_DLQ_TOPIC", "hccm.ros.events.dlq")
	viper.SetDefault("LOG_LEVEL", "INFO")
	viper.SetDefault("KRUIZE_HOST", "localhost")
	viper.SetDefault("KRUIZE_PORT", "8080")
	viper.SetDefault("KRUIZE_URL", fmt.Sprintf("http://%s:%s", viper.GetString("KRUIZE_HOST"), viper.GetString("KRUIZE_PORT")))
	viper.SetDefault("KRUIZE_PERFORMANCE_PROFILE_VERSION", "v2.0")
	viper.SetDefault("RECOMMENDATION_POLL_INTERVAL_HOURS", 24)
	viper.SetDefault("DATA_RETENTION_PERIOD", 15)
	viper.SetDefault("READ_HEADER_TIMEOUT", 15)
	viper.SetDefault("ROS_API_READ_TIMEOUT", 60)
	viper.SetDefault("ROS_API_WRITE_TIMEOUT", 120)
	viper.SetDefault("ROS_API_IDLE_TIMEOUT", 120)
	viper.SetDefault("RECORD_LIMIT_CSV", 1000)
	viper.SetDefault("CSV_STREAM_INTERVAL", 100)
	viper.SetDefault("DISABLE_NAMESPACE_RECOMMENDATION", false)
	viper.SetDefault("DISABLE_NAMESPACE_RECOMMENDATION", true)
	viper.SetDefault("ROS_USE_NATIVE_ENGINE", true)
	viper.SetDefault("ROS_OOM_BASE_BUMP", 0.15)
	viper.SetDefault("ROS_OOM_MAX_BUMP", 1.60)
	viper.SetDefault("ROS_RETENTION_MONTHS", 6)
	viper.SetDefault("ROS_HISTORY_RETENTION_DAYS", 90)
	viper.SetDefault("ROS_STALENESS_THRESHOLD_HOURS", 48)
	viper.SetDefault("ROS_STALE_CLEANUP_DAYS", 30)
	viper.SetDefault("ROS_MAX_LOOKBACK_DAYS", 90)
	viper.SetDefault("MAXIMUM_COUNT_PER_QUERY_PARAM", 5)
	viper.SetDefault("ROS_API_MAX_OFFSET", 10000)
	viper.SetDefault("ROS_API_MAX_NODE_RESULTS", 1000)
	viper.SetDefault("ROS_FLEET_HEATMAP_MAX_NODES", 1000)
	viper.SetDefault("ROS_API_RATE_LIMIT_ENABLED", false)
	viper.SetDefault("ROS_API_RATE_LIMIT_RPM", 60)
	viper.SetDefault("ROS_API_RATE_LIMIT_BURST", 30)
	viper.SetDefault("ROS_API_RATE_LIMIT_EXPIRES_MINUTES", 5)
	viper.SetDefault("ROS_READINESS_CHECK_KAFKA", false)
	viper.SetDefault("ROS_READINESS_CHECK_S3", false)
	viper.SetDefault("ROS_READINESS_S3_REGION", "us-east-1")
	viper.SetDefault("ROS_HEALTHZ_MAX_GOROUTINES", 5000)
	viper.SetDefault("ROS_HEALTHZ_MAX_GC_PAUSE_MS", 100)
	viper.SetDefault("DEVELOPMENT", false)
	viper.SetDefault("GLOBAL_HTTP_CLIENT_TIMEOUT_SECS", 30)
	viper.SetDefault("UPDATE_KRUIZE_PERF_PROFILE", true)
	viper.SetDefault("ROS_DB_MAX_CONNS", 10)
	viper.SetDefault("ROS_DB_MAX_CONNS", defaultDBMaxConns)
	viper.SetDefault("ROS_DB_MIN_CONNS", 2)
	viper.SetDefault("ROS_DB_MAX_CONN_LIFETIME", 30)
	viper.SetDefault("ROS_DB_MAX_CONN_IDLE_TIME", 5)
	viper.SetDefault("ROS_DB_STATEMENT_CACHE_MODE", "describe")
	viper.SetDefault("ROS_DB_ACQUIRE_TIMEOUT_SECS", 5)
	viper.SetDefault("ROS_DB_STATEMENT_TIMEOUT", 25)
	viper.SetDefault("ROS_DB_INGEST_STATEMENT_TIMEOUT", 120)
	viper.SetDefault("ROS_INGEST_FLUSH_BATCH_SIZE", math.MaxInt32)
	viper.SetDefault("ROS_INGEST_STRICT_ANALYTICS", true)
	viper.SetDefault("ONPREM", false)
	viper.SetDefault("ROS_TERM_CONFIG_CACHE_MAX_ENTRIES", 0)
	viper.SetDefault("KOKU_MASU_URL", "")
	viper.SetDefault("ROS_COST_CACHE_MAX_ENTRIES", 1000)
	viper.SetDefault("ROS_SAVINGS_ESTIMATES_ENABLED", true)
	viper.SetDefault("ROS_BUSINESS_HOURS_ENABLED", true)
	viper.SetDefault("ROS_VISUAL_INSIGHTS_ENABLED", true)
	viper.SetDefault("ROS_HOURLY_VM_DIGESTS_ENABLED", true)
	viper.SetDefault("ROS_HOURLY_VM_DIGESTS_RETENTION_DAYS", 90)
	viper.SetDefault("ROS_HOURLY_NODE_DIGESTS_ENABLED", true)
	viper.SetDefault("ROS_HOURLY_NODE_DIGESTS_RETENTION_DAYS", 90)
	viper.SetDefault("ROS_THRESHOLD_RECALCULATION_ENABLED", true)
	viper.SetDefault("ROS_SAVINGS_RECALCULATION_ENABLED", true)
	viper.SetDefault("ROS_RBAC_CACHE_TTL", 60)
	viper.SetDefault("ROS_RBAC_CACHE_MAX_ENTRIES", 500)
	viper.SetDefault("KAFKA_LAG_POLL_INTERVAL_SECONDS", 30)
	viper.SetDefault("ROS_KAFKA_SESSION_TIMEOUT_MS", 120000)
	viper.SetDefault("ROS_KAFKA_HEARTBEAT_INTERVAL_MS", 30000)
	viper.SetDefault("ROS_KAFKA_WORKERS", 3)
	viper.SetDefault("ROS_KAFKA_PARALLEL", true)
	viper.SetDefault("ROS_MANIFEST_DOWNLOAD_WORKERS", 2)
	viper.SetDefault("ROS_THRESHOLD_RECALC_CONCURRENCY", 3)
	viper.SetDefault("ROS_RESHIP_POLLER_INTERVAL_SECS", 60)
	viper.SetDefault("ROS_RESHIP_MAX_RETRIES", 10)
	viper.SetDefault("ROS_RESHIP_CONCURRENCY", 2)
	viper.SetDefault("ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK", false)
	viper.SetDefault("ROS_REPLICA_TARGET_UTILIZATION_PCT", 70)
	viper.SetDefault("ROS_CONTAINER_CPU_COST_PERCENTILE", 0.60)
	viper.SetDefault("ROS_CONTAINER_CPU_PERF_PERCENTILE", 0.98)
	viper.SetDefault("ROS_CONTAINER_MEM_COST_PERCENTILE", 0.95)
	viper.SetDefault("ROS_CONTAINER_MEM_PERF_PERCENTILE", 1.0)
	viper.SetDefault("ROS_CONTAINER_MIN_MARGIN", 1.15)
	viper.SetDefault("ROS_CONTAINER_MAX_MARGIN", 1.50)
	viper.SetDefault("ROS_CONTAINER_LIMIT_MULTIPLIER", 1.05)
	viper.SetDefault("ROS_CONTAINER_CPU_FLOOR_MC", 25)
	viper.SetDefault("ROS_CONTAINER_MEM_FLOOR_KIB", 4096)
	viper.SetDefault("ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC", 10)
	viper.SetDefault("ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB", 10240)
	viper.SetDefault("ROS_IDLE_DETECTION_ENABLED", true)
	viper.SetDefault("ROS_IDLE_ZOMBIE_CPU_MILLICORES", 1)
	viper.SetDefault("ROS_IDLE_ZOMBIE_PEAK_MILLICORES", 10)
	viper.SetDefault("ROS_IDLE_CPU_UTILIZATION_PCT", 2)
	viper.SetDefault("ROS_IDLE_MEMORY_UTILIZATION_PCT", 5)
	viper.SetDefault("ROS_IDLE_BURST_RATIO", 10)
	viper.SetDefault("ROS_IDLE_MIN_OBSERVATION_DAYS", 14)
	viper.SetDefault("ROS_IDLE_EXCLUDE_NAMESPACES", "kube-system,openshift-*")
	viper.SetDefault("ROS_IDLE_EXCLUDE_WORKLOAD_TYPES", "DaemonSet")
	viper.SetDefault("ROS_IDLE_GPU_SM_ACTIVE_BP", 500)
	viper.SetDefault("ROS_IDLE_GPU_DRAM_ACTIVE_BP", 500)
	viper.SetDefault("ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD", 100.0)
	viper.SetDefault("ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD", 0.5)
	viper.SetDefault("ROS_CONTAINER_SPARSE_DATA_THRESHOLD", 2)
	viper.SetDefault("ROS_NAMESPACE_CPU_COST_PERCENTILE", 0.60)
	viper.SetDefault("ROS_NAMESPACE_CPU_PERF_PERCENTILE", 0.98)
	viper.SetDefault("ROS_NAMESPACE_MEM_COST_PERCENTILE", 0.95)
	viper.SetDefault("ROS_NAMESPACE_MEM_PERF_PERCENTILE", 1.0)
	viper.SetDefault("ROS_NAMESPACE_MIN_MARGIN", 1.15)
	viper.SetDefault("ROS_NAMESPACE_MAX_MARGIN", 1.50)
	viper.SetDefault("ROS_NAMESPACE_LIMIT_MULTIPLIER", 1.05)
	viper.SetDefault("ROS_NAMESPACE_CPU_FLOOR_MC", 25)
	viper.SetDefault("ROS_NAMESPACE_MEM_FLOOR_KIB", 4096)
	viper.SetDefault("ROS_NAMESPACE_IDLE_CPU_THRESHOLD_MC", 10)
	viper.SetDefault("ROS_NAMESPACE_IDLE_MEM_THRESHOLD_KIB", 10240)
	viper.SetDefault("ROS_NAMESPACE_MEM_TREND_SLOPE_THRESHOLD", 500.0)
	viper.SetDefault("ROS_NAMESPACE_LOW_CONFIDENCE_THRESHOLD", 0.5)
	viper.SetDefault("ROS_NAMESPACE_SPARSE_DATA_THRESHOLD", 2)
	viper.SetDefault("ROS_NODE_UNDERUTIL_THRESHOLD", 0.30)
	viper.SetDefault("ROS_NODE_OVERCOMMIT_THRESHOLD", 1.50)
	viper.SetDefault("ROS_NODE_ALLOCATABLE_FACTOR", 0.93)
	viper.SetDefault("ROS_NODE_STRANDED_IMBALANCE_THRESHOLD", 0.6)
	viper.SetDefault("ROS_NODE_EMA_ALPHA", 0.3)
	viper.SetDefault("ROS_NODE_COST_TARGET_UTILIZATION", 0.80)
	viper.SetDefault("ROS_NODE_PERF_TARGET_UTILIZATION", 0.55)
	viper.SetDefault("ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER", 2.0)
	viper.SetDefault("ROS_NODE_TREND_MIN_DAYS", 3)
	viper.SetDefault("ROS_NODE_ZOMBIE_CPU_MC", 200)
	viper.SetDefault("ROS_NODE_ZOMBIE_MAX_PODS", 5)
	viper.SetDefault("ROS_NODE_IDLE_CPU_UTIL_PCT", 10)
	viper.SetDefault("ROS_NODE_IDLE_MEM_UTIL_PCT", 10)
	viper.SetDefault("ROS_NODE_IDLE_MAX_PODS", 10)
	viper.SetDefault("ROS_NODE_POD_HEADROOM_CONSOLIDATION_GATE", 0.15)
	viper.SetDefault("ROS_NODE_POD_HEADROOM_NOTIFICATION_THRESHOLD", 0.10)
	viper.SetDefault("ROS_GPU_IDLE_THRESHOLD", 0.02)
	viper.SetDefault("ROS_GPU_UNDERUTILIZED_SM_THRESHOLD", 0.25)
	viper.SetDefault("ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD", 0.15)
	viper.SetDefault("ROS_GPU_MEMBOUND_DRAM_THRESHOLD", 0.60)
	viper.SetDefault("ROS_GPU_MEMBOUND_TENSOR_THRESHOLD", 0.15)
	viper.SetDefault("ROS_GPU_FB_HEADROOM_FACTOR", 1.20)
	viper.SetDefault("ROS_GPU_COMPUTE_BOUND_DRAM_THRESHOLD", 0.30)
	viper.SetDefault("ROS_GPU_MIG_FB_PERCENTILE", 0.98)
	viper.SetDefault("ROS_GPU_CONFIDENCE_DAYS_TIER1", 3)
	viper.SetDefault("ROS_GPU_CONFIDENCE_DAYS_TIER2", 7)
	viper.SetDefault("ROS_GPU_CONFIDENCE_DAYS_TIER3", 14)
	viper.SetDefault("ROS_GPU_SPIKE_RATIO_THRESHOLD", 5.0)
	viper.SetDefault("ROS_GPU_SPIKE_CONFIDENCE_PENALTY", 0.70)
	viper.SetDefault("ROS_GPU_NO_PROFILING_CONFIDENCE_FACTOR", 0.50)
	viper.SetDefault("ROS_GPU_TIMESLICING_MAJORITY_THRESHOLD", 0.50)
	viper.SetDefault("ROS_GPU_TIMESLICING_MIN_REPLICAS", 2)
	viper.SetDefault("ROS_GPU_TIMESLICING_MAX_REPLICAS", 8)
	viper.SetDefault("ROS_GPU_TIMESLICING_BASE_PENALTY", 0.70)
	viper.SetDefault("ROS_GPU_TIMESLICING_IMPACTED_WEIGHT", 0.30)
	viper.SetDefault("ROS_GPU_NODE_FRESHNESS_DAYS", 7)
	viper.SetDefault("ROS_PVC_OVERSIZED_THRESHOLD", 0.20)
	viper.SetDefault("ROS_PVC_NEAR_FULL_THRESHOLD", 0.85)
	viper.SetDefault("ROS_PVC_MIN_TREND_DAYS", 2)
	viper.SetDefault("ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER", 2)
	viper.SetDefault("ROS_PVC_MIN_RECOMMENDED_GIB", 1)
	viper.SetDefault("ROS_PVC_DAYS_TO_FULL_ALERT", 30)
	viper.SetDefault("ROS_QUOTA_HEADROOM_PERCENT", 10)
	viper.SetDefault("ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT", 90)
	viper.SetDefault("ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT", 70)
	viper.SetDefault("ROS_CLUSTER_QUOTA_HEADROOM_PERCENT", 10)
	viper.SetDefault("ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT", 90)
	viper.SetDefault("ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT", 70)
	viper.SetDefault("ROS_VM_CPU_PERCENTILE_COST", 0.95)
	viper.SetDefault("ROS_VM_CPU_PERCENTILE_PERF", 0.99)
	viper.SetDefault("ROS_VM_CPU_MARGIN_MIN", 0.15)
	viper.SetDefault("ROS_VM_CPU_MARGIN_MAX", 0.50)
	viper.SetDefault("ROS_VM_CPU_ADAPTIVE_MARGIN_ENABLED", true)
	viper.SetDefault("ROS_VM_MEM_MARGIN_MIN", 0.20)
	viper.SetDefault("ROS_VM_DOWNSIZE_HYSTERESIS_RATIO", 0.60)
	viper.SetDefault("ROS_VM_MIN_VCPU_CHANGE", 2)
	viper.SetDefault("ROS_VM_MIN_GIB_CHANGE", 2)
	viper.SetDefault("ROS_VM_IDLE_CPU_MC", 50)
	viper.SetDefault("ROS_VM_IDLE_MEMORY_MIB", 512)
	viper.SetDefault("ROS_VM_IDLE_CPU_MC_WINDOWS", 200)
	viper.SetDefault("ROS_VM_IDLE_MEMORY_MIB_WINDOWS", 3072)
	viper.SetDefault("ROS_VM_LINUX_MEMORY_FLOOR_GIB", 1)
	viper.SetDefault("ROS_VM_WINDOWS_MEMORY_FLOOR_GIB", 2)
	viper.SetDefault("ROS_VM_DISK_PROJECTION_DAYS", 30)
	viper.SetDefault("ROS_VM_DISK_HEADROOM_PCT", 0.25)
	viper.SetDefault("ROS_VM_DISK_ROUND_STEP_GIB", 10)
	viper.SetDefault("ROS_VM_DISK_MIN_GROWTH_MIB_PER_DAY", 100)
	viper.SetDefault("ROS_VM_HIGH_IOPS_THRESHOLD", 3000)
	viper.SetDefault("ROS_VM_IO_SEQUENTIAL_THRESHOLD_BYTES", 65536)
	viper.SetDefault("ROS_VM_IO_RANDOM_THRESHOLD_BYTES", 16384)
	viper.SetDefault("ROS_VM_IO_MIN_IOPS_CLASSIFICATION", 100)
	viper.SetDefault("ROS_VM_ENABLE_INSTANCE_TYPE_MATCHING", true)
	viper.SetDefault("ROS_VM_ABANDONED_MIN_DAYS", 3)
	viper.SetDefault("ROS_VM_WINDOWS_KERNEL_RESERVE_GIB", 1.5)
	viper.SetDefault("ROS_VM_DOWNSIZE_STABILITY_DAYS", 3)
	viper.SetDefault("ROS_VM_CRASH_LOOP_RESTART_THRESHOLD", 3)
	viper.SetDefault("ROS_VM_GPU_IDLE_THRESHOLD", 0.05)
	viper.SetDefault("ROS_VM_GPU_UNDERUTIL_THRESHOLD", 0.30)
	viper.SetDefault("ROS_VM_GPU_FB_SATURATION_MIB", 0)
	viper.SetDefault("ROS_VM_GPU_COMPUTE_SATURATION_THRESHOLD", 0.85)
	viper.SetDefault("ROS_VM_GPU_TIMESLICE_MIN_REPLICAS", 2)
	viper.SetDefault("ROS_VM_GPU_TIMESLICE_MAX_REPLICAS", 16)
	viper.SetDefault("ROS_VM_GPU_TIMESLICE_FB_SAFETY_BP", 8000)
	viper.SetDefault("ROS_VM_GPU_TIMESLICE_DRAM_PENALTY_BP", 5000)
	viper.SetDefault("ROS_VM_NETWORK_THROUGHPUT_THRESHOLD_BPS", 62500000)
	viper.SetDefault("ROS_VM_NETWORK_PPS_THRESHOLD", 100000)
	viper.SetDefault("ROS_VM_NETWORK_DROP_RATIO_BP", 10)
	viper.SetDefault("ROS_VM_NETWORK_SUSTAINED_DAYS", 7)
	viper.SetDefault("ROS_VM_ENABLE_NETWORK_SERIES", true)
	viper.SetDefault("ROS_VM_REC_HISTORY_RETENTION_DAYS", 90)
	viper.SetDefault("ROS_VM_ENABLE_PLACEMENT_CHECKS", true)
	viper.SetDefault("ROS_VM_PLACEMENT_SKEW_RATIO", 3)
	viper.SetDefault("ROS_VM_ENABLE_SHARED_PVC_CORRELATION", true)
	viper.SetDefault("ROS_VM_NUMA_NODE_MEMORY_GIB", 64.0)
	viper.SetDefault("ROS_VM_NUMA_ASSUMED_SOCKETS", 2)
	viper.SetDefault("ROS_VM_ENABLE_POWER_SCHEDULE", true)
	viper.SetDefault("ROS_VM_POWER_OFF_MIN_IDLE_DAYS", 14)
	viper.SetDefault("ROS_VM_POWER_OFF_IDLE_RATIO_THRESHOLD", 0.7)
	viper.SetDefault("ROS_VM_NETWORK_QOS_ENABLED", true)
	viper.SetDefault("ROS_VM_NETWORK_QOS_SRIOV_DROP_THRESHOLD", 0.01)
	viper.SetDefault("ROS_VM_NETWORK_QOS_SRIOV_THROUGHPUT_BPS", 5000000000)
	viper.SetDefault("ROS_VM_NETWORK_QOS_DPDK_PPS_THRESHOLD", 500000)
	viper.SetDefault("ROS_VM_STORAGE_TIERING_ENABLED", true)
	viper.SetDefault("ROS_VM_STORAGE_TIERING_MIN_DAYS", 7)
	viper.SetDefault("ROS_VM_STORAGE_TIERING_COLD_MIN_DAYS", 14)
	viper.SetDefault("ROS_VM_STORAGE_TIERING_IOPS_MIN_DAYS", 7)
	viper.SetDefault("ROS_VM_STORAGE_TIERING_THROUGHPUT_MIN_DAYS", 7)
	viper.SetDefault("ROS_VM_STORAGE_TIERING_HIGH_IOPS_THRESHOLD", 5000)
	viper.SetDefault("ROS_VM_STORAGE_TIERING_HIGH_THROUGHPUT_BPS", 104857600)
	viper.SetDefault("ROS_SNAPSHOT_ORPHAN_AGE_DAYS", 7)
	viper.SetDefault("ROS_SNAPSHOT_NEVER_RESTORED_DAYS", 30)
	viper.SetDefault("ROS_SNAPSHOT_STALE_DAYS", 90)
	viper.SetDefault("ROS_SNAPSHOT_REDUNDANT_THRESHOLD", 3)
	viper.SetDefault("ROS_SNAPSHOT_COST_PER_GIB_MONTH", 0.05)
	viper.SetDefault("ROS_SNAPSHOT_INVENTORY_FRESH_HOURS", 6)
	viper.SetDefault("ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS", 48)
	viper.SetDefault("ROS_SNAPSHOT_STALE_GRACE_HOURS", 48)
	viper.SetDefault("ROS_TAGS_ENABLED", true)
	viper.SetDefault("ROS_INTERNAL_TAGS_AUTH_REQUIRED", true)
	viper.SetDefault("ROS_SYNTH_MANIFEST_QUIET_PERIOD", 30)
	viper.SetDefault("ROS_HISTORY_DEFAULT_DAYS", 30)
	viper.SetDefault("ROS_FLEET_SUMMARY_CACHE_TTL", 300)
	viper.SetDefault("ROS_FLEET_SUMMARY_CACHE_CAPACITY", 256)
	viper.SetDefault("ROS_FLEET_HEATMAP_CACHE_CAPACITY", 128)
	viper.SetDefault("ROS_TAGS_SOURCE", "db")
	viper.SetDefault("ROS_TAGS_SYNC_MAX_BODY_MIB", 10)
	viper.SetDefault("ROS_ENABLED_PLUGINS", "")
	viper.SetDefault("ROS_DISABLED_PLUGINS", "")
	viper.SetDefault("KUBERNETES_SA_TOKEN_PATH", "/var/run/secrets/kubernetes.io/serviceaccount/token")
	viper.SetDefault("KUBERNETES_TOKEN_REVIEW_URL", "https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews")
	viper.SetDefault("ROS_CSV_MAX_BODY_BYTES", 524288000)
	viper.SetDefault("ROS_CSV_DOWNLOAD_TIMEOUT_SECS", 120)
	viper.SetDefault("ROS_CSV_ALLOWED_HOSTS", "")
	viper.SetDefault("ROS_CSV_DENY_PRIVATE_NETWORKS", true)
	viper.SetDefault("ROS_LOG_POISON_PAYLOAD", false)
	viper.SetDefault("ROS_HOUSEKEEPER_SHUTDOWN_GRACE_SECS", 30)
	viper.SetDefault("ROS_SHUTDOWN_TIMEOUT_SECONDS", 30)
	viper.SetDefault("ROS_SETTINGS_LOCKED", false)
	viper.SetDefault("ROS_SETTINGS_LOCKED_CONTAINER", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_GPU", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_NODE", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_NAMESPACE", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_PVC", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_VM", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_QUOTA", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_CLUSTER_QUOTA", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_IDLE", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_SNAPSHOT", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_BUSINESS_HOURS", true)
	viper.SetDefault("ROS_SETTINGS_LOCKED_TERMS", true)

	// Unleash config
	viper.SetDefault("UnleashClientAccessToken", "rosocp:dev.token")
	viper.SetDefault("UnleashHostname", "0.0.0.0")
	viper.SetDefault("UnleashScheme", "http")
	viper.SetDefault("UnleashPort", 3063)
	viper.SetDefault(
		"UnleashFullURL",
		fmt.Sprintf(
			"%s://%s:%d/api/",
			viper.GetString("UnleashScheme"),
			viper.GetString("UnleashHostname"),
			viper.GetInt("UnleashPort")),
	)

	// Hack till viper issue get fix - https://github.com/spf13/viper/issues/761
	envKeysMap := &map[string]interface{}{}
	var probeCfg Config
	if err := mapstructure.Decode(&probeCfg, envKeysMap); err != nil {
		log.Printf("config: mapstructure decode for env key probe: %v", err)
	}
	for k := range *envKeysMap {
		if bindErr := viper.BindEnv(k); bindErr != nil {
			log.Printf("config: viper.BindEnv(%s): %v", k, bindErr)
		}
	}
	// Legacy env names kept for backward compatibility.
	_ = viper.BindEnv("KubernetesSATokenPath", "KUBERNETES_SA_TOKEN_PATH", "KUBERNETES_SERVICE_ACCOUNT_TOKEN_PATH")
	_ = viper.BindEnv("CSVDownloadTimeoutSecs", "ROS_CSV_DOWNLOAD_TIMEOUT_SECS", "ROS_CSV_DOWNLOAD_TIMEOUT_SECONDS")
	// Requirements spec name; ROS_STALENESS_THRESHOLD_HOURS remains the primary env var.
	_ = viper.BindEnv("ROS_STALENESS_THRESHOLD_HOURS", "ROS_STALENESS_THRESHOLD_HOURS", "ROS_STALE_DATA_THRESHOLD_HOURS")
	// Deprecated: ROS_STALE_ARCHIVE_DAYS renamed to ROS_STALE_CLEANUP_DAYS (behavior is delete, not archive).
	_ = viper.BindEnv("ROS_STALE_CLEANUP_DAYS", "ROS_STALE_CLEANUP_DAYS", "ROS_STALE_ARCHIVE_DAYS")
	// Deprecated: DB_POOL_SIZE was a legacy Helm/Clowder name; prefer ROS_DB_MAX_CONNS.
	_ = viper.BindEnv("ROS_DB_MAX_CONNS", "ROS_DB_MAX_CONNS", "DB_POOL_SIZE")

	if err := viper.Unmarshal(&cfg); err != nil {
		log.Fatalf("config: cannot unmarshal configuration: %v", err)
	}
	validateLoadedConfig(cfg)
}

func validateLoadedConfig(c *Config) {
	if c == nil {
		return
	}
	if c.KokuMasuURL == "" {
		log.Printf("WARN: KOKU_MASU_URL not configured — savings estimates will be unavailable")
	}
	if c.DBHost == "" || c.DBPort == "" || c.DBName == "" || c.DBUser == "" {
		log.Fatalf("config: required database settings missing (DBHost=%q, DBPort=%q, DBName=%q, DBUser=%q)",
			c.DBHost, c.DBPort, c.DBName, c.DBUser)
	}
	if c.MaxLookbackDays <= 0 {
		log.Printf("config: ROS_MAX_LOOKBACK_DAYS (%d) is invalid; using 14", c.MaxLookbackDays)
		c.MaxLookbackDays = 14
	}
	if c.StalenessThresholdHours <= 0 {
		log.Printf("config: ROS_STALENESS_THRESHOLD_HOURS (%d) is invalid; using 48", c.StalenessThresholdHours)
		c.StalenessThresholdHours = 48
	}
	if c.StaleCleanupDays <= 0 {
		log.Printf("config: ROS_STALE_CLEANUP_DAYS (%d) is invalid; using 30", c.StaleCleanupDays)
		c.StaleCleanupDays = 30
	}
	if c.DBMaxConns <= 0 {
		log.Printf("config: ROS_DB_MAX_CONNS (%d) is invalid; using %d", c.DBMaxConns, defaultDBMaxConns)
		c.DBMaxConns = defaultDBMaxConns
	}
	// ADR-0320: warn if pool cannot satisfy worst-case concurrent demand.
	if needed := c.ManifestDownloadWorkers*c.KafkaWorkers + 2; c.DBMaxConns < needed {
		log.Printf("WARN: ROS_DB_MAX_CONNS (%d) < ManifestDownloadWorkers(%d) × KafkaWorkers(%d) + 2 = %d; "+
			"pool exhaustion may occur under concurrent ingestion load (see ADR-0320)",
			c.DBMaxConns, c.ManifestDownloadWorkers, c.KafkaWorkers, needed)
	}
	if c.DBMinConns < 0 {
		log.Printf("config: ROS_DB_MIN_CONNS (%d) is invalid; using 2", c.DBMinConns)
		c.DBMinConns = 2
	}
	if c.DBMaxConnLifetimeMins <= 0 {
		c.DBMaxConnLifetimeMins = 30
	}
	if c.DBMaxConnIdleTimeMins <= 0 {
		c.DBMaxConnIdleTimeMins = 5
	}
	if c.DBStatementCacheMode == "" {
		c.DBStatementCacheMode = "describe"
	}
	if c.DBAcquireTimeoutSecs < 0 {
		log.Printf("config: ROS_DB_ACQUIRE_TIMEOUT_SECS (%d) is invalid; using 5", c.DBAcquireTimeoutSecs)
		c.DBAcquireTimeoutSecs = 5
	}
	if c.DBStatementTimeoutSecs <= 0 {
		c.DBStatementTimeoutSecs = 25
	}
	if c.DBIngestStatementTimeoutSecs <= 0 {
		c.DBIngestStatementTimeoutSecs = 120
	}
	if c.IngestFlushBatchSize <= 0 {
		c.IngestFlushBatchSize = math.MaxInt32
	}
	if c.ReshipConcurrency <= 0 {
		c.ReshipConcurrency = 2
	}
	if c.KubernetesSATokenPath == "" {
		c.KubernetesSATokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}
	if c.KubernetesTokenReviewURL == "" {
		c.KubernetesTokenReviewURL = "https://kubernetes.default.svc/apis/authentication.k8s.io/v1/tokenreviews"
	}
	if c.CSVMaxBodyBytes <= 0 {
		c.CSVMaxBodyBytes = 104857600 // 100 MiB
	}
	if c.CSVDownloadTimeoutSecs <= 0 {
		c.CSVDownloadTimeoutSecs = 120
	}
	if c.TagsSyncMaxBodyMiB <= 0 {
		c.TagsSyncMaxBodyMiB = 10
	}
	if c.APIMaxOffset <= 0 {
		c.APIMaxOffset = 10000
	}
	if c.APIMaxNodeResults <= 0 {
		c.APIMaxNodeResults = 1000
	}
	if c.FleetHeatmapMaxNodes <= 0 {
		c.FleetHeatmapMaxNodes = 1000
	}
	if c.RateLimitRPM <= 0 {
		c.RateLimitRPM = 60
	}
	if c.RateLimitBurst <= 0 {
		c.RateLimitBurst = c.RateLimitRPM / 2
		if c.RateLimitBurst < 1 {
			c.RateLimitBurst = 1
		}
	}
	if c.RateLimitExpiresMinutes <= 0 {
		c.RateLimitExpiresMinutes = 5
	}
	if c.ReadinessS3Region == "" {
		c.ReadinessS3Region = "us-east-1"
	}
	if c.HousekeeperShutdownGraceSecs <= 0 {
		c.HousekeeperShutdownGraceSecs = 30
	}
	if c.ShutdownTimeoutSecs <= 0 {
		c.ShutdownTimeoutSecs = 30
	}
	if c.KafkaLagPollIntervalSecs <= 0 {
		c.KafkaLagPollIntervalSecs = 30
	}
	if c.KafkaSessionTimeoutMS <= 0 {
		c.KafkaSessionTimeoutMS = 120000
	}
	if c.KafkaHeartbeatIntervalMS <= 0 {
		c.KafkaHeartbeatIntervalMS = 30000
	}
	if c.ManifestDownloadWorkers <= 0 {
		c.ManifestDownloadWorkers = 2
	}
	if c.ReplicaTargetUtilizationPct < 10 || c.ReplicaTargetUtilizationPct > 95 {
		log.Printf("config: ROS_REPLICA_TARGET_UTILIZATION_PCT (%d) outside valid range 10-95; using 70", c.ReplicaTargetUtilizationPct)
		c.ReplicaTargetUtilizationPct = 70
	}
}

// EffectiveTermConfigCacheMaxEntries returns the resolved cache size:
// explicit ROS_TERM_CONFIG_CACHE_MAX_ENTRIES > 0 wins; otherwise 5 (on-prem) or 1000 (SaaS).
func (c *Config) EffectiveTermConfigCacheMaxEntries() int {
	if c.TermConfigCacheMaxEntries > 0 {
		return c.TermConfigCacheMaxEntries
	}
	if c.OnPrem {
		return 5
	}
	return 1000
}

// TagsSyncBodyLimit returns an Echo BodyLimit middleware size string for tag sync routes.
func (c *Config) TagsSyncBodyLimit() string {
	if c == nil || c.TagsSyncMaxBodyMiB <= 0 {
		return "10M"
	}
	return fmt.Sprintf("%dM", c.TagsSyncMaxBodyMiB)
}

// CORSAllowOrigins returns explicit CORS Allow-Origin values. When empty and DEVELOPMENT is true,
// returns wildcard. When empty in non-development, returns nil (caller should deny cross-origin).
func (c *Config) CORSAllowOrigins() []string {
	if c == nil || strings.TrimSpace(c.CORSAllowedOrigins) == "" {
		if c != nil && c.Development {
			return []string{"*"}
		}
		return nil
	}
	parts := strings.Split(c.CORSAllowedOrigins, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func GetConfig() *Config {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg == nil {
		initConfig()
		log.Println("config: initialized")
	}
	return cfg
}
