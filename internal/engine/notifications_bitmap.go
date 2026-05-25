package engine

import "sort"

// notificationCodeBitmap supports deduplicated notification code sets for codes 1–63.
type notificationCodeBitmap uint64

func (b notificationCodeBitmap) has(code int16) bool {
	if code < 1 || code > 63 {
		return false
	}
	return b&(1<<(code-1)) != 0
}

func (b *notificationCodeBitmap) add(code int16) {
	if code < 1 || code > 63 {
		return
	}
	*b |= 1 << (code - 1)
}

func (b notificationCodeBitmap) slice() []int16 {
	if b == 0 {
		return nil
	}
	out := make([]int16, 0, 4)
	for code := int16(1); code <= 63; code++ {
		if b.has(code) {
			out = append(out, code)
		}
	}
	return out
}

func notificationCodesFromSlice(codes []int16) notificationCodeBitmap {
	var b notificationCodeBitmap
	for _, c := range codes {
		b.add(c)
	}
	return b
}

func appendUnique(codes []int16, code int16) []int16 {
	b := notificationCodesFromSlice(codes)
	if b.has(code) {
		return codes
	}
	b.add(code)
	return b.slice()
}

// mergeNotificationCodes returns sorted unique codes from existing plus new entries.
func mergeNotificationCodes(existing []int16, add ...int16) []int16 {
	b := notificationCodesFromSlice(existing)
	for _, c := range add {
		b.add(c)
	}
	return b.slice()
}

// sortedNotificationCodes ensures stable ordering for tests and API output.
func sortedNotificationCodes(codes []int16) []int16 {
	if len(codes) <= 1 {
		return codes
	}
	out := append([]int16(nil), codes...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
