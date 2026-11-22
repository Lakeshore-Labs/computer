// Compare two synthetic theatre cue sheets mounted directly from Git tags.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/Lakeshore-Labs/computer"
	"github.com/ewhauser/gbash"
	"github.com/hairyhenderson/go-fsimpl/gitfs"
	"github.com/spf13/afero"
)

// Git exposes io/fs names relative to the tree. Computer passes rooted mount paths.
type relativeNames struct{ fs.FS }

func (r relativeNames) Open(name string) (fs.File, error) {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" {
		name = "."
	}
	return r.FS.Open(name)
}

func compare(repo, output, recipe string) error {
	mounts := []computer.Mount{{Guest: "/out", Host: output}, {Guest: "/recipe", Host: recipe, ReadOnly: true}}
	for _, edition := range []string{"rehearsal", "opening"} {
		u := &url.URL{Scheme: "git+file", Path: filepath.ToSlash(repo), Fragment: "refs/tags/" + edition}
		tree, err := gitfs.New(u)
		if err != nil {
			return err
		}
		mounts = append(mounts, computer.Mount{Guest: "/" + edition, FS: afero.FromIOFS{FS: relativeNames{tree}}, ReadOnly: true})
	}
	c, err := computer.New(computer.Config{Mounts: mounts, WorkDir: "/out"})
	if err != nil {
		return err
	}
	defer c.Close()
	const script = `set -eo pipefail
cat /rehearsal/cues.csv > first.csv
cat /opening/cues.csv > last.csv
awk -F, 'NR==FNR {if (FNR>1) {lengths[$1]=$2; colors[$1]=$3}; next}
FNR==1 {print "cue,rehearsal,opening,first_color,last_color";next}
{print $1 "," lengths[$1] "," $2 "," colors[$1] "," $3}' /rehearsal/cues.csv /opening/cues.csv > comparison.csv
awk -F, -f /recipe/cue-sheet.awk comparison.csv > cues.svg
cat comparison.csv
sha256sum /rehearsal/cues.csv /opening/cues.csv`
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: script})
	if err != nil {
		return err
	}
	fmt.Print(res.Stdout)
	if res.ExitCode != 0 {
		return fmt.Errorf("shell exit %d: %s", res.ExitCode, res.Stderr)
	}
	return nil
}

func main() {
	repo := flag.String("repo", "", "synthetic local repository")
	out := flag.String("output", "", "existing output directory")
	recipe := flag.String("recipe", "", "directory containing cue-sheet.awk")
	flag.Parse()
	if *repo == "" || *out == "" || *recipe == "" {
		log.Fatal("pass -repo, -output and -recipe")
	}
	if err := compare(*repo, *out, *recipe); err != nil {
		log.Fatal(err)
	}
}
