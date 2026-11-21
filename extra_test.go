package computer

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ewhauser/gbash"
	"github.com/gen2brain/webp"

	"github.com/Lakeshore-Labs/computer/extra"
)

func newExtra(t *testing.T) *Computer {
	return newComputer(t, Config{Commands: extra.Commands()})
}

// hostHome is the host directory behind /home/user, for putting files into a computer without a shell.
func hostHome(c *Computer) string {
	for _, m := range c.Mounts() {
		if m.Guest == "/home/user" {
			return m.Host
		}
	}
	panic("no /home/user mount")
}

func writeHome(t *testing.T, c *Computer, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(hostHome(c), name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// fail runs a script that is expected to fail and returns its standard error and exit code.
func fail(t *testing.T, c *Computer, script string) (string, int) {
	t.Helper()
	res, err := c.Exec(context.Background(), &gbash.ExecutionRequest{Script: script})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("%s: expected a failure, got %q", script, res.Stdout)
	}
	return strings.TrimSpace(res.Stderr), res.ExitCode
}

// testPNG is a w by h image whose pixel at (x, y) is (x, y, 0), so that a crop or a rotation can be told by a pixel.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 0, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestExtraImageInfo(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	if got := run(t, c, "image info a.png"); got != "a.png: png 40x20" {
		t.Errorf("got %q", got)
	}
	if got := run(t, c, "image info --json a.png | jq -c '[.format, .width, .height]'"); got != `["png",40,20]` {
		t.Errorf("got %q", got)
	}
}

func TestExtraImageConvert(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	formats := []string{"jpg", "jpeg", "gif", "bmp", "tiff", "png"}
	if webp.Dynamic() != nil { // otherwise the command refuses webp, as the host's libwebp is not part of the build
		formats = append(formats, "webp")
	}
	for _, ext := range formats {
		run(t, c, "image convert a.png out."+ext)
		want := map[string]string{"jpg": "jpeg", "jpeg": "jpeg"}[ext]
		if want == "" {
			want = ext
		}
		if got := run(t, c, "image info --json out."+ext+" | jq -c '[.format, .width, .height]'"); got != `["`+want+`",40,20]` {
			t.Errorf("%s: got %q", ext, got)
		}
	}
	if msg, code := fail(t, c, "image convert a.png out.xyz"); code != 2 || !strings.HasPrefix(msg, "image: ") {
		t.Errorf("got %d %q", code, msg)
	}
}

func TestExtraImageResize(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "image resize a.png w.png --width 10")
	if got := run(t, c, "image info w.png"); got != "w.png: png 10x5" {
		t.Errorf("width only: %q", got)
	}
	run(t, c, "image resize a.png h.png --height 10")
	if got := run(t, c, "image info h.png"); got != "h.png: png 20x10" {
		t.Errorf("height only: %q", got)
	}
	run(t, c, "image resize a.png wh.png --width 7 --height 9")
	if got := run(t, c, "image info wh.png"); got != "wh.png: png 7x9" {
		t.Errorf("both: %q", got)
	}
}

func TestExtraImageThumbnailFitsWithinASquare(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "image thumbnail a.png t.png --size 10")
	if got := run(t, c, "image info t.png"); got != "t.png: png 10x5" {
		t.Errorf("got %q", got)
	}
	// An image already smaller than the square is not enlarged.
	run(t, c, "image thumbnail a.png big.png --size 100")
	if got := run(t, c, "image info big.png"); got != "big.png: png 40x20" {
		t.Errorf("got %q", got)
	}
}

func pixel(t *testing.T, c *Computer, name string, x, y int) color.NRGBA {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(hostHome(c), name))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func TestExtraImageCrop(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "image crop a.png c.png --x 5 --y 3 --width 10 --height 8")
	if got := run(t, c, "image info c.png"); got != "c.png: png 10x8" {
		t.Errorf("got %q", got)
	}
	if p := pixel(t, c, "c.png", 0, 0); p.R != 5 || p.G != 3 {
		t.Errorf("the crop starts at the wrong pixel: %v", p)
	}
	if _, code := fail(t, c, "image crop a.png c.png --x 35 --y 0 --width 10 --height 5"); code != 1 {
		t.Errorf("a crop outside the image exited %d", code)
	}
}

func TestExtraImageRotate(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "image rotate a.png r.png --degrees 90")
	if got := run(t, c, "image info r.png"); got != "r.png: png 20x40" {
		t.Errorf("got %q", got)
	}
	// Turning clockwise puts the top left pixel at the top right.
	if p := pixel(t, c, "r.png", 19, 0); p.R != 0 || p.G != 0 {
		t.Errorf("top right after 90 degrees: %v", p)
	}
	if p := pixel(t, c, "r.png", 0, 0); p.R != 0 || p.G != 19 {
		t.Errorf("top left after 90 degrees: %v", p)
	}
	run(t, c, "image rotate a.png r180.png --degrees 180")
	if p := pixel(t, c, "r180.png", 0, 0); p.R != 39 || p.G != 19 {
		t.Errorf("top left after 180 degrees: %v", p)
	}
	if _, code := fail(t, c, "image rotate a.png r.png --degrees 45"); code != 2 {
		t.Errorf("45 degrees exited %d", code)
	}
}

