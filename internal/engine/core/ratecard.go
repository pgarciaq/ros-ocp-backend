package core

// RateCard is deposited cost data for Apply*. Integer only; librobne never fetches.
// Empty / nil card: skip savings, Currency unset (never default "USD").
//
// Projection hours (calendar month × 24) are an Apply* argument, not a card field.
// Observed request hours on NamespaceSpend are milli-hours.
type RateCard struct {
	// Currency is ISO 4217 cost-model unit. Copy from the source; do not invent "USD".
	Currency string
	// Distribution is "cpu" or "memory" for infra+distributed allocation. Empty means cpu.
	Distribution string
	// MarkupBasisPoints is applied only to Tier A unit prices (not A+B spend). 10% = 1000.
	MarkupBasisPoints int64

	// Tier A — unit prices (CLI / operator). Micro-cents per core-hour / GiB-hour.
	CPUMicroCentsPerCoreHour     int64
	MemMicroCentsPerGiBHour      int64
	GPUMicroCentsPerGPUHour      int64 // optional; unused on the container path
	StorageMicroCentsPerGiBMonth int64 // optional; unused on the container path

	// Tier B — observed namespace spend. Prefer B when Namespaces is non-nil
	// and the rec's namespace is present. Koku mapper fills B only.
	Namespaces map[string]NamespaceSpend
}

// NamespaceSpend is Tier B totals for one namespace (micro-cents and milli-hours).
type NamespaceSpend struct {
	CostModelCPUMicroCents int64
	CostModelMemMicroCents int64
	InfraMicroCents        int64
	DistributedMicroCents  int64
	CPURequestMilliHours   int64
	MemRequestMilliHours   int64
}

// IsEmpty reports a nil card or a card with no Tier A prices and no namespaces.
func (c *RateCard) IsEmpty() bool {
	if c == nil {
		return true
	}
	if len(c.Namespaces) > 0 {
		return false
	}
	return c.CPUMicroCentsPerCoreHour <= 0 &&
		c.MemMicroCentsPerGiBHour <= 0 &&
		c.GPUMicroCentsPerGPUHour <= 0 &&
		c.StorageMicroCentsPerGiBMonth <= 0
}

// HasTierB reports that namespace spend was deposited (even if the map is empty).
// Missing namespaces then yield NotifNoCostData rather than falling back to Tier A.
func (c *RateCard) HasTierB() bool {
	return c != nil && c.Namespaces != nil
}

// CPURateMicroCentsPerMCHour is the Tier A CPU rate in micro-cents per millicore-hour.
func (c *RateCard) CPURateMicroCentsPerMCHour() int64 {
	if c == nil || c.CPUMicroCentsPerCoreHour <= 0 {
		return 0
	}
	rate := c.CPUMicroCentsPerCoreHour / MillicoresPerCore
	return applyTierAMarkup(rate, c.MarkupBasisPoints)
}

// MemRateMicroCentsPerGiBHour is the Tier A memory rate in micro-cents per GiB-hour.
func (c *RateCard) MemRateMicroCentsPerGiBHour() int64 {
	if c == nil || c.MemMicroCentsPerGiBHour <= 0 {
		return 0
	}
	return applyTierAMarkup(c.MemMicroCentsPerGiBHour, c.MarkupBasisPoints)
}

func applyTierAMarkup(rate, markupBP int64) int64 {
	if markupBP <= 0 {
		return rate
	}
	return ScaleMicroCentsByBasisPoints(rate, MarginScale+markupBP)
}
