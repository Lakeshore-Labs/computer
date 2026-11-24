package computer

import (
	"archive/tar"
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
	"sync"

	"github.com/Lakeshore-Labs/computer/wasm"
)

// pythonBuild is the archive of a release of the python workflow, made from python/build.sh: python.wasm and lib/.
//
//go:embed python/python-wasi.tar.gz
var pythonBuild []byte

// The directory the embedded archive is unpacked into, once by EmbeddedPython. A package-level
// sync.OnceValues(unpackPython) would keep the archive in every binary, including those that never call it.
var (
	pythonOnce sync.Once
	pythonDir  string
	pythonErr  error
)

// EmbeddedPython compiles the CPython build this module carries. On first use it unpacks the build, about 90 MB,
// into computer/python-<checksum> in the user's cache directory (os.UserCacheDir), and later processes reuse it;
// a host must not mount that directory. Each call compiles the program again, so a host calls it once and shares
// the Python, as with LoadPython. p is as for LoadPython, except that its host module is the one the build
// imports, and a zero MemoryPages is 8192 (512 MiB), which the build needs.
func EmbeddedPython(ctx context.Context, p wasm.Program) (*Python, error) {
	pythonOnce.Do(func() { pythonDir, pythonErr = unpackPython() })
	if pythonErr != nil {
		return nil, fmt.Errorf("unpacking the embedded Python: %w", pythonErr)
	}
	p.HostModule = "computer"
	if p.MemoryPages == 0 {
		p.MemoryPages = 8192
	}
	return LoadPython(ctx, filepath.Join(pythonDir, "python.wasm"), filepath.Join(pythonDir, "lib"), p)
}

// unpackPython is the directory of the unpacked build, unpacking it unless it is there. It unpacks into a new
// directory and renames that into place, so the build is either whole or absent, also when two processes unpack
// at once.
func unpackPython() (_ string, err error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(pythonBuild)
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
	gz, err := gzip.NewReader(bytes.NewReader(pythonBuild))
	if err != nil {
		return "", err
	}
	r := tar.NewReader(gz)
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if !filepath.IsLocal(h.Name) {
			return "", fmt.Errorf("%q leaves the directory", h.Name)
		}
		name := filepath.Join(tmp, h.Name)
		switch h.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(name, 0o755)
		case tar.TypeReg:
			err = writeFile(name, r)
		default:
			err = fmt.Errorf("%q is neither a file nor a directory", h.Name)
		}
		if err != nil {
			return "", err
		}
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
