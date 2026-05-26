package money

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatCentsToSavings(t *testing.T) {
	obj := FormatCentsToSavings(123456, "USD")
	assert.Equal(t, "1234.560000", obj.Value)
	assert.Equal(t, "USD", obj.Units)
}

func TestFormatCentsToSavings_zeroCents(t *testing.T) {
	obj := FormatCentsToSavings(0, "")
	assert.Equal(t, "0.000000", obj.Value)
	assert.Equal(t, DefaultCurrency, obj.Units)
}

func TestFormatUSDToSavings(t *testing.T) {
	obj := FormatUSDToSavings(12.34, "EUR")
	assert.Equal(t, "12.340000", obj.Value)
	assert.Equal(t, "EUR", obj.Units)
}
