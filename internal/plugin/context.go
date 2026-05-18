package plugin

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

// PluginContext carries shared dependencies to plugins during initialization.
type PluginContext struct {
	Pool   *pgxpool.Pool
	Config interface{} // Plugin-specific typed config, set during registration
	Logger *logrus.Entry
}
