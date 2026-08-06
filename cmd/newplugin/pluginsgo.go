package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var blankImportRE = regexp.MustCompile(`(?m)^\t_ "github\.com/redhatinsights/ros-ocp-backend/internal/plugins/([^"]+)"$`)

// InsertBlankImport adds a blank import for pluginDir into plugins.go content,
// keeping imports sorted alphabetically by directory name. Idempotent.
func InsertBlankImport(content, pluginDir string) (string, error) {
	importPath := fmt.Sprintf(`_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/%s"`, pluginDir)
	if strings.Contains(content, importPath) {
		return content, nil
	}

	matches := blankImportRE.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return "", fmt.Errorf("no blank plugin imports found in plugins.go")
	}

	dirs := make([]string, 0, len(matches)+1)
	for _, m := range matches {
		dirs = append(dirs, m[1])
	}
	dirs = append(dirs, pluginDir)
	sort.Strings(dirs)

	var block strings.Builder
	block.WriteString("import (\n")
	for _, d := range dirs {
		fmt.Fprintf(&block, "\t_ \"github.com/redhatinsights/ros-ocp-backend/internal/plugins/%s\"\n", d)
	}
	block.WriteString(")\n")

	// Replace the existing import (...) block that contains blank plugin imports.
	start := strings.Index(content, "import (")
	if start < 0 {
		return "", fmt.Errorf("import block not found in plugins.go")
	}
	end := strings.Index(content[start:], ")")
	if end < 0 {
		return "", fmt.Errorf("import block end not found in plugins.go")
	}
	end = start + end + 1
	// Include trailing newline after )
	if end < len(content) && content[end] == '\n' {
		end++
	}

	return content[:start] + block.String() + content[end:], nil
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
