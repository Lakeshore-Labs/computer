package computer

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/Lakeshore-Labs/computer/wasm"
)

// The embedded wasi-python release (see python/README.md): its interpreter, gzipped, its standard library and
// packages as one zip, and the checksums that name them.
var (
	//go:embed python/python.wasm.gz
	pythonWasm []byte
	//go:embed python/python-stdlib.zip
	pythonStdlib []byte
	//go:embed python/SHA256SUMS
	pythonSums []byte
	// Flask and what it needs beyond the release, pure Python (see python/README.md).
	//go:embed python/site.zip
	pythonSiteZip []byte
)

// The directory the embedded release is unpacked into, once by EmbeddedPython. A package-level
// sync.OnceValues(unpackPython) would keep the release in every binary, including those that never call it.
var (
	pythonOnce sync.Once
	pythonDir  string
	pythonErr  error
)

// EmbeddedPython compiles the wasi-python release this module carries: CPython with numpy, pandas, scipy,
// scikit-learn, statsmodels, matplotlib, Pillow, lxml, python-docx, python-pptx, Flask and more. On first use it unpacks
// the release, about 270 MB, into computer/python-<checksum> in the user's cache directory (os.UserCacheDir), and
// later processes reuse it; a host must not mount that directory. Each call compiles the program again, so a host
// calls it once and shares the Python, as with LoadPython.
func EmbeddedPython(ctx context.Context, p wasm.Program) (*Python, error) {
	pythonOnce.Do(func() { pythonDir, pythonErr = unpackPython() })
	if pythonErr != nil {
		return nil, fmt.Errorf("unpacking the embedded Python: %w", pythonErr)
	}
	return LoadPython(ctx, pythonDir, p)
}

// unpackPython is the directory of the unpacked release, unpacking it unless it is there. It unpacks into a new
// directory and renames that into place, so the release is either whole or absent, also when two processes unpack
// at once.
func unpackPython() (_ string, err error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(slices.Concat(pythonSums, pythonSiteZip))
	dir := filepath.Join(cache, "computer")
	build := filepath.Join(dir, "python-"+hex.EncodeToString(sum[:8]))
	if _, err := os.Stat(build); err == nil {
		return build, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(dir, "unpacking")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmp)
		}
	}()
	gz, err := gzip.NewReader(bytes.NewReader(pythonWasm))
	if err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(tmp, "python.wasm"), gz); err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(tmp, "lib", "python315.zip"), bytes.NewReader(pythonStdlib)); err != nil {
		return "", err
	}
	// matplotlib reads its data from a directory, which the release keeps as a zip inside the zip.
	stdlib, err := zip.NewReader(bytes.NewReader(pythonStdlib), int64(len(pythonStdlib)))
	if err != nil {
		return "", err
	}
	inner, err := fs.ReadFile(stdlib, "matplotlib/mpl-data.zip")
	if err != nil {
		return "", err
	}
	if err := unpackZip(filepath.Join(tmp, "mpl-data"), inner); err != nil {
		return "", err
	}
	if err := unpackZip(filepath.Join(tmp, "site"), pythonSiteZip); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, build); err != nil {
		if _, statErr := os.Stat(build); statErr == nil { // another process unpacked it first
			os.RemoveAll(tmp)
			return build, nil
		}
		return "", err
	}
	return build, nil
}

func unpackZip(dir string, data []byte) error {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	return os.CopyFS(dir, z)
}

func writeFile(name string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	return errors.Join(err, f.Close())
}
