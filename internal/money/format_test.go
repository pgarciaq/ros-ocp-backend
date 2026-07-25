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
	assert.Equal(t, int64(123456), obj.Cents, "Cents field should be populated")
}

func TestFormatCentsToAmount_exactValues(t *testing.T) {
	obj := FormatCentsToAmount(199, "USD")
	assert.Equal(t, "1.99", obj.Value)
}

func TestFormatCentsToAmount_negative(t *testing.T) {
	obj := FormatCentsToAmount(-105, "USD")
	assert.Equal(t, "-1.05", obj.Value)
	assert.Equal(t, int64(-105), obj.Cents, "Cents field should preserve sign")
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
	assert.Equal(t, int64(1234), obj.Cents, "Cents field should be populated from float")
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

func TestParseCentsFromAmount(t *testing.T) {
	tests := []struct {
		name  string
		input *MoneyAmount
		want  int64
	}{
		{"nil", nil, 0},
		{"empty value", &MoneyAmount{Value: ""}, 0},
		{"zero", &MoneyAmount{Value: "0.00"}, 0},
		{"positive", &MoneyAmount{Value: "12.34"}, 1234},
		{"negative", &MoneyAmount{Value: "-5.67"}, -567},
		{"single cent", &MoneyAmount{Value: "0.01"}, 1},
		{"no decimal", &MoneyAmount{Value: "100"}, 10000},
		{"large", &MoneyAmount{Value: "99999.99"}, 9999999},
		{"invalid", &MoneyAmount{Value: "abc"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ParseCentsFromAmount(tc.input))
		})
	}
}

func TestParseCentsFromAmount_usesCache(t *testing.T) {
	m := FormatCentsToAmount(4567, "USD")
	assert.Equal(t, int64(4567), ParseCentsFromAmount(&m),
		"should return cached Cents without parsing")
}

func TestParseCentsFromAmount_fallbackForHandBuilt(t *testing.T) {
	m := &MoneyAmount{Value: "45.67", Units: "USD"}
	assert.Equal(t, int64(0), m.Cents, "hand-built MoneyAmount has zero Cents")
	assert.Equal(t, int64(4567), ParseCentsFromAmount(m),
		"should fall back to string parsing when Cents is 0")
}

func TestParseCentsFromAmount_zeroValueZeroCents(t *testing.T) {
	m := FormatCentsToAmount(0, "USD")
	assert.Equal(t, int64(0), ParseCentsFromAmount(&m))
}

func TestSetAmountFromCents_updatesCentsField(t *testing.T) {
	m := &MoneyAmount{Value: "0.00", Units: "USD"}
	SetAmountFromCents(m, 999)
	assert.Equal(t, "9.99", m.Value)
	assert.Equal(t, int64(999), m.Cents)
}

func TestConvertAmount_updatesCentsField(t *testing.T) {
	m := FormatCentsToAmount(10000, "USD")
	ConvertAmount(&m, 0.92)
	assert.Equal(t, "92.00", m.Value)
	assert.Equal(t, int64(9200), m.Cents)
}

func TestSetAmountFromCents(t *testing.T) {
	tests := []struct {
		name  string
		cents int64
		want  string
	}{
		{"zero", 0, "0.00"},
		{"positive", 1234, "12.34"},
		{"negative", -567, "-5.67"},
		{"single cent", 1, "0.01"},
		{"large", 9999999, "99999.99"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &MoneyAmount{Value: "0.00", Units: "USD"}
			SetAmountFromCents(m, tc.cents)
			assert.Equal(t, tc.want, m.Value)
		})
	}
}

func TestSetAmountFromCents_Nil(t *testing.T) {
	SetAmountFromCents(nil, 100)
}

func TestConvertAmount(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		rate     float64
		expected string
	}{
		{"identity rate", "100.00", 1.0, "100.00"},
		{"USD to EUR approx", "100.00", 0.92, "92.00"},
		{"USD to GBP", "250.50", 0.79, "197.90"},
		{"small amount", "0.01", 1.5, "0.02"},
		{"negative", "-10.00", 2.0, "-20.00"},
		{"round half up", "1.00", 0.005, "0.01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &MoneyAmount{Value: tc.value, Units: "USD"}
			ConvertAmount(m, tc.rate)
			assert.Equal(t, tc.expected, m.Value)
		})
	}
}

func TestConvertAmount_Nil(t *testing.T) {
	ConvertAmount(nil, 1.5)
}

func BenchmarkParseCentsFromAmount_cached(b *testing.B) {
	m := FormatCentsToAmount(123456, "USD")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseCentsFromAmount(&m)
	}
}

func BenchmarkParseCentsFromAmount_fallback(b *testing.B) {
	m := &MoneyAmount{Value: "1234.56", Units: "USD"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseCentsFromAmount(m)
	}
}

func TestSplitDollarsAndCents(t *testing.T) {
	d, c := SplitDollarsAndCents("12.34")
	assert.Equal(t, int64(12), d)
	assert.Equal(t, int64(34), c)

	d, c = SplitDollarsAndCents("100")
	assert.Equal(t, int64(100), d)
	assert.Equal(t, int64(0), c)

	d, c = SplitDollarsAndCents("")
	assert.Equal(t, int64(0), d)
	assert.Equal(t, int64(0), c)
}
