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

	"github.com/Lakeshore-Labs/computer/wasm"
)

// pythonBuild is the release archive the python workflow makes from python/build.sh: python.wasm and lib/.
//
//go:embed python/python-wasi.tar.gz
var pythonBuild []byte

// EmbeddedPython compiles the CPython build this module carries. Its standard library is unpacked once into a
// directory under dir named for the build, so a process given the same dir later reuses it; dir must be outside
// every mount, as LoadPython's lib must. p is as for LoadPython, except that its host module is the one the build
// imports, and a zero MemoryPages is 8192 (512 MiB), which the build needs.
func EmbeddedPython(ctx context.Context, dir string, p wasm.Program) (*Python, error) {
	sum := sha256.Sum256(pythonBuild)
	build := filepath.Join(dir, "python-"+hex.EncodeToString(sum[:8]))
	if _, err := os.Stat(build); errors.Is(err, fs.ErrNotExist) {
		if err := unpackPython(dir, build); err != nil {
			return nil, fmt.Errorf("unpacking the embedded Python: %w", err)
		}
	} else if err != nil {
		return nil, err
	}
	p.HostModule = "computer"
	if p.MemoryPages == 0 {
		p.MemoryPages = 8192
	}
	return LoadPython(ctx, filepath.Join(build, "python.wasm"), filepath.Join(build, "lib"), p)
}

// unpackPython unpacks the embedded build into a new directory in dir and renames it to build, so that build is
// either whole or absent, also when two processes unpack at once.
func unpackPython(dir, build string) (err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(dir, "unpacking")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmp)
		}
	}()
	gz, err := gzip.NewReader(bytes.NewReader(pythonBuild))
	if err != nil {
		return err
	}
	r := tar.NewReader(gz)
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !filepath.IsLocal(h.Name) {
			return fmt.Errorf("%q leaves the directory", h.Name)
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
			return err
		}
	}
	if err := os.Rename(tmp, build); err != nil {
		if _, statErr := os.Stat(build); statErr == nil { // another process unpacked it first
			return os.RemoveAll(tmp)
		}
		return err
	}
	return nil
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
