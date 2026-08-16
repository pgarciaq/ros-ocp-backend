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

// LoadResult is ROS container, namespace, and storage rows collected from a file, directory, or tarball.
type LoadResult struct {
	Rows            []Row
	NamespaceRows   []NamespaceRow
	PVCRows         []PVCRow
	Files           []string
	CostOnlySkipped []string
	RowsSkipped     int // unparseable data rows (bad numbers/timestamps); not cost-only files
}

// ErrNoROSFiles means the input had no ROS container, namespace, or storage CSV the parser could use.
var ErrNoROSFiles = errors.New("no ROS container, namespace, or storage CSV found")

// ErrCostOnlyInput means every candidate file was a cost-management CSV, not ROS.
type ErrCostOnlyInput struct {
	Files []string
}

func (e *ErrCostOnlyInput) Error() string {
	return fmt.Sprintf("cost-only CSV is not ROS container input (missing ROS columns): %s", strings.Join(e.Files, ", "))
}

func (r LoadResult) hasROS() bool {
	return len(r.Rows) > 0 || len(r.NamespaceRows) > 0 || len(r.PVCRows) > 0
}

func mergePart(out *LoadResult, part LoadResult) {
	out.Rows = append(out.Rows, part.Rows...)
	out.NamespaceRows = append(out.NamespaceRows, part.NamespaceRows...)
	out.PVCRows = append(out.PVCRows, part.PVCRows...)
	out.Files = append(out.Files, part.Files...)
	out.CostOnlySkipped = append(out.CostOnlySkipped, part.CostOnlySkipped...)
	out.RowsSkipped += part.RowsSkipped
}

func finishLoad(out LoadResult) (LoadResult, error) {
	if out.hasROS() {
		return out, nil
	}
	if len(out.CostOnlySkipped) > 0 {
		return LoadResult{}, &ErrCostOnlyInput{Files: out.CostOnlySkipped}
	}
	return LoadResult{}, ErrNoROSFiles
}

// Load reads ROS container, namespace, and storage CSVs from a directory, a .csv file, or a .tar.gz.
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
			var nsMiss *MissingNamespaceColumnsError
			var stMiss *MissingStorageColumnsError
			if errors.As(err, &cost) {
				out.CostOnlySkipped = append(out.CostOnlySkipped, cost.Files...)
				continue
			}
			if errors.As(err, &miss) {
				continue
			}
			if errors.As(err, &nsMiss) {
				continue
			}
			if errors.As(err, &stMiss) {
				continue
			}
			return LoadResult{}, err
		}
		mergePart(&out, part)
	}
	return finishLoad(out)
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
	return parseCSVReader(f, filepath.Base(path), kind)
}

func parseCSVReader(r io.Reader, name string, kind Kind) (LoadResult, error) {
	switch kind {
	case KindNamespace:
		rows, skipped, err := ParseNamespaceRows(r)
		if err != nil {
			return LoadResult{}, fmt.Errorf("%s: %w", name, err)
		}
		if len(rows) == 0 && skipped > 0 {
			return LoadResult{}, fmt.Errorf("%s: all %d data rows were unparseable", name, skipped)
		}
		return LoadResult{NamespaceRows: rows, Files: []string{name}, RowsSkipped: skipped}, nil
	case KindStorage:
		rows, skipped, err := ParsePVCRows(r)
		if err != nil {
			return LoadResult{}, fmt.Errorf("%s: %w", name, err)
		}
		if len(rows) == 0 && skipped > 0 {
			return LoadResult{}, fmt.Errorf("%s: all %d data rows were unparseable", name, skipped)
		}
		return LoadResult{PVCRows: rows, Files: []string{name}, RowsSkipped: skipped}, nil
	}
	rows, skipped, err := ParseRows(r)
	if err != nil {
		return LoadResult{}, fmt.Errorf("%s: %w", name, err)
	}
	if len(rows) == 0 && skipped > 0 {
		return LoadResult{}, fmt.Errorf("%s: all %d data rows were unparseable", name, skipped)
	}
	if kind == KindUnknown && len(rows) == 0 {
		return LoadResult{}, fmt.Errorf("%w: %s", ErrNoROSFiles, name)
	}
	return LoadResult{Rows: rows, Files: []string{name}, RowsSkipped: skipped}, nil
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
		part, err := parseCSVReader(tr, filepath.Base(name), kind)
		if err != nil {
			var miss *MissingROSColumnsError
			var nsMiss *MissingNamespaceColumnsError
			var stMiss *MissingStorageColumnsError
			if errors.As(err, &miss) {
				if kind == KindContainerROS {
					return LoadResult{}, err
				}
				continue
			}
			if errors.As(err, &nsMiss) {
				if kind == KindNamespace {
					return LoadResult{}, err
				}
				continue
			}
			if errors.As(err, &stMiss) {
				if kind == KindStorage {
					return LoadResult{}, err
				}
				continue
			}
			return LoadResult{}, err
		}
		if !part.hasROS() && part.RowsSkipped > 0 && (kind == KindContainerROS || kind == KindNamespace || kind == KindStorage) {
			return LoadResult{}, fmt.Errorf("%s: all %d data rows were unparseable", name, part.RowsSkipped)
		}
		mergePart(&out, part)
	}
	return finishLoad(out)
}
