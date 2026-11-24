package computer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"

	"github.com/spf13/afero"

	"github.com/Lakeshore-Labs/computer/tools"
	"github.com/Lakeshore-Labs/computer/wasm"
)

// pythonBuild is the archive of a release of the python workflow, made from python/build.sh: python.wasm and lib/.
//
//go:embed python/python-wasi.tar.gz
var pythonBuild []byte

// EmbeddedPython compiles the CPython build this module carries. Its standard library is held in memory, about
// 90 MB. p is as for LoadPython, except that its host module is the one the build imports, and a zero MemoryPages
// is 8192 (512 MiB), which the build needs.
func EmbeddedPython(ctx context.Context, p wasm.Program) (*Python, error) {
	build, err := unpackPython()
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
