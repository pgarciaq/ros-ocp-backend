package model

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestScanContainerColumnAlignment verifies that scanNativeContainerRowsNoSort
// maps every SQL column to the correct Go struct field. It SELECTs a row of
// literal sentinel values (one unique value per column, in nativeDetailSelect
// order) and asserts each NativeRecommendationRow field individually. Any
// positional swap silently introduced during PROF-2 maintenance would cause
// exactly one assertion to fail, identifying the misaligned column.
func TestScanContainerColumnAlignment(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	sqlDB := stdlib.OpenDBFromPool(pool)

	// 79 columns matching nativeDetailSelect (without the page-sort suffix).
	// Every value is unique so a positional swap cannot go undetected.
	query := `SELECT
		'org_c01'::text,                               -- 1  OrgID
		'cluster_c02'::text,                           -- 2  ClusterUUID
		'namespace_c03'::text,                         -- 3  Namespace
		'workload_c04'::text,                          -- 4  Workload
		'workloadtype_c05'::text,                      -- 5  WorkloadType
		'container_c06'::text,                         -- 6  ContainerName
		'term_c07'::text,                              -- 7  Term
		'engine_c08'::text,                            -- 8  Engine
		901::bigint,                                   -- 9  RecCPURequestMC
		1001::bigint,                                  -- 10 RecCPULimitMC
		1101::bigint,                                  -- 11 RecMemRequestKiB
		1201::bigint,                                  -- 12 RecMemLimitKiB
		1301::bigint,                                  -- 13 CurrentCPURequestMC
		1401::bigint,                                  -- 14 CurrentCPULimitMC
		1501::bigint,                                  -- 15 CurrentMemRequestKiB
		1601::bigint,                                  -- 16 CurrentMemLimitKiB
		1701::integer,                                 -- 17 VariationCPURequestPct
		1801::integer,                                 -- 18 VariationCPULimitPct
		1901::integer,                                 -- 19 VariationMemRequestPct
		2001::integer,                                 -- 20 VariationMemLimitPct
		ARRAY[21,22]::smallint[],                      -- 21 NotificationCodes
		22.5::real,                                    -- 22 ConfidenceLevel
		true::boolean,                                 -- 23 Stale
		2401::integer,                                 -- 24 PodCountMin
		2501::integer,                                 -- 25 PodCountMax
		2601::integer,                                 -- 26 PodCountAvg
		2701::integer,                                 -- 27 DesiredReplicas
		2801::integer,                                 -- 28 AvailableReplicas
		2901::integer,                                 -- 29 RecommendedReplicas
		'repconf_c30'::text,                           -- 30 ReplicaConfidence
		'repexpl_c31'::text,                           -- 31 ReplicaExplanation
		3201::bigint,                                  -- 32 EstimatedSavingsCents
		3301::bigint,                                  -- 33 EstimatedCPUSavingsCents
		3401::bigint,                                  -- 34 EstimatedMemSavingsCents
		'idlestate_c35'::text,                         -- 35 IdleState
		'2026-01-15 10:00:36+00'::timestamptz,         -- 36 IdleSince
		3701::integer,                                 -- 37 IdleDurationDays
		3801::bigint,                                  -- 38 PeakCPUMillicores
		3901::bigint,                                  -- 39 PeakMemoryBytes
		4001::bigint,                                  -- 40 EstimatedWasteCents
		'2026-02-15 10:00:41+00'::timestamptz,         -- 41 MonitoringEndTime
		4201::integer,                                 -- 42 ExplDataDays
		43.5::double precision,                        -- 43 ExplDecayHalfLifeHours
		4401::bigint,                                  -- 44 ExplCPUCostPctMC
		4501::bigint,                                  -- 45 ExplCPUPerfPctMC
		4601::bigint,                                  -- 46 ExplCPUUsageP95MC
		4701::bigint,                                  -- 47 ExplCPUUsageP50MC
		4801::bigint,                                  -- 48 ExplCPUUsageMeanMC
		4901::integer,                                 -- 49 ExplCPUAdaptiveMarginBP
		50.5::double precision,                        -- 50 ExplCPUTrendSlope
		5101::bigint,                                  -- 51 ExplMemCostPctKiB
		5201::bigint,                                  -- 52 ExplMemPerfPctKiB
		5301::bigint,                                  -- 53 ExplMemUsageP95KiB
		5401::bigint,                                  -- 54 ExplMemUsageP50KiB
		5501::bigint,                                  -- 55 ExplMemUsageMeanKiB
		5601::integer,                                 -- 56 ExplMemAdaptiveMarginBP
		57.5::double precision,                        -- 57 ExplMemTrendSlope
		5801::bigint,                                  -- 58 ExplOOMCountSum
		true::boolean,                                 -- 59 ExplOOMBumpApplied
		false::boolean,                                -- 60 ExplCPUFloorApplied
		true::boolean,                                 -- 61 ExplMemFloorApplied
		false::boolean,                                -- 62 ExplIsIdle
		6301::integer,                                 -- 63 ExplGPUSMActiveAvgBP
		6401::integer,                                 -- 64 ExplGPUTensorActiveAvgBP
		6501::integer,                                 -- 65 ExplGPUDRAMActiveAvgBP
		6601::integer,                                 -- 66 ExplGPUFBUsageMaxMiB
		6701::integer,                                 -- 67 ExplGPUFBP98MiB
		'gpurec_c68'::text,                            -- 68 ExplGPURecommendedProfile
		'gpucur_c69'::text,                            -- 69 ExplGPUCurrentProfile
		true::boolean,                                 -- 70 ExplGPUHasProfilingData
		false::boolean,                                -- 71 ExplGPUMemoryBound
		'2026-03-15 10:00:00+00'::timestamptz,         -- 72 UpdatedAt
		'src_c73'::text,                               -- 73 SourceID
		'alias_c74'::text,                             -- 74 ClusterAlias
		'2026-04-15 10:01:15+00'::timestamptz,         -- 75 LastReported
		true::boolean,                                 -- 76 AnalyticsIncomplete
		'2026-05-15 10:01:17+00'::timestamptz,         -- 77 AnalyticsIncompleteAt
		false::boolean,                                -- 78 IngestHooksFailed
		'2026-06-15 10:01:19+00'::timestamptz          -- 79 IngestHooksFailedAt
	`

	rows, err := sqlDB.Query(query)
	require.NoError(t, err)
	defer rows.Close()

	result, err := scanNativeContainerRowsNoSort(rows, 1)
	require.NoError(t, err)
	require.Len(t, result, 1)
	r := result[0]

	// --- string fields ---
	assert.Equal(t, "org_c01", r.OrgID, "OrgID")
	assert.Equal(t, "cluster_c02", r.ClusterUUID, "ClusterUUID")
	assert.Equal(t, "namespace_c03", r.Namespace, "Namespace")
	assert.Equal(t, "workload_c04", r.Workload, "Workload")
	assert.Equal(t, "workloadtype_c05", r.WorkloadType, "WorkloadType")
	assert.Equal(t, "container_c06", r.ContainerName, "ContainerName")
	assert.Equal(t, "term_c07", r.Term, "Term")
	assert.Equal(t, "engine_c08", r.Engine, "Engine")

	// --- *int64 recommendation values ---
	assertInt64Ptr(t, 901, r.RecCPURequestMC, "RecCPURequestMC")
	assertInt64Ptr(t, 1001, r.RecCPULimitMC, "RecCPULimitMC")
	assertInt64Ptr(t, 1101, r.RecMemRequestKiB, "RecMemRequestKiB")
	assertInt64Ptr(t, 1201, r.RecMemLimitKiB, "RecMemLimitKiB")

	// --- *int64 current values ---
	assertInt64Ptr(t, 1301, r.CurrentCPURequestMC, "CurrentCPURequestMC")
	assertInt64Ptr(t, 1401, r.CurrentCPULimitMC, "CurrentCPULimitMC")
	assertInt64Ptr(t, 1501, r.CurrentMemRequestKiB, "CurrentMemRequestKiB")
	assertInt64Ptr(t, 1601, r.CurrentMemLimitKiB, "CurrentMemLimitKiB")

	// --- *int32 variation values ---
	assertInt32Ptr(t, 1701, r.VariationCPURequestPct, "VariationCPURequestPct")
	assertInt32Ptr(t, 1801, r.VariationCPULimitPct, "VariationCPULimitPct")
	assertInt32Ptr(t, 1901, r.VariationMemRequestPct, "VariationMemRequestPct")
	assertInt32Ptr(t, 2001, r.VariationMemLimitPct, "VariationMemLimitPct")

	// --- SmallintArray ---
	require.NotNil(t, r.NotificationCodes, "NotificationCodes")
	assert.Equal(t, SmallintArray{21, 22}, r.NotificationCodes, "NotificationCodes")

	// --- *float32 ---
	require.NotNil(t, r.ConfidenceLevel, "ConfidenceLevel")
	assert.InDelta(t, float32(22.5), *r.ConfidenceLevel, 0.01, "ConfidenceLevel")

	// --- bool ---
	assert.True(t, r.Stale, "Stale")

	// --- *int pod counts / replicas ---
	assertIntPtr(t, 2401, r.PodCountMin, "PodCountMin")
	assertIntPtr(t, 2501, r.PodCountMax, "PodCountMax")
	assertIntPtr(t, 2601, r.PodCountAvg, "PodCountAvg")
	assertIntPtr(t, 2701, r.DesiredReplicas, "DesiredReplicas")
	assertIntPtr(t, 2801, r.AvailableReplicas, "AvailableReplicas")
	assertIntPtr(t, 2901, r.RecommendedReplicas, "RecommendedReplicas")

	// --- *string ---
	assertStringPtr(t, "repconf_c30", r.ReplicaConfidence, "ReplicaConfidence")
	assertStringPtr(t, "repexpl_c31", r.ReplicaExplanation, "ReplicaExplanation")

	// --- *int64 savings ---
	assertInt64Ptr(t, 3201, r.EstimatedSavingsCents, "EstimatedSavingsCents")
	assertInt64Ptr(t, 3301, r.EstimatedCPUSavingsCents, "EstimatedCPUSavingsCents")
	assertInt64Ptr(t, 3401, r.EstimatedMemSavingsCents, "EstimatedMemSavingsCents")

	// --- idle fields ---
	assert.Equal(t, "idlestate_c35", r.IdleState, "IdleState")
	assertTimePtr(t, time.Date(2026, 1, 15, 10, 0, 36, 0, time.UTC), r.IdleSince, "IdleSince")
	assertIntPtr(t, 3701, r.IdleDurationDays, "IdleDurationDays")

	// --- *int64 peak / waste ---
	assertInt64Ptr(t, 3801, r.PeakCPUMillicores, "PeakCPUMillicores")
	assertInt64Ptr(t, 3901, r.PeakMemoryBytes, "PeakMemoryBytes")
	assertInt64Ptr(t, 4001, r.EstimatedWasteCents, "EstimatedWasteCents")

	// --- *time.Time ---
	assertTimePtr(t, time.Date(2026, 2, 15, 10, 0, 41, 0, time.UTC), r.MonitoringEndTime, "MonitoringEndTime")

	// --- explanation factors ---
	assertIntPtr(t, 4201, r.ExplDataDays, "ExplDataDays")
	assertFloat64Ptr(t, 43.5, r.ExplDecayHalfLifeHours, "ExplDecayHalfLifeHours")
	assertInt64Ptr(t, 4401, r.ExplCPUCostPctMC, "ExplCPUCostPctMC")
	assertInt64Ptr(t, 4501, r.ExplCPUPerfPctMC, "ExplCPUPerfPctMC")
	assertInt64Ptr(t, 4601, r.ExplCPUUsageP95MC, "ExplCPUUsageP95MC")
	assertInt64Ptr(t, 4701, r.ExplCPUUsageP50MC, "ExplCPUUsageP50MC")
	assertInt64Ptr(t, 4801, r.ExplCPUUsageMeanMC, "ExplCPUUsageMeanMC")
	assertInt32Ptr(t, 4901, r.ExplCPUAdaptiveMarginBP, "ExplCPUAdaptiveMarginBP")
	assertFloat64Ptr(t, 50.5, r.ExplCPUTrendSlope, "ExplCPUTrendSlope")
	assertInt64Ptr(t, 5101, r.ExplMemCostPctKiB, "ExplMemCostPctKiB")
	assertInt64Ptr(t, 5201, r.ExplMemPerfPctKiB, "ExplMemPerfPctKiB")
	assertInt64Ptr(t, 5301, r.ExplMemUsageP95KiB, "ExplMemUsageP95KiB")
	assertInt64Ptr(t, 5401, r.ExplMemUsageP50KiB, "ExplMemUsageP50KiB")
	assertInt64Ptr(t, 5501, r.ExplMemUsageMeanKiB, "ExplMemUsageMeanKiB")
	assertInt32Ptr(t, 5601, r.ExplMemAdaptiveMarginBP, "ExplMemAdaptiveMarginBP")
	assertFloat64Ptr(t, 57.5, r.ExplMemTrendSlope, "ExplMemTrendSlope")
	assertInt64Ptr(t, 5801, r.ExplOOMCountSum, "ExplOOMCountSum")
	assertBoolPtr(t, true, r.ExplOOMBumpApplied, "ExplOOMBumpApplied")
	assertBoolPtr(t, false, r.ExplCPUFloorApplied, "ExplCPUFloorApplied")
	assertBoolPtr(t, true, r.ExplMemFloorApplied, "ExplMemFloorApplied")
	assertBoolPtr(t, false, r.ExplIsIdle, "ExplIsIdle")

	// --- GPU explanation factors ---
	assertInt32Ptr(t, 6301, r.ExplGPUSMActiveAvgBP, "ExplGPUSMActiveAvgBP")
	assertInt32Ptr(t, 6401, r.ExplGPUTensorActiveAvgBP, "ExplGPUTensorActiveAvgBP")
	assertInt32Ptr(t, 6501, r.ExplGPUDRAMActiveAvgBP, "ExplGPUDRAMActiveAvgBP")
	assertInt32Ptr(t, 6601, r.ExplGPUFBUsageMaxMiB, "ExplGPUFBUsageMaxMiB")
	assertInt32Ptr(t, 6701, r.ExplGPUFBP98MiB, "ExplGPUFBP98MiB")
	assertStringPtr(t, "gpurec_c68", r.ExplGPURecommendedProfile, "ExplGPURecommendedProfile")
	assertStringPtr(t, "gpucur_c69", r.ExplGPUCurrentProfile, "ExplGPUCurrentProfile")
	assertBoolPtr(t, true, r.ExplGPUHasProfilingData, "ExplGPUHasProfilingData")
	assertBoolPtr(t, false, r.ExplGPUMemoryBound, "ExplGPUMemoryBound")

	// --- cluster join fields ---
	assert.Equal(t, time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC), r.UpdatedAt.UTC(), "UpdatedAt")
	assert.Equal(t, "src_c73", r.SourceID, "SourceID")
	assert.Equal(t, "alias_c74", r.ClusterAlias, "ClusterAlias")
	assert.Equal(t, time.Date(2026, 4, 15, 10, 1, 15, 0, time.UTC), r.LastReported.UTC(), "LastReported")
	assert.True(t, r.AnalyticsIncomplete, "AnalyticsIncomplete")
	assertTimePtr(t, time.Date(2026, 5, 15, 10, 1, 17, 0, time.UTC), r.AnalyticsIncompleteAt, "AnalyticsIncompleteAt")
	assert.False(t, r.IngestHooksFailed, "IngestHooksFailed")
	assertTimePtr(t, time.Date(2026, 6, 15, 10, 1, 19, 0, time.UTC), r.IngestHooksFailedAt, "IngestHooksFailedAt")

	// --- fields NOT in nativeDetailSelect must remain zero-valued ---
	assert.Nil(t, r.RecommendationAppliedAt, "RecommendationAppliedAt should not be scanned")
	assert.Nil(t, r.Category, "Category should not be scanned")
	assert.Nil(t, r.CategoryCPU, "CategoryCPU should not be scanned")
	assert.Nil(t, r.CategoryMemory, "CategoryMemory should not be scanned")
	assert.Nil(t, r.PageSortText, "PageSortText should not be scanned (NoSort variant)")
}

