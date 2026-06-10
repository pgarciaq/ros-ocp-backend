package utils

import (
	"net"
	"strings"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestValidateCSVDownloadURL_RequiresAllowlistInProduction(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	_ = config.GetConfig()

	_, err := validateCSVDownloadURL("https://bucket.s3.amazonaws.com/object.csv")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected blocked fetch without allowlist, got %v", err)
	}
}

func TestValidateCSVDownloadURL_AllowsWhenDevelopmentUnsetAllowlist(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	_ = config.GetConfig()

	_, err := validateCSVDownloadURL("https://bucket.s3.amazonaws.com/object.csv")
	if err != nil {
		t.Fatalf("expected dev mode allow: %v", err)
	}
}

func TestValidateCSVDownloadURL_DeniesPrivateIP(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "10.0.0.1")
	t.Setenv("ROS_CSV_DENY_PRIVATE_NETWORKS", "true")
	_ = config.GetConfig()

	_, err := validateCSVDownloadURL("https://10.0.0.1/object.csv")
	if err == nil || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("expected private IP denial, got %v", err)
	}
}

func TestValidateCSVDownloadURL_DeniesLoopback(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "127.0.0.1")
	_ = config.GetConfig()

	_, err := validateCSVDownloadURL("https://127.0.0.1/object.csv")
	if err == nil || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("expected loopback denial, got %v", err)
	}
}

func TestValidateCSVDownloadURL_DeniesLocalhostHostname(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "localhost")
	_ = config.GetConfig()

	_, err := validateCSVDownloadURL("https://localhost/object.csv")
	if err == nil || !strings.Contains(err.Error(), "restricted") {
		t.Fatalf("expected localhost denial, got %v", err)
	}
}

func TestIsRestrictedIP(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"127.0.0.1", true},
		{"8.8.8.8", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			got := isRestrictedIP(parseTestIP(t, tt.ip))
			if got != tt.blocked {
				t.Fatalf("isRestrictedIP(%q)=%v; want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

func parseTestIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("invalid test IP %q", s)
	}
	return ip
}
