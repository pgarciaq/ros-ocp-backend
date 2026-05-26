package money

import "fmt"

// SavingsObject is the structured savings value returned by ROS API responses.
type SavingsObject struct {
	Value string `json:"value"`
	Units string `json:"units"`
}

// FormatCentsToSavings converts integer cents to a SavingsObject with six decimal places.
func FormatCentsToSavings(cents int64, currency string) SavingsObject {
	if currency == "" {
		currency = DefaultCurrency
	}
	usd := float64(cents) / 100.0
	return SavingsObject{
		Value: fmt.Sprintf("%.6f", usd),
		Units: currency,
	}
}

// FormatCentsToSavingsPtr converts nullable cents to a SavingsObject pointer.
func FormatCentsToSavingsPtr(cents *int64, currency string) *SavingsObject {
	if cents == nil {
		return nil
	}
	s := FormatCentsToSavings(*cents, currency)
	return &s
}

// FormatUSDToSavings converts a USD float (already in dollars) to a SavingsObject.
func FormatUSDToSavings(usd float64, currency string) SavingsObject {
	if currency == "" {
		currency = DefaultCurrency
	}
	return SavingsObject{
		Value: fmt.Sprintf("%.6f", usd),
		Units: currency,
	}
}

// FormatUSDPtrToSavingsPtr converts nullable float32 USD to a SavingsObject pointer.
func FormatUSDPtrToSavingsPtr(usd *float32, currency string) *SavingsObject {
	if usd == nil {
		return nil
	}
	s := FormatUSDToSavings(float64(*usd), currency)
	return &s
}
