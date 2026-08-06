package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Reserved plugin directory / Name() values that must not be scaffolded.
var reservedNames = map[string]struct{}{
	"example":  {},
	"_example": {},
	"plugin":   {},
	"plugins":  {},
	"kruize":   {}, // special mutual-exclusivity marker; do not scaffold replacements
}

// ValidateName checks NAME against the scaffolder rules.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("NAME is required")
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("NAME %q must match [a-z][a-z0-9-]*", name)
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("NAME %q must not contain consecutive hyphens", name)
	}
	if strings.HasSuffix(name, "-") {
		return fmt.Errorf("NAME %q must not end with a hyphen", name)
	}
	if _, ok := reservedNames[name]; ok {
		return fmt.Errorf("NAME %q is reserved", name)
	}
	return nil
}

// PackageName converts a plugin id (may contain hyphens) to a Go package name.
func PackageName(name string) string {
	return strings.ReplaceAll(name, "-", "")
}

// TypeName returns the exported plugin struct name, e.g. cluster-quota → ClusterQuotaPlugin.
func TypeName(name string) string {
	parts := strings.Split(name, "-")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	b.WriteString("Plugin")
	return b.String()
}
