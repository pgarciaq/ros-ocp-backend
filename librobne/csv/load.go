package csv

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LoadResult is ROS container rows collected from a file, directory, or tarball.
type LoadResult struct {
	Rows            []Row
	Files           []string
	CostOnlySkipped []string
	RowsSkipped     int // unparseable data rows (bad numbers/timestamps); not cost-only files
}

// ErrNoROSFiles means the input had no ROS container CSV the parser could use.
var ErrNoROSFiles = errors.New("no ROS container CSV found")

// ErrCostOnlyInput means every candidate file was a cost-management CSV, not ROS.
type ErrCostOnlyInput struct {
	Files []string
}

func (e *ErrCostOnlyInput) Error() string {
	return fmt.Sprintf("cost-only CSV is not ROS container input (missing ROS columns): %s", strings.Join(e.Files, ", "))
}

// Load reads ROS container CSVs from a directory, a .csv file, or a .tar.gz.
// Tar member names have a leading "./" stripped before filename matching (spec §8).
func Load(path string) (LoadResult, error) {
	st, err := os.Stat(path)
	if err != nil {
		return LoadResult{}, err
	}
	if st.IsDir() {
		return loadDir(path)
	}
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return loadTarGz(path)
	}
	return loadFile(path)
}

func loadDir(dir string) (LoadResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return LoadResult{}, err
	}
	var out LoadResult
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(filepath.Ext(name), ".csv") {
			continue
		}
		if ClassifyFilename(name) == KindOther {
			continue
		}
		part, err := loadFile(filepath.Join(dir, name))
		if err != nil {
			var cost *ErrCostOnlyInput
			var miss *MissingROSColumnsError
			if errors.As(err, &cost) {
				out.CostOnlySkipped = append(out.CostOnlySkipped, cost.Files...)
				continue
			}
			if errors.As(err, &miss) {
				continue
			}
			return LoadResult{}, err
		}
		out.Rows = append(out.Rows, part.Rows...)
		out.Files = append(out.Files, part.Files...)
		out.CostOnlySkipped = append(out.CostOnlySkipped, part.CostOnlySkipped...)
		out.RowsSkipped += part.RowsSkipped
	}
	if len(out.Rows) == 0 {
		if len(out.CostOnlySkipped) > 0 {
			return LoadResult{}, &ErrCostOnlyInput{Files: out.CostOnlySkipped}
		}
		return LoadResult{}, ErrNoROSFiles
	}
	return out, nil
}

func loadFile(path string) (LoadResult, error) {
	kind := ClassifyFilename(path)
	switch kind {
	case KindCostOnly:
		return LoadResult{}, &ErrCostOnlyInput{Files: []string{filepath.Base(path)}}
	case KindOther:
		return LoadResult{}, fmt.Errorf("%w: %s", ErrNoROSFiles, filepath.Base(path))
	}
	f, err := os.Open(filepath.Clean(path)) //nolint:gosec // G304: CLI --input path
	if err != nil {
		return LoadResult{}, err
	}
	defer func() { _ = f.Close() }()
	rows, skipped, err := ParseRows(f)
	if err != nil {
		return LoadResult{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if len(rows) == 0 && skipped > 0 {
		return LoadResult{}, fmt.Errorf("%s: all %d data rows were unparseable", filepath.Base(path), skipped)
	}
	if kind == KindUnknown && len(rows) == 0 {
		return LoadResult{}, fmt.Errorf("%w: %s", ErrNoROSFiles, filepath.Base(path))
	}
	return LoadResult{Rows: rows, Files: []string{filepath.Base(path)}, RowsSkipped: skipped}, nil
}

func loadTarGz(path string) (LoadResult, error) {
	f, err := os.Open(filepath.Clean(path)) //nolint:gosec // G304: CLI --input path
	if err != nil {
		return LoadResult{}, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return LoadResult{}, fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var out LoadResult
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return LoadResult{}, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := stripDotSlash(hdr.Name)
		if !strings.EqualFold(filepath.Ext(name), ".csv") {
			continue
		}
		kind := ClassifyFilename(name)
		switch kind {
		case KindCostOnly:
			out.CostOnlySkipped = append(out.CostOnlySkipped, filepath.Base(name))
			continue
		case KindOther:
			continue
		}
		rows, skipped, err := ParseRows(tr)
		if err != nil {
			var miss *MissingROSColumnsError
			if errors.As(err, &miss) {
				if kind == KindContainerROS {
					return LoadResult{}, fmt.Errorf("%s: %w", name, err)
				}
				continue
			}
			return LoadResult{}, fmt.Errorf("%s: %w", name, err)
		}
		if len(rows) == 0 && skipped > 0 && kind == KindContainerROS {
			return LoadResult{}, fmt.Errorf("%s: all %d data rows were unparseable", name, skipped)
		}
		out.Rows = append(out.Rows, rows...)
		out.Files = append(out.Files, filepath.Base(name))
		out.RowsSkipped += skipped
	}
	if len(out.Rows) == 0 {
		if len(out.CostOnlySkipped) > 0 {
			return LoadResult{}, &ErrCostOnlyInput{Files: out.CostOnlySkipped}
		}
		return LoadResult{}, ErrNoROSFiles
	}
	return out, nil
}
