package extra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path"
	"strings"

	"github.com/ewhauser/gbash/commands"
	xdraw "golang.org/x/image/draw"

	"github.com/gen2brain/webp" // registers the WebP decoder
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// maxPixels bounds the size of an image to decode and of one to make. A small file can declare billions of
// pixels, and decoding it would exhaust memory.
const maxPixels = 100_000_000

const imageUsage = `usage: image COMMAND [ARGUMENTS]

  image info FILE [--json]                         print the format, width and height
  image convert IN OUT                             write IN as the format OUT's extension names
  image resize IN OUT --width N [--height N]       scale to a size; one of the two keeps the aspect ratio
  image thumbnail IN OUT --size N                  scale down to fit within N by N
  image crop IN OUT --x N --y N --width N --height N   cut out a rectangle
  image rotate IN OUT --degrees 90|180|270         turn clockwise

Reads png, jpeg, gif, bmp, tiff and webp. Writes png, jpg, jpeg, gif, bmp, tiff and webp, by the extension of OUT.
Options:
  --json        info: print JSON
  --width N     resize, crop: the width in pixels
  --height N    resize, crop: the height in pixels
  --size N      thumbnail: the side of the square
  --x N, --y N  crop: the left and top of the rectangle
  --degrees N   rotate: 90, 180 or 270
  --help        print this text`

var imageCommand = commands.DefineCommand("image", func(ctx context.Context, inv *commands.Invocation) error {
	err := runImage(ctx, inv)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprintln(inv.Stdout, imageUsage)
		return nil
	}
	return exit(inv, "image", err)
})

func runImage(ctx context.Context, inv *commands.Invocation) error {
	if len(inv.Args) == 0 {
		return usagef("a command is needed; see image --help")
	}
	sub, args := inv.Args[0], inv.Args[1:]
	if sub == "--help" || sub == "-h" || sub == "help" {
		return flag.ErrHelp
	}
	fs := flag.NewFlagSet(sub, flag.ContinueOnError)
	var asJSON bool
	var width, height, size, x, y, degrees int
	fs.BoolVar(&asJSON, "json", false, "")
	fs.IntVar(&width, "width", 0, "")
	fs.IntVar(&height, "height", 0, "")
	fs.IntVar(&size, "size", 0, "")
	fs.IntVar(&x, "x", 0, "")
	fs.IntVar(&y, "y", 0, "")
	fs.IntVar(&degrees, "degrees", 0, "")
	files, err := parse(fs, args)
	if err != nil {
		return err
	}
	want := 2
	if sub == "info" {
		want = 1
	}
	switch sub {
	case "info", "convert", "resize", "thumbnail", "crop", "rotate":
	default:
		return usagef("unknown command %q; see image --help", sub)
	}
	if len(files) != want {
		return usagef("%s takes %d file name(s), not %d", sub, want, len(files))
	}

	if sub == "info" {
		data, err := inv.FS.ReadFile(ctx, files[0])
		if err != nil {
			return err
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", files[0], err)
		}
		if asJSON {
			return json.NewEncoder(inv.Stdout).Encode(map[string]any{"file": files[0], "format": format, "width": cfg.Width, "height": cfg.Height})
		}
		_, err = fmt.Fprintf(inv.Stdout, "%s: %s %dx%d\n", files[0], format, cfg.Width, cfg.Height)
		return err
	}

	// The command line is checked before the input is read.
	var transform func(*image.NRGBA) (*image.NRGBA, error)
	switch sub {
	case "resize":
		if width <= 0 && height <= 0 {
			return usagef("resize needs --width, --height or both")
		}
		transform = func(src *image.NRGBA) (*image.NRGBA, error) {
			w, h := width, height
			b := src.Bounds()
			if w <= 0 {
				w = scaled(b.Dx(), h, b.Dy())
			} else if h <= 0 {
				h = scaled(b.Dy(), w, b.Dx())
			}
			return scale(src, w, h)
		}
	case "thumbnail":
		if size <= 0 {
			return usagef("thumbnail needs --size")
		}
		transform = func(src *image.NRGBA) (*image.NRGBA, error) {
			b := src.Bounds()
			if b.Dx() <= size && b.Dy() <= size {
				return src, nil
			}
			if b.Dx() >= b.Dy() {
				return scale(src, size, scaled(b.Dy(), size, b.Dx()))
			}
			return scale(src, scaled(b.Dx(), size, b.Dy()), size)
		}
	case "crop":
		if width <= 0 || height <= 0 {
			return usagef("crop needs --width and --height")
		}
		transform = func(src *image.NRGBA) (*image.NRGBA, error) {
			r := image.Rect(x, y, x+width, y+height)
			if !r.In(src.Bounds()) {
				return nil, fmt.Errorf("the rectangle %v is not inside the image, which is %v", r, src.Bounds())
			}
			dst := image.NewNRGBA(image.Rect(0, 0, width, height))
			draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
			return dst, nil
		}
	case "rotate":
		if degrees != 90 && degrees != 180 && degrees != 270 {
			return usagef("rotate needs --degrees 90, 180 or 270")
		}
		transform = func(src *image.NRGBA) (*image.NRGBA, error) { return rotate(src, degrees), nil }
	case "convert":
		transform = func(src *image.NRGBA) (*image.NRGBA, error) { return src, nil }
	}
	encode, err := encoder(files[1])
	if err != nil {
		return err
	}

	data, err := inv.FS.ReadFile(ctx, files[0])
	if err != nil {
		return err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil {
		err = hostLibrary(format)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", files[0], err)
	}
	if err := checkSize(cfg.Width, cfg.Height); err != nil {
		return fmt.Errorf("%s: %w", files[0], err)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%s: %w", files[0], err)
	}
	src := image.NewNRGBA(img.Bounds())
	draw.Draw(src, src.Bounds(), img, img.Bounds().Min, draw.Src)
	out, err := transform(src)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := encode(&buf, out); err != nil {
		return err
	}
	f, err := inv.FS.OpenFile(ctx, files[1], os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(buf.Bytes())
	return errors.Join(err, f.Close())
}

// hostLibrary refuses WebP when the webp package has loaded the host's libwebp, which it does unless the program
// is built with -tags nodynamic. The computer runs only code that is compiled into it.
func hostLibrary(format string) error {
	if format == "webp" && webp.Dynamic() == nil {
		return errors.New("webp is refused: this program loads the host's libwebp; build it with -tags nodynamic")
	}
	return nil
}

func checkSize(w, h int) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("the size %dx%d is empty", w, h)
	}
	if mp := float64(w) * float64(h) / 1e6; mp > maxPixels/1e6 {
		return fmt.Errorf("%dx%d is %.0f megapixels, more than the limit of %d", w, h, mp, maxPixels/1_000_000)
	}
	return nil
}