// TestScanNamespaceColumnAlignment verifies that scanNativeNamespaceRowsNoSort
// maps every SQL column to the correct Go struct field.
func TestScanNamespaceColumnAlignment(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	sqlDB := stdlib.OpenDBFromPool(pool)

	// 53 columns matching nativeNSSelect (without the page-sort suffix).
	query := `SELECT
		'org_n01'::text,                               -- 1  OrgID
		'cluster_n02'::text,                           -- 2  ClusterUUID
		'nsname_n03'::text,                            -- 3  NamespaceName
		'term_n04'::text,                              -- 4  Term
		'engine_n05'::text,                            -- 5  Engine
		601::bigint,                                   -- 6  RecCPURequestMC
		701::bigint,                                   -- 7  RecCPULimitMC
		801::bigint,                                   -- 8  RecMemRequestKiB
		901::bigint,                                   -- 9  RecMemLimitKiB
		1001::bigint,                                  -- 10 CurrentCPURequestMC
		1101::bigint,                                  -- 11 CurrentCPULimitMC
		1201::bigint,                                  -- 12 CurrentMemRequestKiB
		1301::bigint,                                  -- 13 CurrentMemLimitKiB
		1401::integer,                                 -- 14 VariationCPURequestPct
		1501::integer,                                 -- 15 VariationCPULimitPct
		1601::integer,                                 -- 16 VariationMemRequestPct
		1701::integer,                                 -- 17 VariationMemLimitPct
		ARRAY[18,19]::smallint[],                      -- 18 NotificationCodes
		19.5::real,                                    -- 19 ConfidenceLevel
		true::boolean,                                 -- 20 Stale
		'idlestate_n21'::text,                         -- 21 IdleState
		'2026-01-21 10:00:22+00'::timestamptz,         -- 22 IdleSince
		2301::integer,                                 -- 23 IdleDurationDays
		2401::bigint,                                  -- 24 EstimatedWasteCents
		2501::bigint,                                  -- 25 EstimatedSavingsCents
		2601::bigint,                                  -- 26 EstimatedCPUSavingsCents
		2701::bigint,                                  -- 27 EstimatedMemSavingsCents
		'2026-02-15 10:00:28+00'::timestamptz,         -- 28 MonitoringEndTime
		'2026-03-15 10:00:29+00'::timestamptz,         -- 29 UpdatedAt
		3001::integer,                                 -- 30 ExplDataDays
		31.5::double precision,                        -- 31 ExplDecayHalfLifeHours
		3201::bigint,                                  -- 32 ExplCPUCostPctMC
		3301::bigint,                                  -- 33 ExplCPUPerfPctMC
		3401::bigint,                                  -- 34 ExplCPUUsageP95MC
		3501::bigint,                                  -- 35 ExplCPUUsageP50MC
		3601::bigint,                                  -- 36 ExplCPUUsageMeanMC
		3701::integer,                                 -- 37 ExplCPUAdaptiveMarginBP
		38.5::double precision,                        -- 38 ExplCPUTrendSlope
		3901::bigint,                                  -- 39 ExplMemCostPctKiB
		4001::bigint,                                  -- 40 ExplMemPerfPctKiB
		4101::bigint,                                  -- 41 ExplMemUsageP95KiB
		4201::bigint,                                  -- 42 ExplMemUsageP50KiB
		4301::bigint,                                  -- 43 ExplMemUsageMeanKiB
		4401::integer,                                 -- 44 ExplMemAdaptiveMarginBP
		45.5::double precision,                        -- 45 ExplMemTrendSlope
		4601::bigint,                                  -- 46 ExplOOMCountSum
		true::boolean,                                 -- 47 ExplOOMBumpApplied
		false::boolean,                                -- 48 ExplCPUFloorApplied
		true::boolean,                                 -- 49 ExplMemFloorApplied
		false::boolean,                                -- 50 ExplIsIdle
		'src_n51'::text,                               -- 51 SourceID
		'alias_n52'::text,                             -- 52 ClusterAlias
		'2026-04-15 10:01:53+00'::timestamptz          -- 53 LastReported
	`

	rows, err := sqlDB.Query(query)
	require.NoError(t, err)
	defer rows.Close()

	result, err := scanNativeNamespaceRowsNoSort(rows, 1)
	require.NoError(t, err)
	require.Len(t, result, 1)
	r := result[0]

	// --- string fields ---
	assert.Equal(t, "org_n01", r.OrgID, "OrgID")
	assert.Equal(t, "cluster_n02", r.ClusterUUID, "ClusterUUID")
	assert.Equal(t, "nsname_n03", r.NamespaceName, "NamespaceName")
	assert.Equal(t, "term_n04", r.Term, "Term")
	assert.Equal(t, "engine_n05", r.Engine, "Engine")

	// --- *int64 recommendation values ---
	assertInt64Ptr(t, 601, r.RecCPURequestMC, "RecCPURequestMC")
	assertInt64Ptr(t, 701, r.RecCPULimitMC, "RecCPULimitMC")
	assertInt64Ptr(t, 801, r.RecMemRequestKiB, "RecMemRequestKiB")
	assertInt64Ptr(t, 901, r.RecMemLimitKiB, "RecMemLimitKiB")

	// --- *int64 current values ---
	assertInt64Ptr(t, 1001, r.CurrentCPURequestMC, "CurrentCPURequestMC")
	assertInt64Ptr(t, 1101, r.CurrentCPULimitMC, "CurrentCPULimitMC")
	assertInt64Ptr(t, 1201, r.CurrentMemRequestKiB, "CurrentMemRequestKiB")
	assertInt64Ptr(t, 1301, r.CurrentMemLimitKiB, "CurrentMemLimitKiB")

	// --- *int32 variation values ---
	assertInt32Ptr(t, 1401, r.VariationCPURequestPct, "VariationCPURequestPct")
	assertInt32Ptr(t, 1501, r.VariationCPULimitPct, "VariationCPULimitPct")
	assertInt32Ptr(t, 1601, r.VariationMemRequestPct, "VariationMemRequestPct")
	assertInt32Ptr(t, 1701, r.VariationMemLimitPct, "VariationMemLimitPct")

	// --- SmallintArray ---
	require.NotNil(t, r.NotificationCodes, "NotificationCodes")
	assert.Equal(t, SmallintArray{18, 19}, r.NotificationCodes, "NotificationCodes")

	// --- *float32 ---
	require.NotNil(t, r.ConfidenceLevel, "ConfidenceLevel")
	assert.InDelta(t, float32(19.5), *r.ConfidenceLevel, 0.01, "ConfidenceLevel")

	// --- bool ---
	assert.True(t, r.Stale, "Stale")

	// --- idle fields ---
	assert.Equal(t, "idlestate_n21", r.IdleState, "IdleState")
	assertTimePtr(t, time.Date(2026, 1, 21, 10, 0, 22, 0, time.UTC), r.IdleSince, "IdleSince")
	assertIntPtr(t, 2301, r.IdleDurationDays, "IdleDurationDays")

	// --- estimated_waste_cents (non-pointer int64 in NativeNamespaceRow) ---
	assert.Equal(t, int64(2401), r.EstimatedWasteCents, "EstimatedWasteCents")

	// --- *int64 savings ---
	assertInt64Ptr(t, 2501, r.EstimatedSavingsCents, "EstimatedSavingsCents")
	assertInt64Ptr(t, 2601, r.EstimatedCPUSavingsCents, "EstimatedCPUSavingsCents")
	assertInt64Ptr(t, 2701, r.EstimatedMemSavingsCents, "EstimatedMemSavingsCents")

	// --- *time.Time ---
	assertTimePtr(t, time.Date(2026, 2, 15, 10, 0, 28, 0, time.UTC), r.MonitoringEndTime, "MonitoringEndTime")

	// --- time.Time ---
	assert.Equal(t, time.Date(2026, 3, 15, 10, 0, 29, 0, time.UTC), r.UpdatedAt.UTC(), "UpdatedAt")

	// --- explanation factors ---
	assertIntPtr(t, 3001, r.ExplDataDays, "ExplDataDays")
	assertFloat64Ptr(t, 31.5, r.ExplDecayHalfLifeHours, "ExplDecayHalfLifeHours")
	assertInt64Ptr(t, 3201, r.ExplCPUCostPctMC, "ExplCPUCostPctMC")
	assertInt64Ptr(t, 3301, r.ExplCPUPerfPctMC, "ExplCPUPerfPctMC")
	assertInt64Ptr(t, 3401, r.ExplCPUUsageP95MC, "ExplCPUUsageP95MC")
	assertInt64Ptr(t, 3501, r.ExplCPUUsageP50MC, "ExplCPUUsageP50MC")
	assertInt64Ptr(t, 3601, r.ExplCPUUsageMeanMC, "ExplCPUUsageMeanMC")
	assertInt32Ptr(t, 3701, r.ExplCPUAdaptiveMarginBP, "ExplCPUAdaptiveMarginBP")
	assertFloat64Ptr(t, 38.5, r.ExplCPUTrendSlope, "ExplCPUTrendSlope")
	assertInt64Ptr(t, 3901, r.ExplMemCostPctKiB, "ExplMemCostPctKiB")
	assertInt64Ptr(t, 4001, r.ExplMemPerfPctKiB, "ExplMemPerfPctKiB")
	assertInt64Ptr(t, 4101, r.ExplMemUsageP95KiB, "ExplMemUsageP95KiB")
	assertInt64Ptr(t, 4201, r.ExplMemUsageP50KiB, "ExplMemUsageP50KiB")
	assertInt64Ptr(t, 4301, r.ExplMemUsageMeanKiB, "ExplMemUsageMeanKiB")
	assertInt32Ptr(t, 4401, r.ExplMemAdaptiveMarginBP, "ExplMemAdaptiveMarginBP")
	assertFloat64Ptr(t, 45.5, r.ExplMemTrendSlope, "ExplMemTrendSlope")
	assertInt64Ptr(t, 4601, r.ExplOOMCountSum, "ExplOOMCountSum")
	assertBoolPtr(t, true, r.ExplOOMBumpApplied, "ExplOOMBumpApplied")
	assertBoolPtr(t, false, r.ExplCPUFloorApplied, "ExplCPUFloorApplied")
	assertBoolPtr(t, true, r.ExplMemFloorApplied, "ExplMemFloorApplied")
	assertBoolPtr(t, false, r.ExplIsIdle, "ExplIsIdle")

	// --- cluster join fields ---
	assert.Equal(t, "src_n51", r.SourceID, "SourceID")
	assert.Equal(t, "alias_n52", r.ClusterAlias, "ClusterAlias")
	assert.Equal(t, time.Date(2026, 4, 15, 10, 1, 53, 0, time.UTC), r.LastReported.UTC(), "LastReported")

	// --- field NOT in nativeNSSelect must remain zero-valued ---
	assert.Nil(t, r.PageSortText, "PageSortText should not be scanned (NoSort variant)")
}

