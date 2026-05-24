package costdata

const DefaultCurrency = "USD"

// ResolveCurrency returns the currency from cost data, defaulting to USD when unset.
func ResolveCurrency(data *ClusterCostData) string {
	if data == nil || data.Currency == "" {
		return DefaultCurrency
	}
	return data.Currency
}
