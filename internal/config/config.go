package config

import (
	"fmt"
	"log"
	"strings"

	"github.com/joho/godotenv"
	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"

	clowder "github.com/redhatinsights/app-common-go/pkg/api/v1"
)

type Config struct {
	// Application config
	ServiceName                     string `mapstructure:"SERVICE_NAME"`
	LogFormatter                    string `mapstructure:"LogFormater"`
	LogLevel                        string `mapstructure:"LOG_LEVEL"`
	RecommendationPollIntervalHours int    `mapstructure:"RECOMMENDATION_POLL_INTERVAL_HOURS"`
	DataRetentionPeriod             int    `mapstructure:"DATA_RETENTION_PERIOD"`
	ReadHeaderTimeout               int    `mapstructure:"READ_HEADER_TIMEOUT"`
	RecordLimitCSV                  int    `mapstructure:"RECORD_LIMIT_CSV"`
	CSVStreamInterval               int    `mapstructure:"CSV_STREAM_INTERVAL"`
	MaxCountPerQueryParam           int    `mapstructure:"MAXIMUM_COUNT_PER_QUERY_PARAM"`
	UpdateKruizePerfProfile         bool   `mapstructure:"UPDATE_KRUIZE_PERF_PROFILE"`

	// Kafka config
	KafkaBootstrapServers string `mapstructure:"KAFKA_BOOTSTRAP_SERVERS"`
	KafkaConsumerGroupId  string `mapstructure:"KAFKA_CONSUMER_GROUP_ID"`
	KafkaAutoCommit       bool   `mapstructure:"KAFKA_AUTO_COMMIT"`
	UploadTopic           string `mapstructure:"UPLOAD_TOPIC"`
	RecommendationTopic   string `mapstructure:"RECOMMENDATION_TOPIC"`
	SourcesEventTopic     string `mapstructure:"SOURCES_EVENT_TOPIC"`
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

	// pgxpool tuning (REST / GPU paths). ROS_DB_MAX_CONNS defaults to 10;
	// ROS_DB_ACQUIRE_TIMEOUT_SECS sets ContextWithAcquireTimeout (0 = no limit).
	DBMaxConns              int    `mapstructure:"ROS_DB_MAX_CONNS"`
	DBMinConns              int    `mapstructure:"ROS_DB_MIN_CONNS"`
	DBMaxConnLifetimeMins   int    `mapstructure:"ROS_DB_MAX_CONN_LIFETIME"`
	DBMaxConnIdleTimeMins   int    `mapstructure:"ROS_DB_MAX_CONN_IDLE_TIME"`
	DBStatementCacheMode    string `mapstructure:"ROS_DB_STATEMENT_CACHE_MODE"`
	DBAcquireTimeoutSecs    int    `mapstructure:"ROS_DB_ACQUIRE_TIMEOUT_SECS"`

	// RBAC config
	RBACHost     string
	RBACPort     string
	RBACProtocol string
	RBACEnabled  bool `mapstructure:"RBAC_ENABLE"`
	// RBACCacheTTLSecs caches RBAC permissions in-memory (0 disables). Default 60.
	RBACCacheTTLSecs int `mapstructure:"ROS_RBAC_CACHE_TTL"`

	// KafkaWorkers is the worker pool size when ROS_KAFKA_PARALLEL is enabled (default 3).
	KafkaWorkers int `mapstructure:"ROS_KAFKA_WORKERS"`
	// KafkaParallel enables parallel Kafka message processing (default true).
	KafkaParallel bool `mapstructure:"ROS_KAFKA_PARALLEL"`

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

	// Sources-api-go config
	SourceApiBaseUrl string `mapstructure:"SOURCES_API_BASE_URL"`
	SourceApiPrefix  string `mapstructure:"SOURCES_API_PREFIX"`

	// Deprecated: use ROS_ENABLED_PLUGINS=kruize instead. Kept for backward compatibility.
	UseNativeEngine bool    `mapstructure:"ROS_USE_NATIVE_ENGINE"`
	OOMBaseBump     float64 `mapstructure:"ROS_OOM_BASE_BUMP"`
	OOMMaxBump      float64 `mapstructure:"ROS_OOM_MAX_BUMP"`
	RetentionMonths int     `mapstructure:"ROS_RETENTION_MONTHS"`
	MaxLookbackDays int     `mapstructure:"ROS_MAX_LOOKBACK_DAYS"`

	// History/quality data retention (days). Defaults to 90.
	// Separate from RetentionMonths because history tables grow faster
	// (one row per container per term per engine per run).
	HistoryRetentionDays int `mapstructure:"ROS_HISTORY_RETENTION_DAYS"`

	// Staleness threshold in hours. Recommendations with no new data beyond
	// this threshold are marked stale. Defaults to 72 (3 days).
	StalenessThresholdHours int `mapstructure:"ROS_STALENESS_THRESHOLD_HOURS"`

	// Stale archive days. Stale recommendations older than this are deleted
	// during the retention sweep. Defaults to 30.
	StaleArchiveDays int `mapstructure:"ROS_STALE_ARCHIVE_DAYS"`

	// Koku masu API URL for fetching cost data (savings estimates)
	KokuMasuURL string `mapstructure:"KOKU_MASU_URL"`

	// SavingsEstimatesEnabled gates Masu effective-rates fetches for container and GPU savings.
	// Default true; set ROS_SAVINGS_ESTIMATES_ENABLED=false to disable all savings calculations.
	SavingsEstimatesEnabled bool `mapstructure:"ROS_SAVINGS_ESTIMATES_ENABLED"`

	// BusinessHoursEnabled gates business-hours settings routes, OpenAPI paths, capabilities,
	// and dual-stream ingestion. Default true; set ROS_BUSINESS_HOURS_ENABLED=false to disable.
	BusinessHoursEnabled bool `mapstructure:"ROS_BUSINESS_HOURS_ENABLED"`

	// ThresholdRecalculationEnabled triggers async recommendation recalculation when tenant
	// threshold settings change via the Settings API PUT. Default true.
	ThresholdRecalculationEnabled bool `mapstructure:"ROS_THRESHOLD_RECALCULATION_ENABLED"`

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
	ContainerCPUCostPercentile      float64 `mapstructure:"ROS_CONTAINER_CPU_COST_PERCENTILE"`
	ContainerCPUPerfPercentile      float64 `mapstructure:"ROS_CONTAINER_CPU_PERF_PERCENTILE"`
	ContainerMemCostPercentile      float64 `mapstructure:"ROS_CONTAINER_MEM_COST_PERCENTILE"`
	ContainerMemPerfPercentile      float64 `mapstructure:"ROS_CONTAINER_MEM_PERF_PERCENTILE"`
	ContainerMinMargin              float64 `mapstructure:"ROS_CONTAINER_MIN_MARGIN"`
	ContainerMaxMargin              float64 `mapstructure:"ROS_CONTAINER_MAX_MARGIN"`
	ContainerLimitMultiplier        float64 `mapstructure:"ROS_CONTAINER_LIMIT_MULTIPLIER"`
	ContainerCPUFloorMC             int64   `mapstructure:"ROS_CONTAINER_CPU_FLOOR_MC"`
	ContainerIdleCPUThresholdMC     int64   `mapstructure:"ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC"`
	ContainerIdleMemThresholdKiB    int64   `mapstructure:"ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB"`
	ContainerMemTrendSlopeThreshold float64 `mapstructure:"ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD"`
	ContainerLowConfidenceThreshold float32 `mapstructure:"ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD"`

	// Namespace sizing and classification thresholds (same shape as container).
	NamespaceCPUCostPercentile      float64 `mapstructure:"ROS_NAMESPACE_CPU_COST_PERCENTILE"`
	NamespaceCPUPerfPercentile      float64 `mapstructure:"ROS_NAMESPACE_CPU_PERF_PERCENTILE"`
	NamespaceMemCostPercentile      float64 `mapstructure:"ROS_NAMESPACE_MEM_COST_PERCENTILE"`
	NamespaceMemPerfPercentile      float64 `mapstructure:"ROS_NAMESPACE_MEM_PERF_PERCENTILE"`
	NamespaceMinMargin              float64 `mapstructure:"ROS_NAMESPACE_MIN_MARGIN"`
	NamespaceMaxMargin              float64 `mapstructure:"ROS_NAMESPACE_MAX_MARGIN"`
	NamespaceLimitMultiplier        float64 `mapstructure:"ROS_NAMESPACE_LIMIT_MULTIPLIER"`
	NamespaceCPUFloorMC             int64   `mapstructure:"ROS_NAMESPACE_CPU_FLOOR_MC"`
	NamespaceIdleCPUThresholdMC     int64   `mapstructure:"ROS_NAMESPACE_IDLE_CPU_THRESHOLD_MC"`
	NamespaceIdleMemThresholdKiB    int64   `mapstructure:"ROS_NAMESPACE_IDLE_MEM_THRESHOLD_KIB"`
	NamespaceMemTrendSlopeThreshold float64 `mapstructure:"ROS_NAMESPACE_MEM_TREND_SLOPE_THRESHOLD"`
	NamespaceLowConfidenceThreshold float32 `mapstructure:"ROS_NAMESPACE_LOW_CONFIDENCE_THRESHOLD"`

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

	//Unleash config
	UnleashClientAccessToken string
	UnleashHostname          string
	UnleashPort              int
	UnleashScheme            string
	UnleashFullURL           string
}

