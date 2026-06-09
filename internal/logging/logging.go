package logging

import (
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"             //nolint:staticcheck
	"github.com/aws/aws-sdk-go/aws/credentials" //nolint:staticcheck
	lc "github.com/redhatinsights/platform-go-middlewares/logging/cloudwatch"
	"github.com/sirupsen/logrus"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

var (
	initOnce  sync.Once
	logger    *logrus.Logger
	rootEntry *logrus.Entry
)

func initLogger() {
	logger = logrus.New()
	cfg := config.GetConfig()
	var logLevel logrus.Level

	switch cfg.LogLevel {
	case "DEBUG":
		logLevel = logrus.DebugLevel
	case "ERROR":
		logLevel = logrus.ErrorLevel
	default:
		logLevel = logrus.InfoLevel
	}

	if cfg.LogFormatter == "text" {
		logger.Formatter = &logrus.TextFormatter{}
	} else {
		logger.Formatter = &logrus.JSONFormatter{}
	}

	logger.Level = logLevel
	logger.Out = os.Stdout
	logger.ReportCaller = true

	if cfg.CwAccessKey != "" {
		cred := credentials.NewStaticCredentials(cfg.CwAccessKey, cfg.CwSecretKey, "")
		awsconf := aws.NewConfig().WithRegion(cfg.CwRegion).WithCredentials(cred)
		hook, err := lc.NewBatchingHook(cfg.CwLogGroup, cfg.CwLogStream, awsconf, 10*time.Second)
		if err != nil {
			logger.Info(err)
		}
		logger.Hooks.Add(hook)
	}
	rootEntry = logger.WithField("service", cfg.ServiceName)
}

func GetLogger() *logrus.Entry {
	initOnce.Do(func() {
		initLogger()
		rootEntry.Info("Logging initialized")
	})
	return rootEntry
}

// Set_request_details returns a request-scoped logger; it does not mutate any package-level logger.
// Callers must assign the return value (for example, log = logging.Set_request_details(...)) so subsequent
// logs include Kafka metadata. This is internal diagnostics only; HTTP response bodies and OpenAPI contracts
// are unaffected.
func Set_request_details(data types.KafkaMsg) *logrus.Entry {
	fields := logrus.Fields{
		"request_id":    data.Request_id,
		"account":       data.Metadata.Account,
		"org_id":        data.Metadata.Org_id,
		"source_id":     data.Metadata.Source_id,
		"cluster_uuid":  data.Metadata.Cluster_uuid,
		"cluster_alias": data.Metadata.Cluster_alias,
	}
	if data.Metadata.Manifest_id != "" {
		fields["manifest_id"] = data.Metadata.Manifest_id
	}
	return GetLogger().WithFields(fields)
}

// Set_request_details_recommendations returns a request-scoped logger for poller messages (same contract as
// Set_request_details: capture the return value; no HTTP/API surface impact).
func Set_request_details_recommendations(data types.RecommendationKafkaMsg) *logrus.Entry {
	return GetLogger().WithFields(logrus.Fields{
		"request_id":         data.Request_id,
		"org_id":             data.Metadata.Org_id,
		"workload_id":        data.Metadata.Workload_id,
		"max_endtime_report": data.Metadata.Max_endtime_report,
		"experiment_name":    data.Metadata.Experiment_name,
	})
}

// ForOrg returns a logger scoped to an organization and cluster — use in pipeline/engine code
// where a full KafkaMsg is not available.
func ForOrg(orgID, clusterUUID string) *logrus.Entry {
	return GetLogger().WithFields(logrus.Fields{
		"org_id":       orgID,
		"cluster_uuid": clusterUUID,
	})
}

// ForOrgOnly returns a logger scoped to an organization — use in API handlers where
// cluster context is not yet known.
func ForOrgOnly(orgID string) *logrus.Entry {
	return GetLogger().WithFields(logrus.Fields{
		"org_id": orgID,
	})
}

// ForRequest returns a logger scoped to an API request — includes org_id and request_id
// for correlation with access logs and distributed tracing.
func ForRequest(orgID, requestID string) *logrus.Entry {
	return GetLogger().WithFields(logrus.Fields{
		"org_id":     orgID,
		"request_id": requestID,
	})
}
