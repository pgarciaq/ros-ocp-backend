package config

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// SecurityEnforcement levels control whether security misconfigurations are fatal or warnings.
type SecurityEnforcement int

const (
	// SecurityEnforcementNone skips all checks (DEVELOPMENT=true).
	SecurityEnforcementNone SecurityEnforcement = iota
	// SecurityEnforcementWarn logs warnings for insecure settings (on-prem default).
	SecurityEnforcementWarn
	// SecurityEnforcementFatal fatals on insecure settings (Clowder or explicit opt-in).
	SecurityEnforcementFatal
)

// DetermineSecurityEnforcement resolves the enforcement tier based on environment:
//   - DEVELOPMENT=true → None (all checks skipped)
//   - ACG_CONFIG present (Clowder/SaaS) → Fatal (FedRAMP authorization boundary)
//   - ROS_SECURITY_ENFORCE=true → Fatal (on-prem opt-in to strict mode)
//   - Otherwise → Warn (on-prem default; insecure settings logged but non-fatal)
func DetermineSecurityEnforcement(c *Config) SecurityEnforcement {
	if c.Development {
		return SecurityEnforcementNone
	}
	if os.Getenv("ACG_CONFIG") != "" {
		return SecurityEnforcementFatal
	}
	if strings.EqualFold(os.Getenv("ROS_SECURITY_ENFORCE"), "true") {
		return SecurityEnforcementFatal
	}
	return SecurityEnforcementWarn
}

// IsDevelopment reports whether DEVELOPMENT=true (local/dev deployments).
func IsDevelopment() bool {
	return GetConfig().Development
}

// securityFinding represents a single security misconfiguration detected at startup.
type securityFinding struct {
	code    string // short identifier (e.g. "RBAC_DISABLED")
	control string // NIST 800-53 control (e.g. "AC-3")
	message string
}

// ValidateSecurityConfig enforces production security requirements at startup.
// Returns an error only when enforcement level is Fatal and a violation is found.
// At Warn level, findings are logged as warnings. At None level, checks are skipped.
func ValidateSecurityConfig() error {
	c := GetConfig()
	if c == nil {
		return nil
	}

	level := DetermineSecurityEnforcement(c)
	if level == SecurityEnforcementNone {
		return nil
	}

	// Clowder + DEVELOPMENT=true is always fatal regardless of enforcement level.
	// This prevents accidentally deploying dev mode inside the FedRAMP boundary.
	if c.Development && os.Getenv("ACG_CONFIG") != "" {
		return fmt.Errorf(
			"config: FATAL: DEVELOPMENT=true is set alongside ACG_CONFIG (Clowder); " +
				"development mode must never be enabled inside the SaaS authorization boundary [CM-6]",
		)
	}

	var findings []securityFinding

	// AC-3: RBAC must be enabled to enforce access control.
	if !c.RBACEnabled {
		findings = append(findings, securityFinding{
			code:    "RBAC_DISABLED",
			control: "AC-3",
			message: "RBAC_ENABLE=false — authorization is bypassed; all API requests are permitted without access checks",
		})
	}

	// SC-8: Database connections must use TLS for transmission confidentiality.
	if c.DBssl == "" || c.DBssl == "disable" {
		findings = append(findings, securityFinding{
			code:    "DB_TLS_DISABLED",
			control: "SC-8",
			message: fmt.Sprintf("DB_SSL=%q — database traffic is unencrypted; set to 'require', 'verify-ca', or 'verify-full'", c.DBssl),
		})
	}

	// SC-8: Kafka connections should use a secure protocol.
	if kafkaInsecure(c) {
		findings = append(findings, securityFinding{
			code:    "KAFKA_TLS_MISSING",
			control: "SC-8",
			message: fmt.Sprintf("KafkaSecurityProtocol=%q — Kafka traffic may be unencrypted; use SASL_SSL or SSL", c.KafkaSecurityProtocol),
		})
	}

	// IA-3: Development-only static tokens must not be present in production.
	if strings.TrimSpace(c.TagsDevToken) != "" {
		findings = append(findings, securityFinding{
			code:    "DEV_TOKEN_PRESENT",
			control: "IA-3",
			message: "ROS_TAGS_DEV_TOKEN is set — static development tokens must be removed in production deployments",
		})
	}

	// Existing checks (migrated from previous implementation).
	if err := validateCSVSecurity(c); err != nil {
		findings = append(findings, securityFinding{
			code:    "CSV_ALLOWLIST_EMPTY",
			control: "SI-10",
			message: err.Error(),
		})
	}
	if err := validateInternalTagsAuth(c); err != nil {
		findings = append(findings, securityFinding{
			code:    "INTERNAL_AUTH_DISABLED",
			control: "AC-3",
			message: err.Error(),
		})
	}

	if len(findings) == 0 {
		return nil
	}

	return applyEnforcement(level, findings)
}

// applyEnforcement processes findings according to the enforcement level.
func applyEnforcement(level SecurityEnforcement, findings []securityFinding) error {
	switch level {
	case SecurityEnforcementFatal:
		var msgs []string
		for _, f := range findings {
			msgs = append(msgs, fmt.Sprintf("  [%s/%s] %s", f.control, f.code, f.message))
		}
		return fmt.Errorf(
			"config: FATAL: %d security violation(s) detected in production mode:\n%s\n"+
				"Set DEVELOPMENT=true for local development or fix the configuration",
			len(findings), strings.Join(msgs, "\n"),
		)

	case SecurityEnforcementWarn:
		for _, f := range findings {
			log.Printf("SECURITY WARNING [%s/%s]: %s", f.control, f.code, f.message)
		}
		log.Printf(
			"SECURITY: %d finding(s) detected; set ROS_SECURITY_ENFORCE=true to make these fatal, "+
				"or DEVELOPMENT=true for local development",
			len(findings),
		)
		return nil

	default:
		return nil
	}
}

func kafkaInsecure(c *Config) bool {
	proto := strings.ToUpper(strings.TrimSpace(c.KafkaSecurityProtocol))
	if proto == "" || proto == "PLAINTEXT" || proto == "SASL_PLAINTEXT" {
		return true
	}
	return false
}

func validateInternalTagsAuth(c *Config) error {
	if c.Development || c.InternalTagsAuthRequired {
		return nil
	}
	return fmt.Errorf(
		"ROS_INTERNAL_TAGS_AUTH_REQUIRED must be true in non-development mode; " +
			"internal tag sync and savings recalc endpoints would be unauthenticated",
	)
}

func validateCSVSecurity(c *Config) error {
	allowed := strings.TrimSpace(c.CSVAllowedHosts)
	if allowed != "" {
		return nil
	}
	if c.Development {
		return nil
	}
	return fmt.Errorf(
		"ROS_CSV_ALLOWED_HOSTS is empty in non-development mode; " +
			"CSV URL fetches are blocked to prevent SSRF — set an explicit host allowlist or DEVELOPMENT=true for local use",
	)
}
