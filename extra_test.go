package computer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ewhauser/gbash"

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

// size is the dimensions that identify reports for name, as "WxH".
func size(t *testing.T, c *Computer, name string) string {
	t.Helper()
	f := strings.Fields(run(t, c, "identify "+name))
	return f[len(f)-1]
}

func TestExtraMagickResizeGeometries(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	for geometry, want := range map[string]string{
		"10":      "10x5",  // width, keeping the aspect ratio
		"x10":     "20x10", // height, keeping the aspect ratio
		"10x10":   "10x5",  // fit within the box
		"7x9!":    "7x9",   // exactly, ignoring the aspect ratio
		"50%":     "20x10", // both sides scaled
		"100x50>": "40x20", // only shrinks: the image is smaller than the box
		"10x10>":  "10x5",  // only shrinks: the image is larger than the box
		"10x10^":  "20x10", // fill the box, keeping the aspect ratio
	} {
		run(t, c, "magick a.png -resize '"+geometry+"' out.png")
		if got := size(t, c, "out.png"); got != want {
			t.Errorf("-resize %s: got %s, want %s", geometry, got, want)
		}
	}
}

func TestExtraMagickThumbnailIsResize(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "magick a.png -thumbnail 10x10 t.png")
	if got := size(t, c, "t.png"); got != "10x5" {
		t.Errorf("got %s", got)
	}
}

func TestExtraMagickCrop(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "magick a.png -crop 10x8+5+3 c.png")
	if got := size(t, c, "c.png"); got != "10x8" {
		t.Errorf("got %s", got)
	}
	if p := pixel(t, c, "c.png", 0, 0); p.R != 5 || p.G != 3 {
		t.Errorf("the crop starts at the wrong pixel: %v", p)
	}
	run(t, c, "magick a.png -crop 10x8 d.png")
	if p := pixel(t, c, "d.png", 0, 0); p.R != 0 || p.G != 0 {
		t.Errorf("without offsets the crop starts at the corner: %v", p)
	}
}

func TestExtraMagickRotate(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "magick a.png -rotate 90 r.png")
	if got := size(t, c, "r.png"); got != "20x40" {
		t.Errorf("got %s", got)
	}
	// Turning clockwise puts the top left pixel at the top right.
	if p := pixel(t, c, "r.png", 19, 0); p.R != 0 || p.G != 0 {
		t.Errorf("top right after 90 degrees: %v", p)
	}
	run(t, c, "magick a.png -rotate -90 l.png")
	if p := pixel(t, c, "l.png", 0, 39); p.R != 0 || p.G != 0 {
		t.Errorf("bottom left after -90 degrees: %v", p)
	}
	msg, code := fail(t, c, "magick a.png -rotate 45 r.png")
	if code != 2 || !strings.Contains(msg, "multiples of 90") {
		t.Errorf("45 degrees: exit %d, %q", code, msg)
	}
}

func TestExtraMagickFlipAndFlop(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "magick a.png -flip v.png")
	if p := pixel(t, c, "v.png", 0, 0); p.R != 0 || p.G != 19 {
		t.Errorf("top left after -flip: %v", p)
	}
	run(t, c, "magick a.png -flop h.png")
	if p := pixel(t, c, "h.png", 0, 0); p.R != 39 || p.G != 0 {
		t.Errorf("top left after -flop: %v", p)
	}
}

func TestExtraMagickAppliesOptionsInOrder(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "magick a.png -crop 20x20+0+0 -resize 10x10 one.png")
	run(t, c, "magick a.png -resize 10x10 -crop 20x20+0+0 two.png")
	if a, b := size(t, c, "one.png"), size(t, c, "two.png"); a != "10x10" || b != "10x5" {
		t.Errorf("crop then resize gave %s, resize then crop gave %s", a, b)
	}
}

func TestExtraMagickWritesTheFormatOfTheOutputName(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	for ext, want := range map[string]string{"jpg": "JPEG", "jpeg": "JPEG", "gif": "GIF", "bmp": "BMP", "tiff": "TIFF", "png": "PNG"} {
		run(t, c, "magick a.png out."+ext)
		if got := run(t, c, "identify out."+ext); got != "out."+ext+" "+want+" 40x20" {
			t.Errorf("%s: got %q", ext, got)
		}
	}
	if msg, code := fail(t, c, "magick a.png out.xyz"); code != 2 || !strings.HasPrefix(msg, "magick: ") {
		t.Errorf("got %d %q", code, msg)
	}
}

