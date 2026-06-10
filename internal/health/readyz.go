package health

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// KafkaCheckFn optionally overrides Kafka readiness checks (tests).
var KafkaCheckFn func(ctx context.Context) error

// S3CheckFn optionally overrides S3 readiness checks (tests).
var S3CheckFn func(ctx context.Context) error

// ReadyzResult holds per-dependency readiness status for GET /readyz.
type ReadyzResult struct {
	OK     bool
	Checks map[string]string
}

// RunReadyzChecks evaluates database and optional Kafka/S3 dependencies.
func RunReadyzChecks(ctx context.Context, pool interface{ Ping(context.Context) error }) ReadyzResult {
	checks := map[string]string{"database": "ok"}
	allOK := true

	if pool == nil {
		return ReadyzResult{
			OK:     false,
			Checks: map[string]string{"database": "pool_uninitialized"},
		}
	}
	if err := pool.Ping(ctx); err != nil {
		return ReadyzResult{
			OK:     false,
			Checks: map[string]string{"database": "unavailable"},
		}
	}

	cfg := config.GetConfig()
	if cfg.ReadinessCheckKafka {
		if err := checkKafka(ctx); err != nil {
			checks["kafka"] = "unavailable"
			allOK = false
		} else {
			checks["kafka"] = "ok"
		}
	}
	if cfg.ReadinessCheckS3 {
		if err := checkS3(ctx); err != nil {
			checks["s3"] = "unavailable"
			allOK = false
		} else {
			checks["s3"] = "ok"
		}
	}

	return ReadyzResult{OK: allOK, Checks: checks}
}

func checkKafka(ctx context.Context) error {
	if KafkaCheckFn != nil {
		return KafkaCheckFn(ctx)
	}
	cfg := config.GetConfig()
	if cfg.KafkaBootstrapServers == "" {
		return fmt.Errorf("kafka bootstrap servers not configured")
	}

	configMap := kafka.ConfigMap{
		"bootstrap.servers": cfg.KafkaBootstrapServers,
		"socket.timeout.ms": 2000,
	}
	if cfg.KafkaSASLMechanism != "" {
		configMap["security.protocol"] = cfg.KafkaSecurityProtocol
		configMap["sasl.mechanism"] = cfg.KafkaSASLMechanism
		configMap["sasl.username"] = cfg.KafkaUsername
		configMap["sasl.password"] = cfg.KafkaPassword
		if cfg.KafkaCA != "" {
			configMap["ssl.ca.location"] = cfg.KafkaCA
		}
	}

	admin, err := kafka.NewAdminClient(&configMap)
	if err != nil {
		return fmt.Errorf("create kafka admin client: %w", err)
	}
	defer admin.Close()

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Second)
	}
	_, err = admin.GetMetadata(nil, false, int(time.Until(deadline).Milliseconds()))
	if err != nil {
		return fmt.Errorf("kafka metadata: %w", err)
	}
	return nil
}

func checkS3(ctx context.Context) error {
	if S3CheckFn != nil {
		return S3CheckFn(ctx)
	}
	cfg := config.GetConfig()
	if cfg.ReadinessS3Bucket == "" {
		return fmt.Errorf("ROS_READINESS_S3_BUCKET not configured")
	}

	awsCfg := &aws.Config{
		Region: aws.String(cfg.ReadinessS3Region),
		Credentials: credentials.NewStaticCredentials(
			cfg.ReadinessS3AccessKey,
			cfg.ReadinessS3SecretKey,
			"",
		),
	}
	if cfg.ReadinessS3Endpoint != "" {
		awsCfg.Endpoint = aws.String(cfg.ReadinessS3Endpoint)
		awsCfg.S3ForcePathStyle = aws.Bool(true)
	}

	sess, err := session.NewSession(awsCfg)
	if err != nil {
		return fmt.Errorf("create aws session: %w", err)
	}

	_, err = s3.New(sess).HeadBucketWithContext(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.ReadinessS3Bucket),
	})
	return err
}
