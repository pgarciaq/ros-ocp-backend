package money

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatCentsToAmount(t *testing.T) {
	obj := FormatCentsToAmount(123456, "USD")
	assert.Equal(t, "1234.56", obj.Value)
	assert.Equal(t, "USD", obj.Units)
}

func TestFormatUSDPtrToAmountPtr(t *testing.T) {
	v := float32(12.5)
	obj := FormatUSDPtrToAmountPtr(&v, "USD")
	require.NotNil(t, obj)
	assert.Equal(t, "12.50", obj.Value)
	assert.Equal(t, "USD", obj.Units)
	assert.Nil(t, FormatUSDPtrToAmountPtr(nil, "USD"))
}

func TestFormatCentsToAmount_zeroCents(t *testing.T) {
	obj := FormatCentsToAmount(0, "")
	assert.Equal(t, "0.00", obj.Value)
	assert.Equal(t, DefaultCurrency, obj.Units)
}

func TestFormatUSDToAmount(t *testing.T) {
	obj := FormatUSDToAmount(12.34, "EUR")
	assert.Equal(t, "12.34", obj.Value)
	assert.Equal(t, "EUR", obj.Units)
}
