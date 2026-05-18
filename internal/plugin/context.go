package plugin

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

// PluginContext is reserved for future plugin lifecycle wiring (typed config injection, startup hooks).
// The dispatch layer does not pass PluginContext yet; plugins use config.GetConfig() and logging like other packages.
type PluginContext struct {
	Pool   *pgxpool.Pool
	Config interface{} // Plugin-specific typed config, set during registration
	Logger *logrus.Entry
}
