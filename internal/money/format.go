package money

import "fmt"

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
