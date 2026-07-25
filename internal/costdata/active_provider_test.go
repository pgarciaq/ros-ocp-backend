package costdata_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

func TestActiveProvider_NilWhenNoKokuURL(t *testing.T) {
	t.Setenv("KOKU_MASU_URL", "")
	config.ResetForTest()
	_ = config.GetConfig()
	costdata.ResetActiveProviderForTest()
	t.Cleanup(func() {
		costdata.ResetActiveProviderForTest()
		config.ResetForTest()
	})

	p := costdata.ActiveProvider()
	assert.Nil(t, p)
}

func TestActiveProvider_ReturnsSingleton(t *testing.T) {
	t.Setenv("KOKU_MASU_URL", "http://fake-koku:5042")
	config.ResetForTest()
	_ = config.GetConfig()
	costdata.ResetActiveProviderForTest()
	t.Cleanup(func() {
		costdata.ResetActiveProviderForTest()
		config.ResetForTest()
	})

	p1 := costdata.ActiveProvider()
	p2 := costdata.ActiveProvider()
	require.NotNil(t, p1)
	require.NotNil(t, p2)
	assert.Same(t, p1, p2, "should return the same instance")
}

func TestSetActiveProviderForTest(t *testing.T) {
	mock := &costdata.NilCostDataProvider{}
	restore := costdata.SetActiveProviderForTest(mock)
	t.Cleanup(restore)

	p := costdata.ActiveProvider()
	require.NotNil(t, p)

	currency, err := p.GetUserCurrency(context.Background(), "1234567")
	require.NoError(t, err)
	assert.Equal(t, "USD", currency)

	rate, err := p.GetExchangeRate(context.Background(), "1234567", "USD", "EUR")
	require.NoError(t, err)
	assert.Equal(t, 1.0, rate)
}

func TestSetActiveProviderForTest_Restore(t *testing.T) {
	costdata.ResetActiveProviderForTest()
	t.Setenv("KOKU_MASU_URL", "http://fake-koku:5042")
	config.ResetForTest()
	_ = config.GetConfig()
	t.Cleanup(func() {
		costdata.ResetActiveProviderForTest()
		config.ResetForTest()
	})

	original := costdata.ActiveProvider()
	require.NotNil(t, original)

	mock := &costdata.NilCostDataProvider{}
	restore := costdata.SetActiveProviderForTest(mock)
	require.NotNil(t, costdata.ActiveProvider())

	restore()
	restored := costdata.ActiveProvider()
	require.NotNil(t, restored)
}

func TestNilCostDataProvider_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var p costdata.CostDataProvider = &costdata.NilCostDataProvider{}

	data, err := p.GetEffectiveRates(context.Background(), "org1", "cluster1", time.Now(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, "USD", data.Currency)

	currency, err := p.GetUserCurrency(context.Background(), "org1")
	require.NoError(t, err)
	assert.Equal(t, "USD", currency)

	rate, err := p.GetExchangeRate(context.Background(), "org1", "USD", "EUR")
	require.NoError(t, err)
	assert.Equal(t, 1.0, rate)
}
