package kruize

import (
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

func ptrFloat64(v float64) *float64 {
	return &v
}

// SynthDBRow holds the typed sibling-row columns feeding SynthesizeKruizeJSON
// (#599 option 2). Scanned from SELECT, excluded from JSON output (same
// pattern as StoredVariationPcts). Nullable amounts stay pointers: nil
// omits the section instead of fabricating it.
type SynthDBRow struct {
	Term   string `gorm:"column:term" json:"-"`
	Engine string `gorm:"column:engine" json:"-"`

	RecCPURequestMC      *int64 `gorm:"column:rec_cpu_request_millicores" json:"-"`
	RecCPULimitMC        *int64 `gorm:"column:rec_cpu_limit_millicores" json:"-"`
	RecMemRequestKiB     *int64 `gorm:"column:rec_memory_request_kib" json:"-"`
	RecMemLimitKiB       *int64 `gorm:"column:rec_memory_limit_kib" json:"-"`
	CurrentCPURequestMC  *int64 `gorm:"column:current_cpu_request_millicores" json:"-"`
	CurrentCPULimitMC    *int64 `gorm:"column:current_cpu_limit_millicores" json:"-"`
	CurrentMemRequestKiB *int64 `gorm:"column:current_memory_request_kib" json:"-"`
	CurrentMemLimitKiB   *int64 `gorm:"column:current_memory_limit_kib" json:"-"`

	MonitoringStartTime time.Time `gorm:"column:monitoring_start_time" json:"-"`
	MonitoringEndTime   time.Time `gorm:"column:monitoring_end_time" json:"-"`

	// NotificationCodesText carries the SMALLINT[] column as text ("{1,77}")
	// because database/sql has no int2-array target here (the model-owned
	// SmallintArray can't be imported without a cycle). Parsed by
	// ParseNotificationCodes; NULL stays nil (omits the section).
	NotificationCodesText *string `gorm:"column:notification_codes" json:"-"`
}

// StoredVariationPcts holds pre-computed per-term, per-engine variation percentages fetched
// from DB columns. Used in API response building to avoid recomputing from the JSON blob.
type StoredVariationPcts struct {
	CPUVariationShortCostPct            *float64 `gorm:"column:cpu_variation_short_cost_pct" json:"-"`
	CPUVariationShortPerformancePct     *float64 `gorm:"column:cpu_variation_short_performance_pct" json:"-"`
	CPUVariationMediumCostPct           *float64 `gorm:"column:cpu_variation_medium_cost_pct" json:"-"`
	CPUVariationMediumPerformancePct    *float64 `gorm:"column:cpu_variation_medium_performance_pct" json:"-"`
	CPUVariationLongCostPct             *float64 `gorm:"column:cpu_variation_long_cost_pct" json:"-"`
	CPUVariationLongPerformancePct      *float64 `gorm:"column:cpu_variation_long_performance_pct" json:"-"`
	MemoryVariationShortCostPct         *float64 `gorm:"column:memory_variation_short_cost_pct" json:"-"`
	MemoryVariationShortPerformancePct  *float64 `gorm:"column:memory_variation_short_performance_pct" json:"-"`
	MemoryVariationMediumCostPct        *float64 `gorm:"column:memory_variation_medium_cost_pct" json:"-"`
	MemoryVariationMediumPerformancePct *float64 `gorm:"column:memory_variation_medium_performance_pct" json:"-"`
	MemoryVariationLongCostPct          *float64 `gorm:"column:memory_variation_long_cost_pct" json:"-"`
	MemoryVariationLongPerformancePct   *float64 `gorm:"column:memory_variation_long_performance_pct" json:"-"`
}

// StoredVariationSpec maps a term/engine pair to stored DB percentage columns.
type StoredVariationSpec struct {
	Term   string
	Engine string
	CPU    func(*StoredVariationPcts) *float64
	Mem    func(*StoredVariationPcts) *float64
}

// StoredVariationSpecs enumerates all supported term/engine pairs.
var StoredVariationSpecs = []StoredVariationSpec{
	{
		Term:   ShortTerm,
		Engine: EngineCost,
		CPU:    func(s *StoredVariationPcts) *float64 { return s.CPUVariationShortCostPct },
		Mem:    func(s *StoredVariationPcts) *float64 { return s.MemoryVariationShortCostPct },
	},
	{
		Term:   ShortTerm,
		Engine: EnginePerformance,
		CPU:    func(s *StoredVariationPcts) *float64 { return s.CPUVariationShortPerformancePct },
		Mem:    func(s *StoredVariationPcts) *float64 { return s.MemoryVariationShortPerformancePct },
	},
	{
		Term:   MediumTerm,
		Engine: EngineCost,
		CPU:    func(s *StoredVariationPcts) *float64 { return s.CPUVariationMediumCostPct },
		Mem:    func(s *StoredVariationPcts) *float64 { return s.MemoryVariationMediumCostPct },
	},
	{
		Term:   MediumTerm,
		Engine: EnginePerformance,
		CPU:    func(s *StoredVariationPcts) *float64 { return s.CPUVariationMediumPerformancePct },
		Mem:    func(s *StoredVariationPcts) *float64 { return s.MemoryVariationMediumPerformancePct },
	},
	{
		Term:   LongTerm,
		Engine: EngineCost,
		CPU:    func(s *StoredVariationPcts) *float64 { return s.CPUVariationLongCostPct },
		Mem:    func(s *StoredVariationPcts) *float64 { return s.MemoryVariationLongCostPct },
	},
	{
		Term:   LongTerm,
		Engine: EnginePerformance,
		CPU:    func(s *StoredVariationPcts) *float64 { return s.CPUVariationLongPerformancePct },
		Mem:    func(s *StoredVariationPcts) *float64 { return s.MemoryVariationLongPerformancePct },
	},
}

// HasValues reports whether at least one stored percentage is non-nil.
func (s *StoredVariationPcts) HasValues() bool {
	return s.CPUVariationShortCostPct != nil ||
		s.CPUVariationShortPerformancePct != nil ||
		s.CPUVariationMediumCostPct != nil ||
		s.CPUVariationMediumPerformancePct != nil ||
		s.CPUVariationLongCostPct != nil ||
		s.CPUVariationLongPerformancePct != nil ||
		s.MemoryVariationShortCostPct != nil ||
		s.MemoryVariationShortPerformancePct != nil ||
		s.MemoryVariationMediumCostPct != nil ||
		s.MemoryVariationMediumPerformancePct != nil ||
		s.MemoryVariationLongCostPct != nil ||
		s.MemoryVariationLongPerformancePct != nil
}

// Lookup returns stored CPU and memory variation pct for term/engine keys.
func (s *StoredVariationPcts) Lookup(term, engine string) (cpu, mem *float64) {
	for _, spec := range StoredVariationSpecs {
		if spec.Term == term && spec.Engine == engine {
			return spec.CPU(s), spec.Mem(s)
		}
	}
	return nil, nil
}

// RecommendationColumnValues holds current request and per-term variation percentages for DB columns.
type RecommendationColumnValues struct {
	CPURequestCurrent    *float64
	MemoryRequestCurrent *float64

	CPUVariationShortCostPct            *float64
	CPUVariationShortPerformancePct     *float64
	CPUVariationMediumCostPct           *float64
	CPUVariationMediumPerformancePct    *float64
	CPUVariationLongCostPct             *float64
	CPUVariationLongPerformancePct      *float64
	MemoryVariationShortCostPct         *float64
	MemoryVariationShortPerformancePct  *float64
	MemoryVariationMediumCostPct        *float64
	MemoryVariationMediumPerformancePct *float64
	MemoryVariationLongCostPct          *float64
	MemoryVariationLongPerformancePct   *float64
}

// ExtractRecommendationColumnValues extracts variation percentages from Kruize payload data.
func ExtractRecommendationColumnValues(data kruizePayload.RecommendationData) RecommendationColumnValues {
	cpuReq := utils.ClampToNumeric10_4Range(data.Current.Requests.Cpu.Amount)
	memReq := utils.ClampToNumeric20_4Range(data.Current.Requests.Memory.Amount)
	recommVals := RecommendationColumnValues{
		CPURequestCurrent:    ptrFloat64(cpuReq),
		MemoryRequestCurrent: ptrFloat64(memReq),
	}
	extractTermVariations(&recommVals, data.RecommendationTerms, cpuReq, memReq)
	return recommVals
}

func extractTermVariations(recommVals *RecommendationColumnValues, terms kruizePayload.Term, cpuReq, memReq float64) {
	if e := terms.Short_term.RecommendationEngines; e != nil {
		recommVals.CPUVariationShortCostPct = ptrFloat64(utils.VariationPercentOfRequestCPU(e.Cost.Variation.Requests.Cpu.Amount, cpuReq))
		recommVals.MemoryVariationShortCostPct = ptrFloat64(utils.VariationPercentOfRequestMemoryBytesMiB(e.Cost.Variation.Requests.Memory.Amount, memReq))
		recommVals.CPUVariationShortPerformancePct = ptrFloat64(utils.VariationPercentOfRequestCPU(e.Performance.Variation.Requests.Cpu.Amount, cpuReq))
		recommVals.MemoryVariationShortPerformancePct = ptrFloat64(utils.VariationPercentOfRequestMemoryBytesMiB(e.Performance.Variation.Requests.Memory.Amount, memReq))
	}
	if e := terms.Medium_term.RecommendationEngines; e != nil {
		recommVals.CPUVariationMediumCostPct = ptrFloat64(utils.VariationPercentOfRequestCPU(e.Cost.Variation.Requests.Cpu.Amount, cpuReq))
		recommVals.MemoryVariationMediumCostPct = ptrFloat64(utils.VariationPercentOfRequestMemoryBytesMiB(e.Cost.Variation.Requests.Memory.Amount, memReq))
		recommVals.CPUVariationMediumPerformancePct = ptrFloat64(utils.VariationPercentOfRequestCPU(e.Performance.Variation.Requests.Cpu.Amount, cpuReq))
		recommVals.MemoryVariationMediumPerformancePct = ptrFloat64(utils.VariationPercentOfRequestMemoryBytesMiB(e.Performance.Variation.Requests.Memory.Amount, memReq))
	}
	if e := terms.Long_term.RecommendationEngines; e != nil {
		recommVals.CPUVariationLongCostPct = ptrFloat64(utils.VariationPercentOfRequestCPU(e.Cost.Variation.Requests.Cpu.Amount, cpuReq))
		recommVals.MemoryVariationLongCostPct = ptrFloat64(utils.VariationPercentOfRequestMemoryBytesMiB(e.Cost.Variation.Requests.Memory.Amount, memReq))
		recommVals.CPUVariationLongPerformancePct = ptrFloat64(utils.VariationPercentOfRequestCPU(e.Performance.Variation.Requests.Cpu.Amount, cpuReq))
		recommVals.MemoryVariationLongPerformancePct = ptrFloat64(utils.VariationPercentOfRequestMemoryBytesMiB(e.Performance.Variation.Requests.Memory.Amount, memReq))
	}
}
