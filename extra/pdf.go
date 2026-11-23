package extra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/Lakeshore-Labs/gbash/commands"
	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

const (
	// maxRenderPixels bounds one rendered page. PDFium draws it into 4 bytes per pixel of WebAssembly memory.
	maxRenderPixels = 50_000_000
	// maxRenderPages bounds the pages one pdftoppm run renders.
	maxRenderPages = 100
	// pdfMemoryPages is the memory limit of the PDFium module, in 64 KiB pages: 1 GiB.
	pdfMemoryPages = 16384
)

const pdftotextUsage = `usage: pdftotext [OPTION...] FILE.pdf [OUTPUT.txt | -]

A subset of poppler's pdftotext. Writes the text of FILE.pdf, a form feed after each page. FILE.pdf may be - for
standard input. OUTPUT.txt may be - for standard output; without it the text goes to FILE.txt.

Options:
  -f N        first page to convert (the first page is 1)
  -l N        last page to convert
  -layout     accepted and ignored; the text is in reading order
  -help       print this text`

const pdftoppmUsage = `usage: pdftoppm -png|-jpeg [OPTION...] FILE.pdf PREFIX

A subset of poppler's pdftoppm. Renders each page of FILE.pdf to PREFIX-N.png or PREFIX-N.jpg. FILE.pdf may be -
for standard input. At most 100 pages, and 50 megapixels for a page.

Options:
  -png, -jpeg   the output format; one of them is needed
  -r DPI        resolution in dots per inch (default 150)
  -f N          first page to convert (the first page is 1)
  -l N          last page to convert
  -help         print this text`

const (
	pdftotextOptions = "-f, -l, -layout"
	pdftoppmOptions  = "-png, -jpeg, -r, -f, -l"
)

var (
	pdftotextCommand = define("pdftotext", pdftotextUsage, runPdftotext)
	pdftoppmCommand  = define("pdftoppm", pdftoppmUsage, runPdftoppm)
)

// pdfArgs is a parsed pdftotext or pdftoppm command line.
type pdfArgs struct {
	files       []string
	first, last int // 0 when not given
	dpi         int
	format      string // "png" or "jpeg", for pdftoppm
}

// parsePDFArgs reads poppler-style options: a single dash and a name, with the value in the next argument.
// supported is the list of options the command accepts, in the message for any other.
func parsePDFArgs(args []string, supported string) (pdfArgs, error) {
	a := pdfArgs{dpi: 150}
	has := func(opt string) bool { return strings.Contains(", "+supported+",", ", "+opt+",") }
	for i := 0; i < len(args); i++ {
		opt := args[i]
		if len(opt) < 2 || opt[0] != '-' {
			a.files = append(a.files, opt)
			continue
		}
		if !has(opt) {
			return a, usagef("unsupported option %s; the supported options are %s", opt, supported)
		}
		switch opt {
		case "-layout":
		case "-png":
			a.format = "png"
		case "-jpeg":
			a.format = "jpeg"
		default: // -f, -l, -r take a number
			if i++; i == len(args) {
				return a, usagef("%s needs a value", opt)
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				return a, usagef("%s needs a positive number, not %q", opt, args[i])
			}
			switch opt {
			case "-f":
				a.first = n
			case "-l":
				a.last = n
			case "-r":
				a.dpi = n
			}
		}
	}
	if a.last != 0 && a.first > a.last {
		return a, usagef("the first page %d is after the last page %d", a.first, a.last)
	}
	return a, nil
}

// pages is the page range to convert, from 1, for a document of n pages. As in poppler, a last page past the end
// of the document is the end of the document.
func (a pdfArgs) pages(n int) (first, last int, err error) {
	first, last = max(a.first, 1), n
	if a.last != 0 {
		last = min(a.last, n)
	}
	if first > last {
		return 0, 0, fmt.Errorf("the document has %d pages, so there is no page %d", n, first)
	}
	return first, last, nil
}

// readPDF reads the file name, or standard input for "-".
func readPDF(ctx context.Context, inv *commands.Invocation, name string) ([]byte, error) {
	if name != "-" {
		return inv.FS.ReadFile(ctx, name)
	}
	if inv.Stdin == nil {
		return nil, nil
	}
	return io.ReadAll(inv.Stdin)
}

// compiledPDFium keeps the compiled PDFium module in memory, so that only the first use in a process pays for
// compiling it. It is not written to a directory.
var compiledPDFium = sync.OnceValue(wazero.NewCompilationCache)

