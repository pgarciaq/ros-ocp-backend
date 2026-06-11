package utils

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

var (
	csvAllowlistUnsetWarnOnce sync.Once
)

func validateCSVDownloadURL(rawURL string) (*url.URL, error) {
	// ADR-0146/0145: Fail-closed SSRF protection via host allowlist and private-network deny.
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse CSV URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("CSV URL scheme must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("CSV URL must include a host")
	}

	cfg := config.GetConfig()
	allowed := strings.TrimSpace(cfg.CSVAllowedHosts)
	if allowed == "" {
		if config.IsDevelopment() {
			csvAllowlistUnsetWarnOnce.Do(func() {
				log.Warn("ROS_CSV_ALLOWED_HOSTS is empty — allowing CSV fetches in development mode only")
			})
		} else {
			return nil, fmt.Errorf("CSV URL fetch blocked: ROS_CSV_ALLOWED_HOSTS is not configured")
		}
	} else {
		ok := false
		for _, h := range strings.Split(allowed, ",") {
			if strings.EqualFold(strings.TrimSpace(h), host) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("CSV URL host %q is not allowed by ROS_CSV_ALLOWED_HOSTS", host)
		}
	}

	if cfg.CSVDenyPrivateNetworks {
		if err := denyRestrictedHost(host); err != nil {
			return nil, err
		}
	}

	return u, nil
}

func denyRestrictedHost(host string) error {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return fmt.Errorf("CSV URL host %q is not allowed (restricted address)", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isRestrictedIP(ip) {
			return fmt.Errorf("CSV URL host %q is not allowed (restricted address)", host)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		if config.IsDevelopment() {
			// Development convenience: allow unresolved hostnames when allowlist matched.
			return nil
		}
		return fmt.Errorf("CSV URL host %q DNS lookup failed: %w", host, err)
	}
	for _, addr := range addrs {
		if isRestrictedIP(addr.IP) {
			return fmt.Errorf("CSV URL host %q resolves to restricted address %s", host, addr.IP)
		}
	}
	return nil
}

func isRestrictedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}