// scaled is n*to/from rounded to the nearest pixel, at least one.
func scaled(n, to, from int) int {
	return max(1, int((int64(n)*int64(to)+int64(from)/2)/int64(from)))
}

func scale(src *image.NRGBA, w, h int) (*image.NRGBA, error) {
	if err := checkSize(w, h); err != nil {
		return nil, err
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst, nil
}

// rotate turns src clockwise by degrees, which is 90, 180 or 270.
func rotate(src *image.NRGBA, degrees int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if degrees != 180 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch degrees {
			case 90:
				dx, dy = h-1-y, x
			case 180:
				dx, dy = w-1-x, h-1-y
			default:
				dx, dy = y, w-1-x
			}
			dst.SetNRGBA(dx, dy, src.NRGBAAt(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// encoder is the writer for the format that name's extension gives.
func encoder(name string) (func(io.Writer, *image.NRGBA) error, error) {
	switch ext := strings.ToLower(path.Ext(name)); ext {
	case ".png":
		return func(w io.Writer, m *image.NRGBA) error { return png.Encode(w, m) }, nil
	case ".jpg", ".jpeg":
		return func(w io.Writer, m *image.NRGBA) error {
			return jpeg.Encode(w, flatten(m), &jpeg.Options{Quality: 90})
		}, nil
	case ".gif":
		return func(w io.Writer, m *image.NRGBA) error { return gif.Encode(w, m, nil) }, nil
	case ".bmp":
		return func(w io.Writer, m *image.NRGBA) error { return bmp.Encode(w, m) }, nil
	case ".webp":
		return func(w io.Writer, m *image.NRGBA) error {
			if err := hostLibrary("webp"); err != nil {
				return err
			}
			return webp.Encode(w, m, webp.Options{Quality: 90})
		}, nil
	case ".tif", ".tiff":
		return func(w io.Writer, m *image.NRGBA) error { return tiff.Encode(w, m, nil) }, nil
	default:
		return nil, usagef("cannot write %q: the extension must be png, jpg, jpeg, gif, bmp or tiff", name)
	}
}

// flatten puts m on white, since JPEG has no transparency and would show it as black.
func flatten(m *image.NRGBA) image.Image {
	dst := image.NewRGBA(m.Bounds())
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), m, m.Bounds().Min, draw.Over)
	return dst
}
