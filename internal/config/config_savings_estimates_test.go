package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_SavingsEstimatesEnabled_DefaultTrue(t *testing.T) {
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "")
	ResetForTest()

	c := GetConfig()
	require.True(t, c.SavingsEstimatesEnabled)
}

func TestConfig_SavingsEstimatesEnabled_EnvParsing(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		want     bool
	}{
		{name: "true string", envValue: "true", setEnv: true, want: true},
		{name: "false string", envValue: "false", setEnv: true, want: false},
		{name: "zero string", envValue: "0", setEnv: true, want: false},
		{name: "one string", envValue: "1", setEnv: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setEnv {
				t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", tt.envValue)
			} else {
				t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "")
			}
			ResetForTest()

			c := GetConfig()
			require.Equal(t, tt.want, c.SavingsEstimatesEnabled)
		})
	}
}
