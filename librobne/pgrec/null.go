package pgrec

import "math"

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullInt64PodCapacity(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func pvcIdleDurationArg(days int) any {
	if days <= 0 {
		return nil
	}
	return days
}

// Float32USDCentsPtr converts nullable USD to integer cents (rounded half away from zero).
func Float32USDCentsPtr(v *float32) *int64 {
	if v == nil {
		return nil
	}
	cents := int64(math.Round(float64(*v) * 100))
	return &cents
}
