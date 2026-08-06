package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultModule = "github.com/redhatinsights/ros-ocp-backend"

// Plan is the set of filesystem actions the scaffolder will perform.
type Plan struct {
	Opts           Options
	PluginDir      string
	PluginGoPath   string
	PluginTestPath string
	PluginsGoPath  string
	PluginGo       string
	PluginTestGo   string
	PluginsGoNext  string
}

// BuildPlan validates options and prepares file contents without writing.
func BuildPlan(repoRoot string, name, phaseStr string, priority int, traitsCSV string) (*Plan, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	phase, phaseLabel, err := parsePhase(phaseStr)
	if err != nil {
		return nil, err
	}
	if priority < 0 {
		return nil, fmt.Errorf("PRIORITY must be >= 0 (got %d)", priority)
	}
	traits, err := parseTraits(traitsCSV)
	if err != nil {
		return nil, err
	}

	pluginDir := filepath.Join(repoRoot, "internal", "plugins", name)
	if st, err := os.Stat(pluginDir); err == nil && st.IsDir() {
		return nil, fmt.Errorf("plugin directory already exists: %s", pluginDir)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	pluginsGoPath := filepath.Join(repoRoot, "internal", "plugins", "plugins.go")
	pluginsGo, err := readFile(pluginsGoPath)
	if err != nil {
		return nil, fmt.Errorf("read plugins.go: %w", err)
	}
	next, err := InsertBlankImport(pluginsGo, name)
	if err != nil {
		return nil, err
	}

	o := Options{
		Name:     name,
		Phase:    phase,
		PhaseStr: phaseLabel,
		Priority: priority,
		Traits:   traits,
		Module:   defaultModule,
	}

	return &Plan{
		Opts:           o,
		PluginDir:      pluginDir,
		PluginGoPath:   filepath.Join(pluginDir, "plugin.go"),
		PluginTestPath: filepath.Join(pluginDir, "plugin_test.go"),
		PluginsGoPath:  pluginsGoPath,
		PluginGo:       generatePluginGo(o),
		PluginTestGo:   generatePluginTestGo(o),
		PluginsGoNext:  next,
	}, nil
}

// Apply writes the plan to disk.
func (p *Plan) Apply() error {
	if err := os.MkdirAll(p.PluginDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p.PluginGoPath, []byte(p.PluginGo), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(p.PluginTestPath, []byte(p.PluginTestGo), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(p.PluginsGoPath, []byte(p.PluginsGoNext), 0o644); err != nil {
		return err
	}
	return nil
}

func (p *Plan) summarize(dryRun bool) string {
	var b string
	prefix := "Will create"
	if !dryRun {
		prefix = "Created"
	}
	b += fmt.Sprintf("%s:\n", prefix)
	b += fmt.Sprintf("  %s\n", p.PluginGoPath)
	b += fmt.Sprintf("  %s\n", p.PluginTestPath)
	b += fmt.Sprintf("Update:\n  %s (blank import %q, sorted)\n", p.PluginsGoPath, p.Opts.Name)
	b += fmt.Sprintf("\nPackage: %s\nType:    %s\nPhase:   %s (%d)\nPriority: %d\n",
		p.Opts.packageName(), p.Opts.typeName(), p.Opts.PhaseStr, p.Opts.Phase, p.Opts.Priority)
	b += "Live traits: Plugin"
	for _, t := range allOptionalTraits {
		if p.Opts.Traits[t] {
			b += ", " + t
		}
	}
	b += "\n"
	return b
}
