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

func TestFormatCentsToAmount_exactValues(t *testing.T) {
	obj := FormatCentsToAmount(199, "USD")
	assert.Equal(t, "1.99", obj.Value)
}

func TestFormatCentsToAmount_negative(t *testing.T) {
	obj := FormatCentsToAmount(-105, "USD")
	assert.Equal(t, "-1.05", obj.Value)
}

func TestFormatCentsToAmount_largeValueAvoidsFloatRounding(t *testing.T) {
	// 1999999999999 cents would lose precision with float64 division.
	obj := FormatCentsToAmount(1999999999999, "USD")
	assert.Equal(t, "19999999999.99", obj.Value)
}

func TestFormatCentsToAmount_singleCent(t *testing.T) {
	obj := FormatCentsToAmount(1, "USD")
	assert.Equal(t, "0.01", obj.Value)
}

func TestFormatCentsToAmount_negativeSingleCent(t *testing.T) {
	obj := FormatCentsToAmount(-1, "USD")
	assert.Equal(t, "-0.01", obj.Value)
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

func TestPatchUnits(t *testing.T) {
	ma := &MoneyAmount{Value: "12.34", Units: "USD"}
	PatchUnits(ma, "EUR")
	assert.Equal(t, "EUR", ma.Units)
	assert.Equal(t, "12.34", ma.Value)
}

func TestPatchUnits_Nil(t *testing.T) {
	PatchUnits(nil, "EUR")
}

func TestPatchUnits_EmptyCurrency(t *testing.T) {
	ma := &MoneyAmount{Value: "1.00", Units: "USD"}
	PatchUnits(ma, "")
	assert.Equal(t, "USD", ma.Units)
}