func TestExtraConvertIsMagick(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "convert a.png -resize 10 c.png")
	if got := size(t, c, "c.png"); got != "10x5" {
		t.Errorf("convert: got %s", got)
	}
	run(t, c, "magick convert a.png -resize 10 m.png")
	if got := size(t, c, "m.png"); got != "10x5" {
		t.Errorf("magick convert: got %s", got)
	}
}

func TestExtraIgnoredOptionsAreAccepted(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	run(t, c, "magick a.png -auto-orient -strip -quality 80 q.jpg")
	if got := size(t, c, "q.jpg"); got != "40x20" {
		t.Errorf("got %s", got)
	}
}

func TestExtraIdentify(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	writeHome(t, c, "b.png", testPNG(t, 8, 6))
	if got := run(t, c, "identify a.png"); got != "a.png PNG 40x20" {
		t.Errorf("got %q", got)
	}
	if got := run(t, c, "magick identify a.png b.png"); got != "a.png PNG 40x20\nb.png PNG 8x6" {
		t.Errorf("got %q", got)
	}
}

func TestExtraMagickReadsWebPButDoesNotWriteIt(t *testing.T) {
	c := newExtra(t)
	// A 1x1 lossless WebP.
	webp, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	writeHome(t, c, "a.webp", webp)
	if got := run(t, c, "identify a.webp"); got != "a.webp WEBP 1x1" {
		t.Errorf("got %q", got)
	}
	run(t, c, "magick a.webp out.png")
	if msg, code := fail(t, c, "magick out.png out.webp"); code != 2 || !strings.Contains(msg, "cannot write") {
		t.Errorf("writing WebP: exit %d, %q", code, msg)
	}
}

func TestExtraMagickRefusesUnsupportedOptions(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	for _, name := range []string{"magick", "convert"} {
		msg, code := fail(t, c, name+" a.png -blur 0x3 out.png")
		if code != 2 || strings.Contains(msg, "\n") || !strings.HasPrefix(msg, name+": ") || !strings.Contains(msg, "-blur") {
			t.Errorf("%s: exit %d, %q", name, code, msg)
		}
		for _, opt := range []string{"-resize", "-thumbnail", "-crop", "-rotate", "-flip", "-flop", "-quality", "-strip", "-auto-orient"} {
			if !strings.Contains(msg, opt) {
				t.Errorf("%s: the message does not list %s: %q", name, opt, msg)
			}
		}
	}
}

func TestExtraMagickUsageErrors(t *testing.T) {
	c := newExtra(t)
	for _, script := range []string{
		"magick", "magick a.png", "magick a.png b.png c.png", "magick a.png -resize",
		"magick a.png -resize big b.png", "magick a.png -crop 10 b.png", "identify",
	} {
		if msg, code := fail(t, c, script); code != 2 || !strings.Contains(msg, ": ") {
			t.Errorf("%s: exit %d, %q", script, code, msg)
		}
	}
	if _, code := fail(t, c, "magick missing.png out.png"); code != 1 {
		t.Errorf("a missing input exited %d", code)
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

func TestExtraMagickRefusesHugeImages(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "huge.bmp", hugeBMP(20000, 20000))
	writeHome(t, c, "a.png", testPNG(t, 40, 20))
	if msg, code := fail(t, c, "magick huge.bmp -resize 10 out.png"); code != 1 || !strings.Contains(msg, "megapixels") {
		t.Errorf("a huge input: %d %q", code, msg)
	}
	if msg, code := fail(t, c, "magick a.png -resize '20000x20000!' out.png"); code != 1 || !strings.Contains(msg, "megapixels") {
		t.Errorf("a huge output: %d %q", code, msg)
	}
}

func TestExtraMagickHelp(t *testing.T) {
	c := newExtra(t)
	for _, script := range []string{"magick --help", "magick -help", "convert -help", "identify -help", "identify --help"} {
		help := run(t, c, script)
		if !strings.Contains(help, "subset of ImageMagick") {
			t.Errorf("%s: no mention of the subset: %q", script, help)
		}
	}
	help := run(t, c, "magick --help")
	for _, want := range []string{"-resize", "-thumbnail", "-crop", "-rotate", "-flip", "-flop", "-quality", "-strip", "-auto-orient", "identify"} {
		if !strings.Contains(help, want) {
			t.Errorf("--help lacks %q", want)
		}
	}
}

func TestExtraCommandsCannotReadTheHost(t *testing.T) {
	c := newExtra(t)
	if msg, code := fail(t, c, "identify /etc/passwd"); !strings.Contains(msg, "does not exist") {
		t.Errorf("identify /etc/passwd: %d %q", code, msg)
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
