package extra

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/Lakeshore-Labs/gbash/commands"
	"github.com/go-analyze/charts"
)

const chartUsage = `usage: chart KIND --x COLUMN --y COLUMN[,COLUMN...] [--title TEXT] [--out FILE] [INPUT.csv]

Draws a chart of a CSV table that has a header row. It reads INPUT.csv, or standard input without one.

  chart bar       one group of bars per row, one bar per --y column, with a zero baseline
  chart line      one line per --y column
  chart pie       one slice per row; takes a single --y column
  chart scatter   one set of points per --y column; the rows are spaced evenly, whatever the --x values are

Options:
  --x COLUMN    the column whose values name the rows
  --y COLUMNS   the numeric columns to draw, separated by commas
  --title TEXT  a title above the chart
  --out FILE    where to write it: FILE.svg or FILE.png; an SVG on standard output without it
  --help        print this text`

var chartCommand = commands.DefineCommand("chart", func(ctx context.Context, inv *commands.Invocation) error {
	err := runChart(ctx, inv)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprintln(inv.Stdout, chartUsage)
		return nil
	}
	return exit(inv, "chart", err)
})

func runChart(ctx context.Context, inv *commands.Invocation) error {
	fs := flag.NewFlagSet("chart", flag.ContinueOnError)
	var x, y, title, out string
	fs.StringVar(&x, "x", "", "")
	fs.StringVar(&y, "y", "", "")
	fs.StringVar(&title, "title", "", "")
	fs.StringVar(&out, "out", "", "")
	args, err := parse(fs, inv.Args)
	if err != nil {
		return err
	}
	if len(args) == 0 || len(args) > 2 {
		return usagef("a chart kind and at most one input file are needed; see chart --help")
	}
	kind, args := args[0], args[1:]
	if kind != "bar" && kind != "line" && kind != "pie" && kind != "scatter" {
		return usagef("unknown chart kind %q; it is bar, line, pie or scatter", kind)
	}
	ys := strings.Split(y, ",")
	switch {
	case x == "":
		return usagef("--x is needed")
	case y == "":
		return usagef("--y is needed")
	case kind == "pie" && len(ys) != 1:
		return usagef("a pie chart draws one --y column")
	}
	format := charts.ChartOutputSVG
	if out != "" {
		switch strings.ToLower(path.Ext(out)) {
		case ".svg":
		case ".png":
			format = charts.ChartOutputPNG
		default:
			return usagef("cannot write %q: the extension must be svg or png", out)
		}
	}

	var in io.Reader = inv.Stdin
	if len(args) == 1 {
		data, err := inv.FS.Open(ctx, args[0])
		if err != nil {
			return err
		}
		defer data.Close()
		in = data
	}
	if in == nil {
		in = strings.NewReader("")
	}
	labels, series, err := readColumns(in, x, ys)
	if err != nil {
		return err
	}

	p := charts.NewPainter(charts.PainterOptions{OutputFormat: format, Width: 800, Height: 500})
	heading := charts.TitleOption{Text: title}
	legend := charts.LegendOption{SeriesNames: ys}
	switch kind {
	case "bar":
		opt := charts.NewBarChartOptionWithSeries(charts.NewSeriesListBar(series, charts.BarSeriesOption{Names: ys}))
		opt.CategoryAxis.Labels, opt.Title, opt.Legend = labels, heading, legend
		// Bar lengths measure distance from zero, even when the observations are close together.
		var positive, negative bool
		for _, column := range series {
			for _, value := range column {
				if value == charts.GetNullValue() || math.IsInf(value, 0) {
					continue
				}
				positive = positive || value > 0
				negative = negative || value < 0
			}
		}
		switch {
		case positive && !negative:
			opt.ValueAxis[0].Min = charts.Ptr(0.0)
		case negative && !positive:
			opt.ValueAxis[0].Max = charts.Ptr(0.0)
		}
		err = p.BarChart(opt)
	case "line":
		opt := charts.NewLineChartOptionWithSeries(charts.NewSeriesListLine(series, charts.LineSeriesOption{Names: ys}))
		opt.XAxis.Labels, opt.Title, opt.Legend = labels, heading, legend
		err = p.LineChart(opt)
	case "scatter":
		opt := charts.NewScatterChartOptionWithSeries(charts.NewSeriesListScatter(series, charts.ScatterSeriesOption{Names: ys}))
		opt.XAxis.Labels, opt.Title, opt.Legend = labels, heading, legend
		err = p.ScatterChart(opt)
	case "pie":
		opt := charts.NewPieChartOptionWithData(series[0])
		opt.SeriesList = charts.NewSeriesListPie(series[0], charts.PieSeriesOption{Names: labels})
		opt.Title, opt.Legend = heading, charts.LegendOption{SeriesNames: labels}
		err = p.PieChart(opt)
	}
	if err != nil {
		return err
	}
	drawing, err := p.Bytes()
	if err != nil {
		return err
	}
	if out == "" {
		_, err = inv.Stdout.Write(drawing)
		return err
	}
	f, err := inv.FS.OpenFile(ctx, out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(drawing)
	return errors.Join(err, f.Close())
}

// readColumns reads a CSV table with a header row and returns the values of column x as labels, and the numbers
// of each of the columns ys, one slice per column.
func readColumns(r io.Reader, x string, ys []string) (labels []string, series [][]float64, err error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("reading the header row: %w", err)
	}
	column := func(name string) (int, error) {
		for i, h := range header {
			if h == name {
				return i, nil
			}
		}
		return 0, fmt.Errorf("there is no column %q; the columns are %s", name, strings.Join(header, ", "))
	}
	xi, err := column(x)
	if err != nil {
		return nil, nil, err
	}
	yi := make([]int, len(ys))
	for i, name := range ys {
		if yi[i], err = column(name); err != nil {
			return nil, nil, err
		}
	}
	series = make([][]float64, len(ys))
	for row := 2; ; row++ { // row 1 is the header
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		labels = append(labels, record[xi])
		for i, col := range yi {
			v, err := strconv.ParseFloat(strings.TrimSpace(record[col]), 64)
			if err != nil {
				return nil, nil, fmt.Errorf("row %d, column %q: %q is not a number", row, ys[i], record[col])
			}
			series[i] = append(series[i], v)
		}
	}
	if len(labels) == 0 {
		return nil, nil, errors.New("the table has no rows")
	}
	return labels, series, nil
}
