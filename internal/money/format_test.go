package money

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatCentsToSavings(t *testing.T) {
	obj := FormatCentsToSavings(123456, "USD")
	assert.Equal(t, "1234.560000", obj.Value)
	assert.Equal(t, "USD", obj.Units)
}

func TestFormatUSDPtrToSavingsPtr(t *testing.T) {
	v := float32(12.5)
	obj := FormatUSDPtrToSavingsPtr(&v, "USD")
	require.NotNil(t, obj)
	assert.Equal(t, "12.500000", obj.Value)
	assert.Equal(t, "USD", obj.Units)
	assert.Nil(t, FormatUSDPtrToSavingsPtr(nil, "USD"))
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
