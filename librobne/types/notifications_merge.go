package types

import "sort"

func AppendUnique(codes []int16, code int16) []int16 {
	for _, c := range codes {
		if c == code {
			return codes
		}
	}
	return append(codes, code)
}

// RemoveCode strips one code from the slice (the array_remove equivalent).
// #881: NotifNoCostData is a condition, not a history entry — the run that
// prices the row's namespace must clear it, or the warning outlives its
// cause across the database round-trip.
func RemoveCode(codes []int16, code int16) []int16 {
	out := codes[:0:0]
	for _, c := range codes {
		if c != code {
			out = append(out, c)
		}
	}
	return out
}

// MergeNotificationCodes returns sorted unique codes from existing plus new
// entries. Codes < 1 are skipped. Codes ≥ 1 are kept (including 64+; #509).
func MergeNotificationCodes(existing []int16, add ...int16) []int16 {
	var out []int16
	for _, c := range existing {
		if c < 1 {
			continue
		}
		out = AppendUnique(out, c)
	}
	for _, c := range add {
		if c < 1 {
			continue
		}
		out = AppendUnique(out, c)
	}
	return SortedNotificationCodes(out)
}

// SortedNotificationCodes ensures stable ordering for tests and API output.
func SortedNotificationCodes(codes []int16) []int16 {
	if len(codes) <= 1 {
		return codes
	}
	out := append([]int16(nil), codes...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
