package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateName(t *testing.T) {
	t.Parallel()
	assert.NoError(t, ValidateName("machineset"))
	assert.NoError(t, ValidateName("foo-bar"))
	assert.Error(t, ValidateName(""))
	assert.Error(t, ValidateName("Foo"))
	assert.Error(t, ValidateName("1bad"))
	assert.Error(t, ValidateName("example"))
	assert.Error(t, ValidateName("kruize"))
	assert.Error(t, ValidateName("foo--bar"))
	assert.Error(t, ValidateName("foo-"))
}

func TestPackageAndTypeName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "clusterquota", PackageName("cluster-quota"))
	assert.Equal(t, "ClusterQuotaPlugin", TypeName("cluster-quota"))
	assert.Equal(t, "machineset", PackageName("machineset"))
	assert.Equal(t, "MachinesetPlugin", TypeName("machineset"))
}

func TestInsertBlankImportSorted(t *testing.T) {
	t.Parallel()
	in := `package plugins

import (
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/container"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/gpu"
)
`
	out, err := InsertBlankImport(in, "aaa")
	require.NoError(t, err)
	assert.Contains(t, out, `plugins/aaa"`)
	// aaa before container
	assert.Less(t, strings.Index(out, "plugins/aaa"), strings.Index(out, "plugins/container"))

	out2, err := InsertBlankImport(out, "aaa")
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(out2, `plugins/aaa"`))
}

func TestGenerateDefaultTraits(t *testing.T) {
	t.Parallel()
	traits, err := parseTraits("")
	require.NoError(t, err)
	o := Options{Name: "demo", Phase: 1, PhaseStr: "produce", Priority: 50, Traits: traits, Module: defaultModule}
	src := generatePluginGo(o)
	assert.Contains(t, src, "package demo")
	assert.Contains(t, src, "func (p *DemoPlugin) RegisterRoutes")
	assert.Contains(t, src, "func (p *DemoPlugin) RetentionTables")
	assert.Contains(t, src, "plugin.EnabledFor(p.Name())")
	// TermProvider commented by default
	assert.Contains(t, src, "// func (p *DemoPlugin) DefaultTerms()")
	assert.Contains(t, src, "MigrationProvider RESERVED")
	assert.Contains(t, src, "// func (p *DemoPlugin) OwnedTables()")
	assert.NotContains(t, src, "\nfunc (p *DemoPlugin) OwnedTables()")
}

func TestGenerateExtraTraits(t *testing.T) {
	t.Parallel()
	traits, err := parseTraits("csv,terms")
	require.NoError(t, err)
	o := Options{Name: "foo-bar", Phase: 2, PhaseStr: "enrich", Priority: 20, Traits: traits, Module: defaultModule}
	src := generatePluginGo(o)
	assert.Contains(t, src, "package foobar")
	assert.Contains(t, src, "type FooBarPlugin struct")
	assert.Contains(t, src, "return \"foo-bar\"")
	assert.Contains(t, src, "plugin.PhaseEnrich")
	assert.Contains(t, src, "return 20")
	assert.Contains(t, src, "func (p *FooBarPlugin) SupportedCSVTypes()")
	assert.Contains(t, src, "func (p *FooBarPlugin) DefaultTerms()")
	assert.Contains(t, src, "// func (p *FooBarPlugin) HookAfterCSVTypes()")
}

func TestBuildPlanDryRunAndApply(t *testing.T) {
	root := t.TempDir()
	// Minimal fake repo
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/redhatinsights/ros-ocp-backend\n"), 0o644))
	pluginsDir := filepath.Join(root, "internal", "plugins")
	require.NoError(t, os.MkdirAll(pluginsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginsDir, "plugins.go"), []byte(`package plugins

import (
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/container"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/vm"
)
`), 0o644))

	plan, err := BuildPlan(root, "zzz-demo", "produce", 50, "")
	require.NoError(t, err)
	_, err = os.Stat(plan.PluginDir)
	assert.True(t, os.IsNotExist(err))
	require.NoError(t, plan.Apply())
	_, err = os.Stat(plan.PluginGoPath)
	require.NoError(t, err)
	_, err = os.Stat(plan.PluginTestPath)
	require.NoError(t, err)

	updated, err := os.ReadFile(plan.PluginsGoPath)
	require.NoError(t, err)
	assert.Contains(t, string(updated), `plugins/zzz-demo"`)
	// sorted: container, vm, zzz-demo
	s := string(updated)
	assert.Less(t, strings.Index(s, "container"), strings.Index(s, "zzz-demo"))

	_, err = BuildPlan(root, "zzz-demo", "produce", 50, "")
	assert.Error(t, err) // conflict
}
