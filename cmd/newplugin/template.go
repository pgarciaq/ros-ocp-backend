package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Options configures scaffold generation.
type Options struct {
	Name     string // plugin id / directory (may contain hyphens)
	Phase    int    // 1, 2, or 3
	PhaseStr string // produce|enrich|optimize
	Priority int
	Traits   map[string]bool // live traits
	Module   string          // go module path
}

func (o Options) packageName() string { return PackageName(o.Name) }
func (o Options) typeName() string    { return TypeName(o.Name) }

func (o Options) needsContext() bool {
	return o.Traits[traitCSV] || o.Traits[traitIngestHook] || o.Traits[traitEnrich] || o.Traits[traitRetention]
}

func (o Options) needsIO() bool {
	return o.Traits[traitCSV]
}

func (o Options) needsTime() bool {
	return o.Traits[traitRetention]
}

func (o Options) needsPGX() bool {
	return o.Traits[traitCSV] || o.Traits[traitIngestHook] || o.Traits[traitRetention]
}

func (o Options) needsEcho() bool {
	return o.Traits[traitAPI]
}

func (o Options) needsIngestion() bool {
	return o.Traits[traitCSV] || o.Traits[traitIngestHook]
}

func (o Options) overridePhase() bool {
	return o.Phase != 1
}

func (o Options) overridePriority() bool {
	return o.Priority != 50
}

func generatePluginGo(o Options) string {
	var b strings.Builder
	pkg := o.packageName()
	typ := o.typeName()

	fmt.Fprintf(&b, "// Package %s implements the %q recommendation plugin.\n", pkg, o.Name)
	fmt.Fprintf(&b, "//\n")
	fmt.Fprintf(&b, "// Scaffolded by: go run ./cmd/newplugin -name %s\n", o.Name)
	fmt.Fprintf(&b, "// Live traits: Plugin")
	for _, t := range allOptionalTraits {
		if o.Traits[t] {
			fmt.Fprintf(&b, ", %s", t)
		}
	}
	fmt.Fprintf(&b, ".\n")
	fmt.Fprintf(&b, "// Commented blocks below document optional traits — uncomment (and add imports) to enable.\n")
	fmt.Fprintf(&b, "// Trait reference: docs-site/plugin-reference/traits.md\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)

	b.WriteString("import (\n")
	if o.needsContext() {
		b.WriteString("\t\"context\"\n")
	}
	if o.needsIO() {
		b.WriteString("\t\"io\"\n")
	}
	if o.needsTime() {
		b.WriteString("\t\"time\"\n")
	}
	if o.needsContext() || o.needsIO() || o.needsTime() {
		b.WriteString("\n")
	}
	if o.needsPGX() {
		b.WriteString("\t\"github.com/jackc/pgx/v5/pgxpool\"\n")
	}
	if o.needsEcho() {
		b.WriteString("\t\"github.com/labstack/echo/v4\"\n")
	}
	if o.needsPGX() || o.needsEcho() {
		b.WriteString("\n")
	}
	if o.needsIngestion() {
		fmt.Fprintf(&b, "\t\"%s/internal/ingestion\"\n", o.Module)
	}
	fmt.Fprintf(&b, "\t\"%s/internal/plugin\"\n", o.Module)
	b.WriteString(")\n\n")

	fmt.Fprintf(&b, "// %s is the %q plugin.\n", typ, o.Name)
	fmt.Fprintf(&b, "type %s struct {\n\tplugin.BasePlugin\n}\n\n", typ)

	b.WriteString("func init() {\n")
	fmt.Fprintf(&b, "\tplugin.Register(&%s{})\n", typ)
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "func (p *%s) Name() string { return %q }\n\n", typ, o.Name)
	fmt.Fprintf(&b, "func (p *%s) Enabled() bool { return plugin.EnabledFor(p.Name()) }\n\n", typ)

	if o.overridePhase() {
		constName := map[int]string{1: "PhaseProduce", 2: "PhaseEnrich", 3: "PhaseOptimize"}[o.Phase]
		fmt.Fprintf(&b, "func (p *%s) Phase() int { return plugin.%s }\n\n", typ, constName)
	}
	if o.overridePriority() {
		fmt.Fprintf(&b, "func (p *%s) Priority() int { return %d }\n\n", typ, o.Priority)
	}

	// --- Live / commented trait sections ---
	writeCSVSection(&b, o, typ)
	writeIngestHookSection(&b, o, typ)
	writeAPISection(&b, o, typ)
	writeEnrichSection(&b, o, typ)
	writeRetentionSection(&b, o, typ)
	writeTermsSection(&b, o, typ)
	writeMigrationSection(&b, typ)

	return b.String()
}

