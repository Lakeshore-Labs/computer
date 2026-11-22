package extra

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/ewhauser/gbash/commands"
	xdraw "golang.org/x/image/draw"

	"golang.org/x/image/bmp"
	"golang.org/x/image/colornames"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/tiff"
	_ "golang.org/x/image/webp" // registers the WebP decoder
)

// maxPixels bounds the size of an image to decode and of one to make. A small file can declare billions of
// pixels, and decoding it would exhaust memory.
const maxPixels = 100_000_000

const magickUsage = `usage: magick INPUT [OPTION...] OUTPUT
       convert INPUT [OPTION...] OUTPUT       (the same as magick)
       identify FILE...                       print NAME FORMAT WIDTHxHEIGHT for each file

A subset of ImageMagick. Options are applied left to right. The output format is the extension of OUTPUT.
Reads png, jpeg, gif, bmp, tiff and webp. Writes png, jpg, jpeg, gif, bmp and tiff.

Options:
  -resize GEOMETRY, -thumbnail GEOMETRY   W, xH, WxH (fit within), WxH! (exactly), N%, WxH> (only shrink), WxH^ (fill)
  -crop WxH+X+Y                           cut out a rectangle; +X+Y is optional and defaults to +0+0
  -rotate DEGREES                         turn clockwise; negative and fractional degrees are supported
  -flip, -flop                            mirror top to bottom, mirror left to right
  -background COLOR                       colour for exposed corners, flattening and JPEG output; default white
  -flatten, -alpha remove                 put the image on the background colour
  -quality N                              JPEG quality, 1 to 100; 90 by default
  -strip, -auto-orient                    accepted and ignored
  -help                                   print this text`

const identifyUsage = `usage: identify FILE...

A subset of ImageMagick. Prints one line per file: NAME FORMAT WIDTHxHEIGHT, for example "a.png PNG 800x600".
Reads png, jpeg, gif, bmp, tiff and webp.`

const supportedOptions = "-resize, -thumbnail, -crop, -rotate, -flip, -flop, -background, -flatten, -alpha, -quality, -strip, -auto-orient"

// define registers a command whose run reports mistakes as "name: message" and prints usage for -help.
func define(name, usage string, run func(context.Context, *commands.Invocation) error) commands.Command {
	return commands.DefineCommand(name, func(ctx context.Context, inv *commands.Invocation) error {
		for _, a := range inv.Args {
			if a == "-help" || a == "--help" {
				fmt.Fprintln(inv.Stdout, usage)
				return nil
			}
		}
		if err := run(ctx, inv); err != nil {
			return exit(inv, name, err)
		}
		return nil
	})
}

var (
	magickCommand   = define("magick", magickUsage, runMagick)
	convertCommand  = define("convert", magickUsage, runMagick)
	identifyCommand = define("identify", identifyUsage, runIdentify)
)

// geometry is an ImageMagick geometry, WxH+X+Y with the flags ! > ^ and %.
type geometry struct {
	w, h, x, y             int
	hasW, hasH             bool
	percent, exact, shrink bool
	fill                   bool
}

func parseGeometry(s string) (geometry, error) {
	var g geometry
	rest := s
	if i := strings.IndexAny(rest, "+-"); i >= 0 {
		if n, _ := fmt.Sscanf(rest[i:], "%d%d", &g.x, &g.y); n == 0 {
			return g, usagef("bad geometry %q: the offsets must look like +X+Y", s)
		}
		rest = rest[:i]
	}
	for rest != "" && strings.ContainsRune("!>^%", rune(rest[len(rest)-1])) {
		switch rest[len(rest)-1] {
		case '!':
			g.exact = true
		case '>':
			g.shrink = true
		case '^':
			g.fill = true
		case '%':
			g.percent = true
		}
		rest = rest[:len(rest)-1]
	}
	ws, hs, _ := strings.Cut(rest, "x")
	var err1, err2 error
	if ws != "" {
		g.w, err1 = strconv.Atoi(ws)
		g.hasW = true
	}
	if hs != "" {
		g.h, err2 = strconv.Atoi(hs)
		g.hasH = true
	}
	if err1 != nil || err2 != nil || !(g.hasW || g.hasH) || g.w < 0 || g.h < 0 {
		return g, usagef("bad geometry %q: it must look like 800, x600, 800x600 or 50%%", s)
	}
	return g, nil
}