// hugeBMP is a bitmap whose header declares w by h pixels and which has no pixel data.
func hugeBMP(w, h int32) []byte {
	b := make([]byte, 54)
	copy(b, "BM")
	binary.LittleEndian.PutUint32(b[10:], 54)
	binary.LittleEndian.PutUint32(b[14:], 40)
	binary.LittleEndian.PutUint32(b[18:], uint32(w))
	binary.LittleEndian.PutUint32(b[22:], uint32(h))
	binary.LittleEndian.PutUint16(b[26:], 1)
	binary.LittleEndian.PutUint16(b[28:], 24)
	return b
}

func TestExtraImageRefusesHugeImages(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "huge.bmp", hugeBMP(20000, 20000))
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	if msg, code := fail(t, c, "image resize huge.bmp out.png --width 10"); code != 1 || !strings.Contains(msg, "megapixels") {
		t.Errorf("a huge input: %d %q", code, msg)
	}
	if msg, code := fail(t, c, "image resize a.png out.png --width 20000 --height 20000"); code != 1 || !strings.Contains(msg, "megapixels") {
		t.Errorf("a huge output: %d %q", code, msg)
	}
}

func TestExtraImageHelpAndUsage(t *testing.T) {
	c := newExtra(t)
	help := run(t, c, "image --help")
	for _, want := range []string{"info", "convert", "resize", "thumbnail", "crop", "rotate", "--json", "--width", "--height", "--size", "--degrees", "--x", "--y"} {
		if !strings.Contains(help, want) {
			t.Errorf("--help lacks %q", want)
		}
	}
	for _, script := range []string{"image", "image nosuch", "image info", "image resize a.png b.png", "image crop a.png b.png --width 3"} {
		if msg, code := fail(t, c, script); code != 2 || !strings.HasPrefix(msg, "image: ") {
			t.Errorf("%s: exit %d, %q", script, code, msg)
		}
	}
}

func TestExtraCommandsCannotReadTheHost(t *testing.T) {
	c := newExtra(t)
	if msg, code := fail(t, c, "image info /etc/passwd"); !strings.Contains(msg, "does not exist") {
		t.Errorf("image info /etc/passwd: %d %q", code, msg)
	}
}

const sales = "month,north,south\njan,10,4\nfeb,12,9\nmar,7,15\n"

func TestExtraChartDrawsEveryKindAsSVG(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "sales.csv", []byte(sales))
	for _, kind := range []string{"bar", "line", "pie", "scatter"} {
		y := "north,south"
		if kind == "pie" {
			y = "north"
		}
		got := run(t, c, "chart "+kind+" --x month --y "+y+" --title 'Sales "+kind+"' sales.csv --out "+kind+".svg && cat "+kind+".svg")
		if !strings.HasPrefix(got, "<svg") || !strings.Contains(got, "Sales "+kind) || !strings.Contains(got, "feb") {
			t.Errorf("%s: not an SVG with the title and the categories: %.300s", kind, got)
		}
	}
}

func TestExtraChartSeriesAreNamedByTheirColumns(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "sales.csv", []byte(sales))
	got := run(t, c, "chart bar --x month --y north,south --out a.svg sales.csv && cat a.svg")
	if !strings.Contains(got, "north") || !strings.Contains(got, "south") {
		t.Error("the legend lacks the column names")
	}
}

func TestExtraChartWritesPNGFromStandardInput(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "sales.csv", []byte(sales))
	run(t, c, "cat sales.csv | chart line --x month --y north --out l.png")
	b, err := os.ReadFile(filepath.Join(hostHome(c), "l.png"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(b)); err != nil {
		t.Errorf("not a PNG: %v", err)
	}
}

func TestExtraChartWritesSVGToStandardOutputByDefault(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "sales.csv", []byte(sales))
	if got := run(t, c, "chart bar --x month --y north sales.csv"); !strings.HasPrefix(got, "<svg") {
		t.Errorf("got %.100q", got)
	}
}

func TestExtraChartErrors(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "sales.csv", []byte(sales))
	writeHome(t, c, "bad.csv", []byte("month,north\njan,many\n"))
	for script, want := range map[string]int{
		"chart sales.csv": 2, // no kind
		"chart donut --x month --y north sales.csv":           2,
		"chart bar --y north sales.csv":                       2, // no --x
		"chart bar --x month sales.csv":                       2, // no --y
		"chart bar --x month --y north --out a.gif sales.csv": 2,
		"chart pie --x month --y north,south sales.csv":       2, // a pie has one series
		"chart bar --x nosuch --y north sales.csv":            1,
		"chart bar --x month --y north bad.csv":               1,
		"chart bar --x month --y north missing.csv":           1,
		"chart bar --x month --y north /etc/passwd":           1,
		"echo 'a,b' | chart bar --x a --y b":                  1, // no rows
	} {
		if msg, code := fail(t, c, script); code != want || !strings.HasPrefix(msg, "chart: ") {
			t.Errorf("%s: exit %d, want %d: %q", script, code, want, msg)
		}
	}
	if msg, _ := fail(t, c, "chart bar --x month --y north bad.csv"); !strings.Contains(msg, "many") {
		t.Errorf("the message does not show the bad value: %q", msg)
	}
	if msg, _ := fail(t, c, "chart bar --x month --y north /etc/passwd"); !strings.Contains(msg, "does not exist") {
		t.Errorf("the host's file was not refused: %q", msg)
	}
}

func TestExtraChartHelp(t *testing.T) {
	c := newExtra(t)
	help := run(t, c, "chart --help")
	for _, want := range []string{"bar", "line", "pie", "scatter", "--x", "--y", "--title", "--out", "INPUT"} {
		if !strings.Contains(help, want) {
			t.Errorf("--help lacks %q", want)
		}
	}
}
