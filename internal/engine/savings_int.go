package engine

import (
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const (
	// MicroCentsPerDollar is the fixed-point scale for savings math (8 decimal places below dollars).
	MicroCentsPerDollar int64 = 100_000_000
	microCentsPerCent   int64 = 1_000_000
	MillicoresPerCore   int64 = 1000
	KiBPerGiB           int64 = 1024 * 1024
	bytesPerGiB         int64 = 1024 * 1024 * 1024
	HoursPerMonthInt    int64 = 730
)

// RateMicroCentsPerMCHour converts a dollar-per-core-hour rate to micro-cents per millicore-hour.
func RateMicroCentsPerMCHour(dollarsPerCoreHour float64) int64 {
	if dollarsPerCoreHour <= 0 {
		return 0
	}
	return int64(math.Round(dollarsPerCoreHour * float64(MicroCentsPerDollar) / float64(MillicoresPerCore)))
}

// RateMicroCentsPerGiBHour converts a dollar-per-GiB-hour rate to micro-cents per GiB-hour.
func RateMicroCentsPerGiBHour(dollarsPerGiBHour float64) int64 {
	if dollarsPerGiBHour <= 0 {
		return 0
	}
	return int64(math.Round(dollarsPerGiBHour * float64(MicroCentsPerDollar)))
}

// RateMicroCentsPerGiBMonth converts a dollar-per-GiB-month rate to micro-cents per GiB-month.
func RateMicroCentsPerGiBMonth(dollarsPerGiBMonth float64) int64 {
	return RateMicroCentsPerGiBHour(dollarsPerGiBMonth)
}

// RateMicroCentsPerDollarMonth converts a flat monthly dollar rate to micro-cents per month.
func RateMicroCentsPerDollarMonth(dollarsPerMonth float64) int64 {
	if dollarsPerMonth <= 0 {
		return 0
	}
	return int64(math.Round(dollarsPerMonth * float64(MicroCentsPerDollar)))
}

// EffectiveRateMicroCentsPerMCHour derives micro-cents/mc-hour from namespace aggregate cost and core-hours.
func EffectiveRateMicroCentsPerMCHour(namespaceCostUSD, requestCoreHours float64) int64 {
	if requestCoreHours <= 0 {
		return 0
	}
	return RateMicroCentsPerMCHour(clampNonNegativeUSD(namespaceCostUSD / requestCoreHours))
}

// EffectiveRateMicroCentsPerGiBHour derives micro-cents/GiB-hour from namespace aggregate cost and GiB-hours.
func EffectiveRateMicroCentsPerGiBHour(namespaceCostUSD, requestGiBHours float64) int64 {
	if requestGiBHours <= 0 {
		return 0
	}
	return RateMicroCentsPerGiBHour(clampNonNegativeUSD(namespaceCostUSD / requestGiBHours))
}

// CPUSavingsMicroCents computes CPU savings in micro-cents from a millicore delta.
func CPUSavingsMicroCents(deltaMC, rateMicroCentsPerMCHour, hoursPerMonth, replicas int64) int64 {
	if deltaMC == 0 || rateMicroCentsPerMCHour == 0 || hoursPerMonth == 0 || replicas == 0 {
		return 0
	}
	perReplica := deltaMC * rateMicroCentsPerMCHour * hoursPerMonth
	return perReplica * replicas
}

// MemSavingsMicroCentsFromKiB computes memory savings in micro-cents from a KiB delta and GiB-hour rate.
func MemSavingsMicroCentsFromKiB(deltaKiB, rateMicroCentsPerGiBHour, hoursPerMonth, replicas int64) int64 {
	if deltaKiB == 0 || rateMicroCentsPerGiBHour == 0 || hoursPerMonth == 0 || replicas == 0 {
		return 0
	}
	wholeGiB := deltaKiB / KiBPerGiB
	remKiB := deltaKiB % KiBPerGiB
	savings := wholeGiB * rateMicroCentsPerGiBHour * hoursPerMonth * replicas
	if remKiB != 0 {
		savings += remKiB * rateMicroCentsPerGiBHour * hoursPerMonth * replicas / KiBPerGiB
	}
	return savings
}

// GiBSavingsMicroCents computes savings for a whole-GiB delta with a GiB-hour rate.
func GiBSavingsMicroCents(deltaGiB, rateMicroCentsPerGiBHour, hoursPerMonth int64) int64 {
	if deltaGiB == 0 || rateMicroCentsPerGiBHour == 0 || hoursPerMonth == 0 {
		return 0
	}
	return deltaGiB * rateMicroCentsPerGiBHour * hoursPerMonth
}

// VCPUSavingsMicroCents computes savings for a whole-vCPU delta with a core-hour rate.
func VCPUSavingsMicroCents(deltaVCPU, rateMicroCentsPerMCHour, hoursPerMonth int64) int64 {
	if deltaVCPU == 0 || rateMicroCentsPerMCHour == 0 || hoursPerMonth == 0 {
		return 0
	}
	return deltaVCPU * MillicoresPerCore * rateMicroCentsPerMCHour * hoursPerMonth
}

// MemorySavingsMicroCentsFromBytes computes memory savings from freed bytes with a GiB-hour rate.
func MemorySavingsMicroCentsFromBytes(deltaBytes, rateMicroCentsPerGiBHour, hoursPerMonth int64) int64 {
	if deltaBytes == 0 || rateMicroCentsPerGiBHour == 0 || hoursPerMonth == 0 {
		return 0
	}
	wholeGiB := deltaBytes / bytesPerGiB
	remBytes := deltaBytes % bytesPerGiB
	savings := wholeGiB * rateMicroCentsPerGiBHour * hoursPerMonth
	if remBytes != 0 {
		savings += remBytes * rateMicroCentsPerGiBHour * hoursPerMonth / bytesPerGiB
	}
	return savings
}

// StorageSavingsMicroCentsFromBytes computes monthly storage savings from a byte delta.
func StorageSavingsMicroCentsFromBytes(deltaBytes, rateMicroCentsPerGiBMonth int64) int64 {
	if deltaBytes == 0 || rateMicroCentsPerGiBMonth == 0 {
		return 0
	}
	wholeGiB := deltaBytes / bytesPerGiB
	remBytes := deltaBytes % bytesPerGiB
	savings := wholeGiB * rateMicroCentsPerGiBMonth
	if remBytes != 0 {
		savings += remBytes * rateMicroCentsPerGiBMonth / bytesPerGiB
	}
	return savings
}

// MonthlyFlatSavingsMicroCents multiplies a count by a flat monthly micro-cent rate.
func MonthlyFlatSavingsMicroCents(count, rateMicroCentsPerMonth int64) int64 {
	if count == 0 || rateMicroCentsPerMonth == 0 {
		return 0
	}
	return count * rateMicroCentsPerMonth
}

// MIGFractionSavingsMicroCents computes GPU monthly savings from unused MIG slice fraction.
func MIGFractionSavingsMicroCents(gpuRateMicroCentsMonthly, totalSlices, recSlices int64) int64 {
	if gpuRateMicroCentsMonthly == 0 || totalSlices <= 0 || recSlices <= 0 || recSlices >= totalSlices {
		return 0
	}
	return gpuRateMicroCentsMonthly * (totalSlices - recSlices) / totalSlices
}

// ScaleMicroCentsByBasisPoints scales micro-cents by basis points (MarginScale = 10000 = 100%).
func ScaleMicroCentsByBasisPoints(microCents, basisPoints int64) int64 {
	if microCents == 0 || basisPoints <= 0 {
		return 0
	}
	return (microCents*basisPoints + MarginScale/2) / MarginScale
}

// MicroCentsToCents rounds micro-cents to integer cents (half away from zero).
func MicroCentsToCents(microCents int64) int64 {
	if microCents == 0 {
		return 0
	}
	if microCents > 0 {
		return (microCents + microCentsPerCent/2) / microCentsPerCent
	}
	return (microCents - microCentsPerCent/2) / microCentsPerCent
}

// MicroCentsToDollars converts micro-cents to USD rounded to two decimal places.
func MicroCentsToDollars(microCents int64) float64 {
	return money.CentsToUSD(MicroCentsToCents(microCents))
}

// QuotaTightenSavingsMicroCents computes savings from freed quota capacity (CPU mc, memory bytes, storage bytes).
func QuotaTightenSavingsMicroCents(cpuDeltaMC, memDeltaBytes, storageDeltaBytes, cpuRate, memRate, storageRate int64) int64 {
	savings := CPUSavingsMicroCents(cpuDeltaMC, cpuRate, HoursPerMonthInt, 1) +
		MemorySavingsMicroCentsFromBytes(memDeltaBytes, memRate, HoursPerMonthInt) +
		StorageSavingsMicroCentsFromBytes(storageDeltaBytes, storageRate)
	if savings < 0 {
		return 0
	}
	return savings
}

func clampNonNegativeUSD(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func replicaCountInt(rec *ContainerRec) int64 {
	if rec.DesiredReplicas > 0 {
		return rec.DesiredReplicas
	}
	return rec.PodCountAvg
}

func replicaCountForSavingsApply(rec *ContainerRec) int64 {
	replicas := replicaCountInt(rec)
	if replicas < 1 {
		return 1
	}
	return replicas
}