func commentBlock(live bool, body string) string {
	if live {
		return body
	}
	var out strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if line == "" {
			out.WriteString("//\n")
			continue
		}
		out.WriteString("// " + line + "\n")
	}
	return out.String()
}

func writeCSVSection(b *strings.Builder, o Options, typ string) {
	live := o.Traits[traitCSV]
	header := "// --- CSVIngestor"
	if live {
		header += " ---\n"
	} else {
		header += " (uncomment to enable; add imports: context, io, pgxpool, ingestion) ---\n"
	}
	b.WriteString(header)

	body := fmt.Sprintf(`func (p *%s) SupportedCSVTypes() []string {
	// Return the CSV type identifier(s) this plugin owns (e.g. "container", "storage").
	return nil
}

func (p *%s) IngestCSV(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) ([]ingestion.MetricRow, error) {
	_ = ctx
	_ = pool
	_ = r
	_ = orgID
	_ = clusterUUID
	return nil, nil
}
`, typ, typ)
	b.WriteString(commentBlock(live, body))
	b.WriteString("\n")
}

func writeIngestHookSection(b *strings.Builder, o Options, typ string) {
	live := o.Traits[traitIngestHook]
	header := "// --- IngestHook"
	if live {
		header += " ---\n"
	} else {
		header += " (uncomment to enable; add imports: context, pgxpool, ingestion) ---\n"
	}
	b.WriteString(header)

	body := fmt.Sprintf(`func (p *%s) HookAfterCSVTypes() []string {
	// CSV types that trigger AfterIngest (e.g. "container").
	return nil
}

func (p *%s) AfterIngest(ctx context.Context, pool *pgxpool.Pool, rows []ingestion.MetricRow, orgID, clusterUUID string) error {
	_ = ctx
	_ = pool
	_ = rows
	_ = orgID
	_ = clusterUUID
	return nil
}
`, typ, typ)
	b.WriteString(commentBlock(live, body))
	b.WriteString("\n")
}

func writeAPISection(b *strings.Builder, o Options, typ string) {
	live := o.Traits[traitAPI]
	header := "// --- APIProvider"
	if live {
		header += " ---\n"
	} else {
		header += " (uncomment to enable; add import: github.com/labstack/echo/v4) ---\n"
	}
	b.WriteString(header)

	body := fmt.Sprintf(`func (p *%s) RegisterRoutes(g *echo.Group) {
	// Example:
	// g.GET("/recommendations/openshift/%s", ...)
	_ = g
}
`, typ, o.Name)
	b.WriteString(commentBlock(live, body))
	b.WriteString("\n")
}

func writeEnrichSection(b *strings.Builder, o Options, typ string) {
	live := o.Traits[traitEnrich]
	header := "// --- APIEnricher"
	if live {
		header += " ---\n"
	} else {
		header += " (uncomment to enable; add import: context) ---\n"
	}
	b.WriteString(header)

	body := fmt.Sprintf(`func (p *%s) EnrichResponse(ctx context.Context, resp interface{}) error {
	_ = ctx
	_ = resp
	return nil
}
`, typ)
	b.WriteString(commentBlock(live, body))
	b.WriteString("\n")
}

func writeRetentionSection(b *strings.Builder, o Options, typ string) {
	live := o.Traits[traitRetention]
	header := "// --- RetentionProvider"
	if live {
		header += " ---\n"
	} else {
		header += " (uncomment to enable; add imports: context, time, pgxpool) ---\n"
	}
	b.WriteString(header)

	body := fmt.Sprintf(`func (p *%s) RetentionTables() []string {
	// Table names this plugin owns for housekeeper sweeps.
	return nil
}

func (p *%s) SweepRetention(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) error {
	_ = ctx
	_ = pool
	_ = olderThan
	return nil
}
`, typ, typ)
	b.WriteString(commentBlock(live, body))
	b.WriteString("\n")
}

func writeTermsSection(b *strings.Builder, o Options, typ string) {
	live := o.Traits[traitTerms]
	header := "// --- TermProvider"
	if live {
		header += " ---\n"
	} else {
		header += " (uncomment to enable) ---\n"
	}
	b.WriteString(header)

	body := fmt.Sprintf(`func (p *%s) DefaultTerms() []plugin.TermConfig {
	return []plugin.TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

func (p *%s) MaxWindowDays() int { return 90 }
`, typ, typ)
	b.WriteString(commentBlock(live, body))
	b.WriteString("\n")
}