// size is the dimensions that g asks for an image of sw by sh.
func (g geometry) size(sw, sh int) (int, int) {
	switch {
	case g.percent:
		if !g.hasH {
			g.h = g.w
		}
		return scaled(sw, g.w, 100), scaled(sh, g.h, 100)
	case g.exact:
		return cmp.Or(g.w, sw), cmp.Or(g.h, sh)
	case !g.hasH:
		return g.w, scaled(sh, g.w, sw)
	case !g.hasW:
		return scaled(sw, g.h, sh), g.h
	}
	// Both sides are given: the image fits within the box when its width is the limit, and fills it otherwise.
	if widthLimits := int64(g.w)*int64(sh) <= int64(g.h)*int64(sw); widthLimits != g.fill {
		return g.w, scaled(sh, g.w, sw)
	}
	return scaled(sw, g.h, sh), g.h
}

// op is one option's change to the image.
type op func(*image.NRGBA) (*image.NRGBA, error)

func runMagick(ctx context.Context, inv *commands.Invocation) error {
	args := inv.Args
	if len(args) > 0 && args[0] == "identify" {
		inv.Args = args[1:]
		return runIdentify(ctx, inv)
	}
	if len(args) > 0 && args[0] == "convert" {
		args = args[1:]
	}
	// The command line is checked before the input is read.
	var files []string
	var ops []op
	quality, background := 90, color.Color(color.White)
	for i := 0; i < len(args); i++ {
		name := args[i]
		if len(name) < 2 || (name[0] != '-' && name[0] != '+') {
			files = append(files, name)
			continue
		}
		// -strip and -auto-orient have nothing to do, because the encoders write no metadata and the decoders
		// do not read an orientation. +repage is a no-op because a crop here always makes a new image with no
		// offset.
		switch name {
		case "-strip", "-auto-orient", "+repage":
			continue
		case "-flip", "-flop":
			ops = append(ops, func(src *image.NRGBA) (*image.NRGBA, error) { return mirror(src, name == "-flip"), nil })
			continue
		case "-flatten":
			over := background // the colour as it is at this point of the command line, as ImageMagick reads it
			ops = append(ops, func(src *image.NRGBA) (*image.NRGBA, error) { return flatten(src, over), nil })
			continue
		case "-resize", "-thumbnail", "-crop", "-rotate", "-quality", "-background", "-alpha":
		default:
			return usagef("unsupported option %s; the supported options are %s", name, supportedOptions)
		}
		if i++; i == len(args) {
			return usagef("%s needs a value", name)
		}
		value := args[i]
		switch name {
		case "-quality":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 100 {
				return usagef("-quality is a number from 1 to 100, not %q", value)
			}
			quality = n
		case "-background":
			c, err := parseColor(value)
			if err != nil {
				return err
			}
			background = c
		case "-alpha":
			over := background
			switch value {
			case "remove", "off": // both leave an image with no transparency; remove is what puts it on the background
				ops = append(ops, func(src *image.NRGBA) (*image.NRGBA, error) { return flatten(src, over), nil })
			default:
				return usagef("-alpha supports remove and off, not %q", value)
			}
		case "-resize", "-thumbnail":
			g, err := parseGeometry(value)
			if err != nil {
				return err
			}
			ops = append(ops, func(src *image.NRGBA) (*image.NRGBA, error) {
				b := src.Bounds()
				w, h := g.size(b.Dx(), b.Dy())
				if g.shrink && w >= b.Dx() && h >= b.Dy() {
					return src, nil
				}
				return scale(src, w, h)
			})
		case "-crop":
			g, err := parseGeometry(value)
			if err != nil {
				return err
			}
			if !g.hasW || !g.hasH {
				return usagef("-crop needs both a width and a height, as in 100x50+10+10")
			}
			ops = append(ops, func(src *image.NRGBA) (*image.NRGBA, error) {
				// ImageMagick keeps the part of the rectangle that is inside the image.
				r := image.Rect(g.x, g.y, g.x+g.w, g.y+g.h).Intersect(src.Bounds())
				if r.Empty() {
					return nil, fmt.Errorf("the crop %s is outside the image, which is %v", value, src.Bounds())
				}
				dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
				draw.Draw(dst, dst.Bounds(), src, r.Min, draw.Src)
				return dst, nil
			})
		case "-rotate":
			degrees, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsInf(degrees, 0) || math.IsNaN(degrees) {
				return usagef("-rotate needs a finite angle in degrees, not %q", value)
			}
			// Preserve exact right angles for integers larger than a float64 can represent.
			if integer, err := strconv.ParseInt(value, 10, 64); err == nil {
				degrees = float64(integer % 360)
			}
			degrees = math.Mod(degrees, 360)
			over := background
			ops = append(ops, func(src *image.NRGBA) (*image.NRGBA, error) {
				if degrees == 0 {
					return src, nil
				}
				if math.Mod(degrees, 90) == 0 {
					return rotate(src, (int(degrees)+360)%360), nil
				}
				return rotateAngle(src, degrees, over)
			})
		}
	}
	if len(files) != 2 {
		return usagef("an input and an output file are needed, not %d file names; see -help", len(files))
	}
	encode, err := encoder(files[1], quality, background)
	if err != nil {
		return err
	}

	data, err := inv.FS.ReadFile(ctx, files[0])
	if err != nil {
		return err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
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
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	for _, apply := range ops {
		if out, err = apply(out); err != nil {
			return err
		}
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

func runIdentify(ctx context.Context, inv *commands.Invocation) error {
	if len(inv.Args) == 0 {
		return usagef("a file name is needed; see identify -help")
	}
	for _, name := range inv.Args {
		data, err := inv.FS.ReadFile(ctx, name)
		if err != nil {
			return err
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fmt.Fprintf(inv.Stdout, "%s %s %dx%d\n", name, strings.ToUpper(format), cfg.Width, cfg.Height)
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

// mirror flips src top to bottom if vertical, and left to right otherwise.
func mirror(src *image.NRGBA, vertical bool) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			dx, dy := b.Dx()-1-x, y
			if vertical {
				dx, dy = x, b.Dy()-1-y
			}
			dst.SetNRGBA(dx, dy, src.NRGBAAt(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
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

// rotateAngle places the image centers on the same point and rounds the new canvas outwards.
func rotateAngle(src *image.NRGBA, degrees float64, background color.Color) (*image.NRGBA, error) {
	b := src.Bounds()
	sin, cos := math.Sincos(degrees * math.Pi / 180)
	w, h := float64(b.Dx()), float64(b.Dy())
	dw := int(math.Ceil(math.Abs(w*cos) + math.Abs(h*sin)))
	dh := int(math.Ceil(math.Abs(w*sin) + math.Abs(h*cos)))
	if err := checkSize(dw, dh); err != nil {
		return nil, err
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	cx, cy := float64(b.Min.X)+w/2, float64(b.Min.Y)+h/2
	transform := f64.Aff3{cos, -sin, float64(dw)/2 - cos*cx + sin*cy, sin, cos, float64(dh)/2 - sin*cx - cos*cy}
	xdraw.BiLinear.Transform(dst, transform, src, b, draw.Src, nil)
	return dst, nil
}

// encoder is the writer for the format that name's extension gives.
// parseColor reads a colour as ImageMagick's options take one: a name, or #rgb or #rrggbb.
func parseColor(s string) (color.Color, error) {
	name := strings.ToLower(s)
	if c, ok := colornames.Map[name]; ok {
		return c, nil
	}
	if name == "none" || name == "transparent" {
		return color.Transparent, nil
	}
	var r, g, b uint8
	if n, _ := fmt.Sscanf(name, "#%02x%02x%02x", &r, &g, &b); n == 3 && len(name) == 7 {
		return color.NRGBA{r, g, b, 255}, nil
	}
	if n, _ := fmt.Sscanf(name, "#%1x%1x%1x", &r, &g, &b); n == 3 && len(name) == 4 {
		return color.NRGBA{r * 17, g * 17, b * 17, 255}, nil
	}
	return nil, usagef("unknown colour %q: use a name such as white, or #rrggbb", s)
}

// encoder writes an image in the format name's extension asks for. JPEG has no transparency, so there the image
// is put on the background first, or it would show as black.
func encoder(name string, quality int, background color.Color) (func(io.Writer, *image.NRGBA) error, error) {
	switch ext := strings.ToLower(path.Ext(name)); ext {
	case ".png":
		return func(w io.Writer, m *image.NRGBA) error { return png.Encode(w, m) }, nil
	case ".jpg", ".jpeg":
		return func(w io.Writer, m *image.NRGBA) error {
			return jpeg.Encode(w, flatten(m, background), &jpeg.Options{Quality: quality})
		}, nil
	case ".gif":
		return func(w io.Writer, m *image.NRGBA) error { return gif.Encode(w, m, nil) }, nil
	case ".bmp":
		return func(w io.Writer, m *image.NRGBA) error { return bmp.Encode(w, m) }, nil
	case ".tif", ".tiff":
		return func(w io.Writer, m *image.NRGBA) error { return tiff.Encode(w, m, nil) }, nil
	default:
		return nil, usagef("cannot write %q: the extension must be png, jpg, jpeg, gif, bmp or tiff", name)
	}
}

// flatten is m drawn over the colour over, which leaves no transparency unless the colour has some.
func flatten(m *image.NRGBA, over color.Color) *image.NRGBA {
	dst := image.NewNRGBA(m.Bounds())
	draw.Draw(dst, dst.Bounds(), image.NewUniform(over), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), m, m.Bounds().Min, draw.Over)
	return dst
}
