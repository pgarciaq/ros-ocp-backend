package engine

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

// #792: one shared rule set for admin term environment variables. The
// startup validator (ValidateTermEnvConfig), the term resolver
// (loadEnvTerm), and the lock report (IsTermLocked) all consult
// validateTermEnvValue, so `locked: true` can never describe a value that
// was never applied, and the rules cannot drift apart.

// termEnvFields are the admin term env var field suffixes, in
// ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>.
var termEnvFields = [3]string{"WINDOW_DAYS", "MIN_DATA_DAYS", "DECAY_HALFLIFE_HOURS"}

// validateTermEnvValue checks one admin term env value. maxWin is the
// plugin's maximum window (only consulted for WINDOW_DAYS).
func validateTermEnvValue(field, value string, maxWin int) error {
	switch field {
	case "WINDOW_DAYS":
		if n, err := strconv.Atoi(value); err != nil || n < 1 || n > maxWin {
			return fmt.Errorf("must be an integer between 1 and %d", maxWin)
		}
	case "MIN_DATA_DAYS":
		if n, err := strconv.Atoi(value); err != nil || n < 1 {
			return fmt.Errorf("must be an integer >= 1")
		}
	case "DECAY_HALFLIFE_HOURS":
		if f, err := strconv.ParseFloat(value, 64); err != nil || f < 0 {
			return fmt.Errorf("must be a number >= 0")
		}
	default:
		return fmt.Errorf("unknown field %q", field)
	}
	return nil
}

// ValidateTermEnvConfig validates every ROS_TERMS_* environment variable at
// startup (#792). Each variable must target a registered term-bearing
// plugin, a term the resolver iterates, and a known field — and its value
// must be one that would be applied. A deployment that cannot honour its
// own admin configuration must not start quietly.
//
// The valid set mirrors the RESOLVER's name space, not the plugins'
// DefaultTerms display names: LoadTermConfig loops the fixed termNames ord
// names (short/medium/long). For the VM plugin those differ from its
// DefaultTerms names (short_term/medium_term/long_term) — the
// DefaultTerms-derived spellings (e.g. ROS_TERMS_VM_SHORT_TERM_WINDOW_DAYS)
// have never been read by the resolver and are rejected here as unknown
// (the #792 review's P1: building from DefaultTerms blessed the dead
// spelling while rejecting the operative ROS_TERMS_VM_SHORT_* vars).
//
// Variables for term-less plugins (hcp since #793, quota, cluster-quota,
// snapshot) or unknown terms/fields are likewise rejected: they configure
// functionality that does not exist. This also catches typo'd variable
// names, the failure mode behind #792.
//
// Values are read with the resolver's semantics (config.EnvString):
// trimmed, empty treated as unset — validating the raw string instead
// would refuse values the resolver honours (or ignore) (#792 review's P2).
// Note the resolver reads these keys only from the environment (dynamic
// keys, no viper-bound file/flag source); if that ever changes, this
// os.Environ scan must follow.
func ValidateTermEnvConfig() error {
	type termEnvTarget struct {
		pluginName string
		termName   string
		field      string
		maxWin     int
	}
	valid := make(map[string]termEnvTarget)
	for _, tp := range plugin.ByTrait[plugin.TermProvider]() {
		maxWin := PluginMaxWindowDays(tp.Name())
		for _, termName := range termNames {
			for _, field := range termEnvFields {
				name := config.TermEnvPrefix(tp.Name(), termName) + field
				valid[name] = termEnvTarget{tp.Name(), termName, field, maxWin}
			}
		}
	}

	for _, kv := range os.Environ() {
		name, raw, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, "ROS_TERMS_") {
			continue
		}
		value := strings.TrimSpace(raw)
		if value == "" {
			continue // same semantics as config.EnvString: unset
		}
		t, ok := valid[name]
		if !ok {
			return fmt.Errorf(
				"invalid admin term configuration: unknown variable %s (it targets no term-bearing plugin, term, or field%s)",
				name, termProviderListForError())
		}
		if err := validateTermEnvValue(t.field, value, t.maxWin); err != nil {
			return fmt.Errorf(
				"invalid admin term configuration: %s=%q (%s term %q, field %s): %s",
				name, value, t.pluginName, t.termName, t.field, err)
		}
	}
	return nil
}

// termProviderNames lists the enabled term-bearing plugin names, for error
// messages.
func termProviderNames() []string {
	var names []string
	for _, tp := range plugin.ByTrait[plugin.TermProvider]() {
		names = append(names, tp.Name())
	}
	return names
}

// termProviderListForError renders the term-bearing plugin list for unknown
// variable errors — with guidance instead of an empty list when none are
// enabled (e.g. kruize exclusivity mode).
func termProviderListForError() string {
	names := termProviderNames()
	if len(names) == 0 {
		return "; no term-bearing plugins are enabled — remove the variable or enable its plugin"
	}
	return "; term-bearing plugins: " + strings.Join(names, ", ")
}
