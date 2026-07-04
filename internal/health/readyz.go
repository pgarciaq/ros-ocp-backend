package health

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

	if cfg.ReadinessS3Endpoint != "" {
		if err := validateS3Endpoint(cfg.ReadinessS3Endpoint); err != nil {
			return err
		}
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.ReadinessS3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.ReadinessS3AccessKey,
			cfg.ReadinessS3SecretKey,
			"",
		)),
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	s3Opts := []func(*s3.Options){
		func(o *s3.Options) {
			if cfg.ReadinessS3Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.ReadinessS3Endpoint)
				o.UsePathStyle = true
			}
		},
	}

	_, err = s3.NewFromConfig(awsCfg, s3Opts...).HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.ReadinessS3Bucket),
	})
	return err
}

// validateS3Endpoint checks that the endpoint is a valid http/https URL
// and does not point to restricted network addresses (SSRF prevention).
func validateS3Endpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("ROS_READINESS_S3_ENDPOINT: invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("ROS_READINESS_S3_ENDPOINT: scheme must be http or https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("ROS_READINESS_S3_ENDPOINT: URL must include a host")
	}
	if host == "localhost" || host == "169.254.169.254" {
		return fmt.Errorf("ROS_READINESS_S3_ENDPOINT: host %q is restricted", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("ROS_READINESS_S3_ENDPOINT: host %q resolves to restricted address", host)
		}
	}
	return nil
}
