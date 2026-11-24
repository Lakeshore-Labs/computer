package computer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"sync"

	"github.com/spf13/afero"

	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/Lakeshore-Labs/computer/wasm"
)

// pythonBuild is the archive of a release of the python workflow, made from python/build.sh: python.wasm and lib/.
//
//go:embed python/python-wasi.tar.gz
var pythonBuild []byte

// EmbeddedPython compiles the CPython build this module carries. Its standard library is unpacked into memory on
// the first call, about 90 MB held for the life of the process, and shared by every later call; each call compiles
// the program again, so a host calls it once and shares the Python, as with LoadPython. p is as for LoadPython,
// except that its host module is the one the build imports, and a zero MemoryPages is 8192 (512 MiB), which the
// build needs.
func EmbeddedPython(ctx context.Context, p wasm.Program) (*Python, error) {
	pythonOnce.Do(func() { pythonFS, pythonErr = unpackPython() })
	build, err := pythonFS, pythonErr
	if err != nil {
		return nil, fmt.Errorf("the embedded Python: %w", err)
	}
	if p.Binary, err = afero.ReadFile(build, "/python.wasm"); err != nil {
		return nil, err
	}
	p.Host, p.HostModule = tools.Host, "computer"
	if p.MemoryPages == 0 {
		p.MemoryPages = 8192
	}
	module, err := wasm.Compile(ctx, p)
	if err != nil {
		return nil, err
	}
	return &Python{module: module, lib: Mount{Guest: pythonLib, FS: afero.NewBasePathFs(build, "/lib"), ReadOnly: true}}, nil
}

// The embedded archive as an in-memory filesystem, unpacked once by EmbeddedPython. A package-level
// sync.OnceValues(unpackPython) would keep the archive in every binary, including those that never call it.
var (
	pythonOnce sync.Once
	pythonFS   afero.Fs
	pythonErr  error
)

// unpackPython is the embedded archive as an in-memory filesystem. MemMapFs tells "/a" from "a", and BasePathFs
// asks for "/a".
func unpackPython() (afero.Fs, error) {
	gz, err := gzip.NewReader(bytes.NewReader(pythonBuild))
	if err != nil {
		return nil, err
	}
	fs, r := afero.NewMemMapFs(), tar.NewReader(gz)
	for {
		h, err := r.Next()
		if err == io.EOF {
			return fs, nil
		}
		if err != nil {
			return nil, err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = fs.MkdirAll("/"+h.Name, 0o755)
		case tar.TypeReg:
			var data []byte
			if data, err = io.ReadAll(r); err == nil {
				err = afero.WriteFile(fs, "/"+h.Name, data, 0o644)
			}
		default:
			err = fmt.Errorf("%q is neither a file nor a directory", h.Name)
		}
		if err != nil {
			return nil, err
		}
	}
}