// ---- typed assert helpers (avoid repetitive nil checks) ----

func assertInt64Ptr(t *testing.T, want int64, got *int64, field string) {
	t.Helper()
	require.NotNilf(t, got, "%s: expected %d, got nil", field, want)
	assert.Equalf(t, want, *got, "%s", field)
}

func assertInt32Ptr(t *testing.T, want int32, got *int32, field string) {
	t.Helper()
	require.NotNilf(t, got, "%s: expected %d, got nil", field, want)
	assert.Equalf(t, want, *got, "%s", field)
}

func assertIntPtr(t *testing.T, want int, got *int, field string) {
	t.Helper()
	require.NotNilf(t, got, "%s: expected %d, got nil", field, want)
	assert.Equalf(t, want, *got, "%s", field)
}

func assertFloat64Ptr(t *testing.T, want float64, got *float64, field string) {
	t.Helper()
	require.NotNilf(t, got, "%s: expected %f, got nil", field, want)
	assert.InDeltaf(t, want, *got, 0.001, "%s", field)
}

func assertBoolPtr(t *testing.T, want bool, got *bool, field string) {
	t.Helper()
	require.NotNilf(t, got, "%s: expected %v, got nil", field, want)
	assert.Equalf(t, want, *got, "%s", field)
}

func assertStringPtr(t *testing.T, want string, got *string, field string) {
	t.Helper()
	require.NotNilf(t, got, "%s: expected %q, got nil", field, want)
	assert.Equalf(t, want, *got, "%s", field)
}

func assertTimePtr(t *testing.T, want time.Time, got *time.Time, field string) {
	t.Helper()
	require.NotNilf(t, got, "%s: expected %v, got nil", field, want)
	assert.Equalf(t, want, got.UTC(), "%s", field)
}
