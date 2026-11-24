package computer

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lakeshore-Labs/gbash"

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

// imageSize is the format and size of the image name in the home directory, as "png 400x200".
func imageSize(t *testing.T, c *Computer, name string) string {
	t.Helper()
	f, err := os.Open(filepath.Join(hostHome(c), name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s %dx%d", format, cfg.Width, cfg.Height)
}

func TestExtraCommandsCannotReadTheHost(t *testing.T) {
	c := newExtra(t)
	if msg, code := fail(t, c, "pdftotext /etc/passwd -"); !strings.Contains(msg, "does not exist") {
		t.Errorf("pdftotext /etc/passwd: %d %q", code, msg)
	}
}

// testPDF is a PDF with one 200 by 100 point page for each of texts, each showing its text in Helvetica.
func testPDF(texts ...string) []byte {
	var objs []string
	kids := make([]string, len(texts))
	for i, text := range texts {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
		stream := fmt.Sprintf("BT /F1 18 Tf 20 40 Td (%s) Tj ET", text)
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	objs = append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(texts)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, objs...)
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

func TestExtraPdftotextWritesTheTextOfEveryPage(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.pdf", testPDF("Hello first", "Hello second"))
	run(t, c, "pdftotext a.pdf out.txt")
	b, err := os.ReadFile(filepath.Join(hostHome(c), "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), "Hello first\n\fHello second\n\f"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Without an output name the text goes next to the input, with a .txt extension.
	run(t, c, "pdftotext a.pdf")
	if _, err := os.Stat(filepath.Join(hostHome(c), "a.txt")); err != nil {
		t.Error(err)
	}
}

func TestExtraPdftotextToStandardOutputAndPageRange(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.pdf", testPDF("Hello first", "Hello second", "Hello third"))
	if got := run(t, c, "pdftotext -f 2 -l 2 a.pdf -"); strings.TrimSpace(got) != "Hello second" {
		t.Errorf("-f 2 -l 2: %q", got)
	}
	if got := run(t, c, "pdftotext -f 2 a.pdf -"); !strings.Contains(got, "second") || !strings.Contains(got, "third") || strings.Contains(got, "first") {
		t.Errorf("-f 2: %q", got)
	}
	if got := run(t, c, "cat a.pdf | pdftotext - -"); !strings.Contains(got, "first") {
		t.Errorf("standard input: %q", got)
	}
	if got := run(t, c, "pdftotext -layout a.pdf -"); !strings.Contains(got, "first") {
		t.Errorf("-layout: %q", got)
	}
}

func TestExtraPdftoppmRendersPagesAtTheDPI(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.pdf", testPDF("Hello first", "Hello second"))
	run(t, c, "pdftoppm -png -r 144 a.pdf out")
	// A 200 by 100 point page is 400 by 200 pixels at 144 DPI.
	for _, name := range []string{"out-1.png", "out-2.png"} {
		if got := imageSize(t, c, name); got != "png 400x200" {
			t.Errorf("got %q", got)
		}
	}
	// The page is not blank: the text is drawn.
	b, _ := os.ReadFile(filepath.Join(hostHome(c), "out-1.png"))
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	dark := 0
	for y := range 200 {
		for x := range 400 {
			if r, _, _, _ := img.At(x, y).RGBA(); r < 0x8000 {
				dark++
			}
		}
	}
	if dark == 0 {
		t.Error("the page is blank")
	}
	run(t, c, "pdftoppm -png -r 72 -f 2 -l 2 a.pdf one")
	if got := imageSize(t, c, "one-2.png"); got != "png 200x100" {
		t.Errorf("got %q", got)
	}
	if _, err := os.Stat(filepath.Join(hostHome(c), "one-1.png")); err == nil {
		t.Error("-f 2 rendered page 1")
	}
	run(t, c, "pdftoppm -jpeg -r 72 -f 1 -l 1 a.pdf j")
	if got := imageSize(t, c, "j-1.jpg"); got != "jpeg 200x100" {
		t.Errorf("got %q", got)
	}
}

func TestExtraPdfCommandsRefuseUnsupportedOptions(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.pdf", testPDF("Hello"))
	for script, listed := range map[string]string{
		"pdftotext -raw a.pdf -":          "-f, -l, -layout",
		"pdftoppm -tiff a.pdf out":        "-png, -jpeg, -r, -f, -l",
		"pdftoppm -r 72 a.pdf out":        "-png, -jpeg, -r, -f, -l", // no format
		"pdftoppm -png -scale-to 9 a.pdf": "-png, -jpeg, -r, -f, -l",
	} {
		msg, code := fail(t, c, script)
		if code != 2 || strings.Contains(msg, "\n") || !strings.Contains(msg, listed) {
			t.Errorf("%s: exit %d, %q", script, code, msg)
		}
	}
}

func TestExtraPdfCommandsFailOnBadInput(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "bad.pdf", []byte("not a pdf"))
	writeHome(t, c, "a.pdf", testPDF("Hello"))
	for script, want := range map[string]int{
		"pdftotext bad.pdf -":              1,
		"pdftotext missing.pdf -":          1,
		"pdftotext /etc/passwd -":          1, // the host's files are not there
		"pdftotext a.pdf":                  0,
		"pdftotext -f 3 a.pdf -":           1, // past the last page
		"pdftotext -f 0 a.pdf -":           2,
		"pdftotext":                        2,
		"pdftoppm -png bad.pdf out":        1,
		"pdftoppm -png -r 0 a.pdf out":     2,
		"pdftoppm -png a.pdf":              2,
		"pdftoppm -png -f 5 a.pdf out":     1,
		"pdftoppm -png -f 1 -l 99 a.pdf o": 0, // the last page is clamped, as poppler does
	} {
		if want == 0 {
			run(t, c, script)
			continue
		}
		if msg, code := fail(t, c, script); code != want || !strings.HasPrefix(msg, strings.Fields(script)[0]+": ") {
			t.Errorf("%s: exit %d, want %d: %q", script, code, want, msg)
		}
	}
}

func TestExtraPdftoppmRefusesHugeImages(t *testing.T) {
	c := newExtra(t)
	writeHome(t, c, "a.pdf", testPDF("Hello"))
	if msg, code := fail(t, c, "pdftoppm -png -r 20000 a.pdf out"); code != 1 || !strings.Contains(msg, "megapixels") {
		t.Errorf("a huge page: %d %q", code, msg)
	}
}

func TestExtraPdfCommandsHelp(t *testing.T) {
	c := newExtra(t)
	for _, name := range []string{"pdftotext", "pdftoppm"} {
		if help := run(t, c, name+" --help"); !strings.Contains(help, "usage: "+name) {
			t.Errorf("%s --help: %q", name, help)
		}
	}
}
