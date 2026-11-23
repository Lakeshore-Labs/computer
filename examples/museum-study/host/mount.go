// Bring two museum image collections into one read-only Computer directory with rclone VFS.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Lakeshore-Labs/computer"
	"github.com/Lakeshore-Labs/computer/extra"
	"github.com/ewhauser/gbash"
	"github.com/rclone/rclone/backend/combine"
	_ "github.com/rclone/rclone/backend/local"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/vfs"
	"github.com/spf13/afero"
)

// rclone handles expose Readdir. The standard fs interface exposes ReadDir.
type directoryHandle struct{ vfs.Handle }

func (f directoryHandle) ReadDir(n int) ([]fs.DirEntry, error) {
	infos, err := f.Handle.Readdir(n)
	entries := make([]fs.DirEntry, len(infos))
	for i, info := range infos {
		entries[i] = fs.FileInfoToDirEntry(info)
	}
	return entries, err
}

type collectionView struct{ tree *vfs.VFS }

func (c collectionView) Open(name string) (fs.File, error) {
	clean := strings.TrimPrefix(path.Clean("/"+name), "/")
	h, err := c.tree.Open(clean)
	if err != nil {
		return nil, err
	}
	return directoryHandle{h}, nil
}

func run(collections, recipe, out string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	upstreams := []string{}
	for _, museum := range []string{"cleveland", "getty"} {
		upstreams = append(upstreams, strconv.Quote(museum+"="+filepath.Join(collections, museum)))
	}
	remote, err := combine.NewFs(ctx, "gallery", "", configmap.Simple{"upstreams": strings.Join(upstreams, " ")})
	if err != nil {
		return err
	}
	tree := vfs.New(ctx, remote, nil)
	defer tree.Shutdown()
	env, err := computer.New(computer.Config{WorkDir: "/out", Commands: extra.Commands(), Mounts: []computer.Mount{
		{Guest: "/collection", FS: afero.FromIOFS{FS: collectionView{tree}}, ReadOnly: true},
		{Guest: "/recipe", Host: recipe, ReadOnly: true},
		{Guest: "/out", Host: out},
	}})
	if err != nil {
		return err
	}
	defer env.Close()
	result, err := env.Exec(ctx, &gbash.ExecutionRequest{Script: `set -eo pipefail
ls /collection > museums.txt
cat museums.txt
for museum in cleveland getty; do
    sha256sum /collection/$museum/painting.jpg
    magick /collection/$museum/painting.jpg -thumbnail 600x460 $museum.jpg
    identify $museum.jpg
done
. /recipe/folio.sh`})
	if err != nil {
		return err
	}
	fmt.Print(result.Stdout)
	if result.ExitCode != 0 {
		return fmt.Errorf("guest exit %d: %s", result.ExitCode, result.Stderr)
	}
	return nil
}
func main() {
	if len(os.Args) != 4 {
		log.Fatal("pass collections, recipe and output directories")
	}
	if err := run(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		log.Fatal(err)
	}
}