var cfg *Config = nil

func initConfig() {
	_ = godotenv.Load() // loads .env into process environment if present; no-op otherwise
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
	viper.SetDefault("LOG_LEVEL", "INFO")
	viper.SetDefault("KRUIZE_HOST", "localhost")
	viper.SetDefault("KRUIZE_PORT", "8080")
	viper.SetDefault("KRUIZE_URL", fmt.Sprintf("http://%s:%s", viper.GetString("KRUIZE_HOST"), viper.GetString("KRUIZE_PORT")))
	viper.SetDefault("KRUIZE_PERFORMANCE_PROFILE_VERSION", "v2.0")
	viper.SetDefault("RECOMMENDATION_POLL_INTERVAL_HOURS", 24)
	viper.SetDefault("DATA_RETENTION_PERIOD", 15)
	viper.SetDefault("READ_HEADER_TIMEOUT", 15)
	viper.SetDefault("RECORD_LIMIT_CSV", 1000)
	viper.SetDefault("CSV_STREAM_INTERVAL", 100)
	viper.SetDefault("DISABLE_NAMESPACE_RECOMMENDATION", false)
	viper.SetDefault("DISABLE_NAMESPACE_RECOMMENDATION", true)
	viper.SetDefault("ROS_USE_NATIVE_ENGINE", true)
	viper.SetDefault("ROS_OOM_BASE_BUMP", 0.15)
	viper.SetDefault("ROS_OOM_MAX_BUMP", 1.60)
	viper.SetDefault("ROS_RETENTION_MONTHS", 6)
	viper.SetDefault("ROS_HISTORY_RETENTION_DAYS", 90)
	viper.SetDefault("ROS_STALENESS_THRESHOLD_HOURS", 72)
	viper.SetDefault("ROS_STALE_ARCHIVE_DAYS", 30)
	viper.SetDefault("ROS_MAX_LOOKBACK_DAYS", 90)
	viper.SetDefault("MAXIMUM_COUNT_PER_QUERY_PARAM", 5)
	viper.SetDefault("GLOBAL_HTTP_CLIENT_TIMEOUT_SECS", 30)
	viper.SetDefault("UPDATE_KRUIZE_PERF_PROFILE", true)
	viper.SetDefault("ROS_DB_MAX_CONNS", 10)
	viper.SetDefault("ROS_DB_MIN_CONNS", 2)
	viper.SetDefault("ROS_DB_MAX_CONN_LIFETIME", 30)
	viper.SetDefault("ROS_DB_MAX_CONN_IDLE_TIME", 5)
	viper.SetDefault("ROS_DB_STATEMENT_CACHE_MODE", "describe")
	viper.SetDefault("ROS_DB_ACQUIRE_TIMEOUT_SECS", 5)
	viper.SetDefault("KOKU_MASU_URL", "")
	viper.SetDefault("ROS_SAVINGS_ESTIMATES_ENABLED", true)
	viper.SetDefault("ROS_BUSINESS_HOURS_ENABLED", true)
	viper.SetDefault("ROS_THRESHOLD_RECALCULATION_ENABLED", true)
	viper.SetDefault("ROS_RBAC_CACHE_TTL", 60)
	viper.SetDefault("ROS_KAFKA_WORKERS", 3)
	viper.SetDefault("ROS_KAFKA_PARALLEL", true)
	viper.SetDefault("ROS_THRESHOLD_RECALC_CONCURRENCY", 3)
	viper.SetDefault("ROS_RESHIP_POLLER_INTERVAL_SECS", 60)
	viper.SetDefault("ROS_RESHIP_MAX_RETRIES", 10)
	viper.SetDefault("ROS_RESHIP_CONCURRENCY", 2)
	viper.SetDefault("ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK", false)
	viper.SetDefault("ROS_CONTAINER_CPU_COST_PERCENTILE", 0.60)
	viper.SetDefault("ROS_CONTAINER_CPU_PERF_PERCENTILE", 0.98)
	viper.SetDefault("ROS_CONTAINER_MEM_COST_PERCENTILE", 0.95)
	viper.SetDefault("ROS_CONTAINER_MEM_PERF_PERCENTILE", 1.0)
	viper.SetDefault("ROS_CONTAINER_MIN_MARGIN", 1.15)
	viper.SetDefault("ROS_CONTAINER_MAX_MARGIN", 1.50)
	viper.SetDefault("ROS_CONTAINER_LIMIT_MULTIPLIER", 1.05)
	viper.SetDefault("ROS_CONTAINER_CPU_FLOOR_MC", 25)
	viper.SetDefault("ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC", 10)
	viper.SetDefault("ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB", 10240)
	viper.SetDefault("ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD", 100.0)
	viper.SetDefault("ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD", 0.5)
	viper.SetDefault("ROS_NAMESPACE_CPU_COST_PERCENTILE", 0.60)
	viper.SetDefault("ROS_NAMESPACE_CPU_PERF_PERCENTILE", 0.98)
	viper.SetDefault("ROS_NAMESPACE_MEM_COST_PERCENTILE", 0.95)
	viper.SetDefault("ROS_NAMESPACE_MEM_PERF_PERCENTILE", 1.0)
	viper.SetDefault("ROS_NAMESPACE_MIN_MARGIN", 1.15)
	viper.SetDefault("ROS_NAMESPACE_MAX_MARGIN", 1.50)
	viper.SetDefault("ROS_NAMESPACE_LIMIT_MULTIPLIER", 1.05)
	viper.SetDefault("ROS_NAMESPACE_CPU_FLOOR_MC", 25)
	viper.SetDefault("ROS_NAMESPACE_IDLE_CPU_THRESHOLD_MC", 10)
	viper.SetDefault("ROS_NAMESPACE_IDLE_MEM_THRESHOLD_KIB", 10240)
	viper.SetDefault("ROS_NAMESPACE_MEM_TREND_SLOPE_THRESHOLD", 500.0)
	viper.SetDefault("ROS_NAMESPACE_LOW_CONFIDENCE_THRESHOLD", 0.5)
	viper.SetDefault("ROS_NODE_UNDERUTIL_THRESHOLD", 0.30)
	viper.SetDefault("ROS_NODE_OVERCOMMIT_THRESHOLD", 1.50)
	viper.SetDefault("ROS_NODE_ALLOCATABLE_FACTOR", 0.93)
	viper.SetDefault("ROS_NODE_STRANDED_IMBALANCE_THRESHOLD", 0.6)
	viper.SetDefault("ROS_NODE_EMA_ALPHA", 0.3)
	viper.SetDefault("ROS_NODE_COST_TARGET_UTILIZATION", 0.80)
	viper.SetDefault("ROS_NODE_PERF_TARGET_UTILIZATION", 0.55)
	viper.SetDefault("ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER", 2.0)
	viper.SetDefault("ROS_NODE_TREND_MIN_DAYS", 3)
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
	viper.SetDefault("ROS_PVC_MIN_TREND_DAYS", 7)
	viper.SetDefault("ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER", 2)
	viper.SetDefault("ROS_PVC_MIN_RECOMMENDED_GIB", 1)
	viper.SetDefault("ROS_PVC_DAYS_TO_FULL_ALERT", 30)
	viper.SetDefault("ROS_SNAPSHOT_ORPHAN_AGE_DAYS", 7)
	viper.SetDefault("ROS_SNAPSHOT_NEVER_RESTORED_DAYS", 30)
	viper.SetDefault("ROS_SNAPSHOT_STALE_DAYS", 90)
	viper.SetDefault("ROS_SNAPSHOT_REDUNDANT_THRESHOLD", 3)
	viper.SetDefault("ROS_SNAPSHOT_COST_PER_GIB_MONTH", 0.05)
	viper.SetDefault("ROS_SNAPSHOT_INVENTORY_FRESH_HOURS", 6)
	viper.SetDefault("ROS_SNAPSHOT_INVENTORY_RETENTION_HOURS", 48)
	viper.SetDefault("ROS_SNAPSHOT_STALE_GRACE_HOURS", 48)

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

	if err := viper.Unmarshal(&cfg); err != nil {
		log.Fatalf("config: cannot unmarshal configuration: %v", err)
	}
	validateLoadedConfig(cfg)
}

func validateLoadedConfig(c *Config) {
	if c == nil {
		return
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
		log.Printf("config: ROS_STALENESS_THRESHOLD_HOURS (%d) is invalid; using 72", c.StalenessThresholdHours)
		c.StalenessThresholdHours = 72
	}
	if c.StaleArchiveDays <= 0 {
		log.Printf("config: ROS_STALE_ARCHIVE_DAYS (%d) is invalid; using 30", c.StaleArchiveDays)
		c.StaleArchiveDays = 30
	}
	if c.DBMaxConns <= 0 {
		log.Printf("config: ROS_DB_MAX_CONNS (%d) is invalid; using 10", c.DBMaxConns)
		c.DBMaxConns = 10
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
	if c.ReshipConcurrency <= 0 {
		c.ReshipConcurrency = 2
	}
}

func GetConfig() *Config {
	if cfg == nil {
		initConfig()
		log.Println("config: initialized")
	}
	return cfg
}
