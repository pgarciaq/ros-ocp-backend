package money

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MoneyAmount is the structured monetary value returned by ROS API responses.
type MoneyAmount struct {
	Value string `json:"value"`
	Units string `json:"units"`
}

// FormatCentsToAmount converts integer cents to a MoneyAmount with two decimal places.
// Formatting uses integer division and remainder only (no float64) to avoid rounding errors.
func FormatCentsToAmount(cents int64, currency string) MoneyAmount {
	if currency == "" {
		currency = DefaultCurrency
	}
	sign := ""
	magnitude := uint64(cents)
	if cents < 0 {
		sign = "-"
		magnitude = uint64(-cents)
	}
	dollars := magnitude / 100
	remainder := magnitude % 100
	return MoneyAmount{
		Value: fmt.Sprintf("%s%d.%02d", sign, dollars, remainder),
		Units: currency,
	}
}

// FormatCentsToAmountPtr converts nullable cents to a MoneyAmount pointer.
func FormatCentsToAmountPtr(cents *int64, currency string) *MoneyAmount {
	if cents == nil {
		return nil
	}
	s := FormatCentsToAmount(*cents, currency)
	return &s
}

// FormatUSDToAmount converts a USD float (already in dollars) to a MoneyAmount.
func FormatUSDToAmount(usd float64, currency string) MoneyAmount {
	if currency == "" {
		currency = DefaultCurrency
	}
	return MoneyAmount{
		Value: fmt.Sprintf("%.2f", usd),
		Units: currency,
	}
}

// FormatUSDPtrToAmountPtr converts nullable float32 USD to a MoneyAmount pointer.
func FormatUSDPtrToAmountPtr(usd *float32, currency string) *MoneyAmount {
	if usd == nil {
		return nil
	}
	s := FormatUSDToAmount(float64(*usd), currency)
	return &s
}

// PatchUnits overwrites the Units field on a MoneyAmount pointer if non-nil.
func PatchUnits(m *MoneyAmount, currency string) {
	if m != nil && currency != "" {
		m.Units = currency
	}
}

// ParseCentsFromAmount parses the Value string of a MoneyAmount back to int64
// cents. Returns 0 if the MoneyAmount is nil or the value cannot be parsed.
func ParseCentsFromAmount(m *MoneyAmount) int64 {
	if m == nil || m.Value == "" {
		return 0
	}
	f, err := strconv.ParseFloat(m.Value, 64)
	if err != nil {
		return 0
	}
	return int64(math.Round(f * 100))
}

// SetAmountFromCents updates a MoneyAmount's Value from an int64 cents value.
func SetAmountFromCents(m *MoneyAmount, cents int64) {
	if m == nil {
		return
	}
	sign := ""
	magnitude := uint64(cents)
	if cents < 0 {
		sign = "-"
		magnitude = uint64(-cents)
	}
	dollars := magnitude / 100
	remainder := magnitude % 100
	m.Value = fmt.Sprintf("%s%d.%02d", sign, dollars, remainder)
}

// ConvertAmount converts a MoneyAmount's Value in-place by multiplying by rate
// and rounding to the nearest cent.
func ConvertAmount(m *MoneyAmount, rate float64) {
	if m == nil || rate == 1.0 {
		return
	}
	cents := ParseCentsFromAmount(m)
	converted := int64(math.Floor(float64(cents)*rate + 0.5))
	SetAmountFromCents(m, converted)
}

// SplitDollarsAndCents is a helper for parsing "$12.34" → 12, 34.
func SplitDollarsAndCents(value string) (dollars int64, remainder int64) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0
	}
	parts := strings.SplitN(value, ".", 2)
	dollars, _ = strconv.ParseInt(parts[0], 10, 64)
	if len(parts) > 1 {
		remainder, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	return dollars, remainder
}