func writeMigrationSection(b *strings.Builder, typ string) {
	b.WriteString("// --- MigrationProvider RESERVED (not consumed by the dispatch pipeline) ---\n")
	b.WriteString("// DDL stays in repo-root migrations/ with a `-- plugin: <name>` header.\n")
	b.WriteString("// Uncomment only for documentation / future tooling experiments.\n")
	body := fmt.Sprintf(`func (p *%s) OwnedTables() []string {
	return nil
}
`, typ)
	b.WriteString(commentBlock(false, body))
}

func generatePluginTestGo(o Options) string {
	pkg := o.packageName()
	typ := o.typeName()

	var asserts []string
	asserts = append(asserts, fmt.Sprintf("\t\t_ plugin.Plugin            = (*%s)(nil)", typ))
	if o.Traits[traitCSV] {
		asserts = append(asserts, fmt.Sprintf("\t\t_ plugin.CSVIngestor       = (*%s)(nil)", typ))
	}
	if o.Traits[traitIngestHook] {
		asserts = append(asserts, fmt.Sprintf("\t\t_ plugin.IngestHook        = (*%s)(nil)", typ))
	}
	if o.Traits[traitAPI] {
		asserts = append(asserts, fmt.Sprintf("\t\t_ plugin.APIProvider       = (*%s)(nil)", typ))
	}
	if o.Traits[traitEnrich] {
		asserts = append(asserts, fmt.Sprintf("\t\t_ plugin.APIEnricher       = (*%s)(nil)", typ))
	}
	if o.Traits[traitRetention] {
		asserts = append(asserts, fmt.Sprintf("\t\t_ plugin.RetentionProvider = (*%s)(nil)", typ))
	}
	if o.Traits[traitTerms] {
		asserts = append(asserts, fmt.Sprintf("\t\t_ plugin.TermProvider      = (*%s)(nil)", typ))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import (\n")
	b.WriteString("\t\"testing\"\n\n")
	b.WriteString("\t\"github.com/stretchr/testify/assert\"\n\n")
	fmt.Fprintf(&b, "\t\"%s/internal/plugin\"\n", o.Module)
	b.WriteString(")\n\n")

	fmt.Fprintf(&b, "func Test%s_traitAssertions(t *testing.T) {\n", typ)
	b.WriteString("\tt.Parallel()\n\n")
	b.WriteString("\tvar (\n")
	b.WriteString(strings.Join(asserts, "\n"))
	b.WriteString("\n\t)\n")
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "func Test%s_name(t *testing.T) {\n", typ)
	b.WriteString("\tt.Parallel()\n\n")
	fmt.Fprintf(&b, "\tp := &%s{}\n", typ)
	fmt.Fprintf(&b, "\tassert.Equal(t, %q, p.Name())\n", o.Name)
	if o.overridePhase() {
		fmt.Fprintf(&b, "\tassert.Equal(t, %d, p.Phase())\n", o.Phase)
	}
	if o.overridePriority() {
		fmt.Fprintf(&b, "\tassert.Equal(t, %d, p.Priority())\n", o.Priority)
	} else {
		fmt.Fprintf(&b, "\tassert.Equal(t, 50, p.Priority())\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func checklist(o Options) string {
	var b strings.Builder
	b.WriteString("Next steps:\n")
	b.WriteString(fmt.Sprintf("  1. Implement domain logic in internal/plugins/%s/\n", o.Name))
	b.WriteString("  2. Uncomment optional traits as needed (CSVIngestor, IngestHook, APIEnricher, TermProvider).\n")
	b.WriteString("  3. Add migrations under migrations/ with `-- plugin: " + o.Name + "` when you own tables.\n")
	b.WriteString("  4. Document HTTP routes in openapi.json with x-plugin-required: " + strconv.Quote(o.Name) + ".\n")
	b.WriteString("  5. Enable locally: ROS_ENABLED_PLUGINS=...," + o.Name + "\n")
	b.WriteString("  6. Add docs-site/plugin-reference/" + o.Name + ".md and mkdocs.yml nav when ready.\n")
	b.WriteString("  7. IQE / UI follow-ups as needed for the new recommendation type.\n")
	b.WriteString("\nTrait reference: https://pgarciaq.github.io/ros-ocp-backend/plugin-reference/traits/\n")
	return b.String()
}
