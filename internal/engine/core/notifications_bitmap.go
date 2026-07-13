package core

import "sort"

// NotificationCodeBitmap supports deduplicated notification code sets for codes 1–63.
type NotificationCodeBitmap uint64

func (b NotificationCodeBitmap) Has(code int16) bool {
	if code < 1 || code > 63 {
		return false
	}
	return b&(1<<(code-1)) != 0
}

func (b *NotificationCodeBitmap) Add(code int16) {
	if code < 1 || code > 63 {
		return
	}
	*b |= 1 << (code - 1)
}

func (b NotificationCodeBitmap) Slice() []int16 {
	if b == 0 {
		return nil
	}
	out := make([]int16, 0, 4)
	for code := int16(1); code <= 63; code++ {
		if b.Has(code) {
			out = append(out, code)
		}
	}
	return out
}

func NotificationCodesFromSlice(codes []int16) NotificationCodeBitmap {
	var b NotificationCodeBitmap
	for _, c := range codes {
		b.Add(c)
	}
	return b
}

func AppendUnique(codes []int16, code int16) []int16 {
	b := NotificationCodesFromSlice(codes)
	if b.Has(code) {
		return codes
	}
	b.Add(code)
	return b.Slice()
}

// MergeNotificationCodes returns sorted unique codes from existing plus new entries.
func MergeNotificationCodes(existing []int16, add ...int16) []int16 {
	b := NotificationCodesFromSlice(existing)
	for _, c := range add {
		b.Add(c)
	}
	return b.Slice()
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