// withPDF opens data as a document in PDFium, which runs as WebAssembly on wazero, and calls fn with it.
// The WebAssembly module gets no directories, no standard streams and a memory limit, so it can reach nothing
// but the bytes it is given. The module is compiled on the first call in a process.
func withPDF(ctx context.Context, data []byte, fn func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, pages int) error) error {
	pool, err := webassembly.Init(webassembly.Config{
		Context:  ctx,
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
		// An FSConfig with no mounts: the default would mount the host's root directory.
		FSConfig: wazero.NewFSConfig(),
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		RuntimeConfig: wazero.NewRuntimeConfig().
			WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
			WithMemoryLimitPages(pdfMemoryPages).
			WithCloseOnContextDone(true).
			WithCompilationCache(compiledPDFium()),
	})
	if err != nil {
		return err
	}
	defer pool.Close()
	p, err := pool.GetInstanceWithContext(ctx)
	if err != nil {
		return err
	}
	defer p.Close()
	opened, err := p.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return fmt.Errorf("not a readable PDF: %w", err)
	}
	defer p.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: opened.Document})
	count, err := p.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: opened.Document})
	if err != nil {
		return err
	}
	return fn(p, opened.Document, count.PageCount)
}

func writeFile(ctx context.Context, inv *commands.Invocation, name string, data []byte) error {
	f, err := inv.FS.OpenFile(ctx, name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Close())
}

func runPdftotext(ctx context.Context, inv *commands.Invocation) error {
	a, err := parsePDFArgs(inv.Args, pdftotextOptions)
	if err != nil {
		return err
	}
	if len(a.files) == 0 || len(a.files) > 2 {
		return usagef("an input file and at most one output file are needed; see pdftotext -help")
	}
	in := a.files[0]
	out := strings.TrimSuffix(in, path.Ext(in)) + ".txt"
	if len(a.files) == 2 {
		out = a.files[1]
	} else if in == "-" {
		out = "-"
	}
	data, err := readPDF(ctx, inv, in)
	if err != nil {
		return err
	}
	var text bytes.Buffer
	err = withPDF(ctx, data, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, n int) error {
		first, last, err := a.pages(n)
		if err != nil {
			return err
		}
		for i := first; i <= last; i++ {
			page, err := p.GetPageText(&requests.GetPageText{Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: i - 1}}})
			if err != nil {
				return fmt.Errorf("page %d: %w", i, err)
			}
			text.WriteString(strings.TrimRight(strings.ReplaceAll(page.Text, "\r\n", "\n"), "\n"))
			text.WriteString("\n\f")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if out == "-" {
		_, err = inv.Stdout.Write(text.Bytes())
		return err
	}
	return writeFile(ctx, inv, out, text.Bytes())
}

func runPdftoppm(ctx context.Context, inv *commands.Invocation) error {
	a, err := parsePDFArgs(inv.Args, pdftoppmOptions)
	if err != nil {
		return err
	}
	if a.format == "" {
		return usagef("an output format is needed; the supported options are %s", pdftoppmOptions)
	}
	if len(a.files) != 2 {
		return usagef("an input file and an output prefix are needed; see pdftoppm -help")
	}
	data, err := readPDF(ctx, inv, a.files[0])
	if err != nil {
		return err
	}
	prefix, ext := a.files[1], map[string]string{"png": ".png", "jpeg": ".jpg"}[a.format]
	return withPDF(ctx, data, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, n int) error {
		first, last, err := a.pages(n)
		if err != nil {
			return err
		}
		if last-first+1 > maxRenderPages {
			return fmt.Errorf("%d pages is more than the limit of %d; use -f and -l", last-first+1, maxRenderPages)
		}
		for i := first; i <= last; i++ {
			page := requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: i - 1}}
			size, err := p.GetPageSizeInPixels(&requests.GetPageSizeInPixels{Page: page, DPI: a.dpi})
			if err != nil {
				return fmt.Errorf("page %d: %w", i, err)
			}
			if err := checkSize(size.Width, size.Height); err != nil {
				return fmt.Errorf("page %d: %w", i, err)
			}
			if px := int64(size.Width) * int64(size.Height); px > maxRenderPixels {
				return fmt.Errorf("page %d is %dx%d, %d megapixels, more than the limit of %d", i, size.Width, size.Height, px/1_000_000, maxRenderPixels/1_000_000)
			}
			rendered, err := p.RenderPageInDPI(&requests.RenderPageInDPI{Page: page, DPI: a.dpi})
			if err != nil {
				return fmt.Errorf("page %d: %w", i, err)
			}
			var buf bytes.Buffer
			if a.format == "png" {
				err = png.Encode(&buf, rendered.Result.Image)
			} else {
				err = jpeg.Encode(&buf, rendered.Result.Image, &jpeg.Options{Quality: 90})
			}
			rendered.Cleanup() // the pixels are in WebAssembly memory until now
			if err != nil {
				return err
			}
			name := fmt.Sprintf("%s-%0*d%s", prefix, len(strconv.Itoa(last)), i, ext)
			if err := writeFile(ctx, inv, name, buf.Bytes()); err != nil {
				return err
			}
		}
		return nil
	})
}
