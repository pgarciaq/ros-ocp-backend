package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// TermEnvPrefix returns the env var prefix for admin term overrides on a recommendation type,
// e.g. "ROS_TERMS_CONTAINER_LONG_" for plugin container and term long.
func TermEnvPrefix(recommendationType, termName string) string {
	return fmt.Sprintf("ROS_TERMS_%s_%s_", strings.ToUpper(recommendationType), strings.ToUpper(termName))
}

// EnvString returns a trimmed environment variable value via viper (AutomaticEnv + BindEnv).
// Call GetConfig() first in tests so viper is initialized.
func EnvString(key string) string {
	return strings.TrimSpace(viper.GetString(key))
}
