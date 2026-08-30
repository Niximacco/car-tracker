// Package chart turns a column of numbers into the coordinates of a picture of
// them.
//
// It draws nothing itself. It works out the geometry - where each point lands,
// where the gridlines go, what the axis says - and hands back strings the
// templates put straight into an inline <svg>. Doing it this way means there is
// no charting library to load, nothing to fetch from a third party, and the
// arithmetic behind every line on the site is something a test can look at.
//
// The svg is inline rather than an image for the same reason the stylesheet is
// embedded: a deploy is one container, and a chart that is part of the document
// takes its colours from the same variables the rest of the page does, so it is
// right in both themes without being drawn twice.
package chart

import (
	"fmt"
	"math"
	"strings"
)

// The drawing area, in the units the svg's viewBox is set in. The picture is
// scaled to whatever width the page gives it, so these are proportions rather
// than pixels: the shape of the box, not its size.
const (
	Width  = 720.0
	Height = 240.0

	padLeft   = 46.0
	padRight  = 12.0
	padTop    = 12.0
	padBottom = 26.0
)

// Value is one reading: what it was, and what to call it when somebody hovers
// over it.
type Value struct {
	Y     float64
	Label string
	// Detail is the line of text a reader gets on hover. It is the whole of the
	// interactivity here on purpose - a chart you can brush, zoom and filter is
	// a chart nobody reads.
	Detail string
	// Missing marks a gap: a fill with no mpg to plot. The line breaks rather
	// than running straight through it, because a line drawn across a gap is a
	// claim that nothing happened there.
	Missing bool
}

// Series is one line on the chart.
type Series struct {
	Name string
	// Class is the css class the line and its points take their colour from, so
	// the palette lives in the stylesheet with every other colour on the site.
	Class  string
	Values []Value
	// Dashed is for a line that is a smoothing of another one rather than a
	// reading in its own right.
	Dashed bool
}

// Point is one plotted reading, in svg coordinates.
//
// The coordinates are strings rather than floats because they exist to be
// interpolated into markup and nothing else. A float rendered by a template
// occasionally comes out as 240.00000000000003, which is valid svg and
// unreadable source.
type Point struct {
	X      string
	Y      string
	Label  string
	Detail string
}

// Line is a series with its geometry worked out.
type Line struct {
	Name   string
	Class  string
	Dashed bool
	// Path is the svg path of the line, with a break at every gap.
	Path   string
	Points []Point
}

// Tick is one gridline and its label, both as coordinates ready to be written
// into the markup.
type Tick struct {
	// At is where the gridline goes: a y coordinate on the vertical axis, an x
	// coordinate on the horizontal one.
	At string
	// Text is where the label's baseline goes, which is not quite the same
	// place - a label centred on its line reads as centred, and a label sitting
	// exactly on it reads as sitting above it.
	Text  string
	Label string
}

// Chart is everything a template needs to draw the picture.
type Chart struct {
	// ViewBox is the svg's own coordinate system. The picture scales to
	// whatever width the page gives it, so nothing here is in pixels.
	ViewBox string
	Lines   []Line
	YTicks  []Tick
	XTicks  []Tick
	// Empty is set when there was nothing to plot, so the page can say so
	// rather than drawing an empty box with an axis on it.
	Empty bool
	// Left and Right are where the gridlines start and stop.
	Left  string
	Right string
	// LabelX is where the vertical axis labels are anchored, and LabelY is the
	// baseline the horizontal ones sit on.
	LabelX string
	LabelY string
}

// Line draws one or more series against a shared vertical scale.
//
// The vertical axis does not start at zero. These are fuel figures in the
// thirties and prices between one and five dollars; an axis from zero would
// turn every one of them into a flat line near the top of the box, which is the
// opposite of what the picture is for. It is a chart of how something changed,
// and it is labelled with the numbers it starts and ends at so it cannot be
// read as anything else.
//
// The horizontal axis is the reading's position in the history rather than its
// date. Fill-ups are close to evenly spaced in time and a proper time axis
// would cost a lot of code to move a handful of points a few units sideways.
func Draw(series []Series, ticks int) Chart {
	left, right := padLeft, Width-padRight
	top, bottom := padTop, Height-padBottom

	drawing := Chart{
		ViewBox: fmt.Sprintf("0 0 %s %s", number(Width), number(Height)),
		Left:    number(left),
		Right:   number(right),
		LabelX:  number(left - 8),
		LabelY:  number(Height - 8),
	}

	low, high, count := bounds(series)
	if count < 2 {
		drawing.Empty = true
		return drawing
	}

	low, high = pad(low, high)

	plotWidth := right - left
	plotHeight := bottom - top

	// x maps a position in the series onto the box, y maps a value onto it
	// upside down, because svg counts downwards from the top.
	x := func(at int) float64 {
		return left + plotWidth*float64(at)/float64(count-1)
	}

	y := func(value float64) float64 {
		return bottom - plotHeight*(value-low)/(high-low)
	}

	for _, one := range series {
		line := Line{Name: one.Name, Class: one.Class, Dashed: one.Dashed}

		var path strings.Builder
		open := false

		for at, value := range one.Values {
			if value.Missing {
				// A gap ends the current stroke. The next real reading starts a
				// fresh one, so the line has a hole in it rather than a lie.
				open = false
				continue
			}

			point := Point{
				X:      number(x(at)),
				Y:      number(y(value.Y)),
				Label:  value.Label,
				Detail: value.Detail,
			}

			command := "L"
			if !open {
				command, open = "M", true
			}

			fmt.Fprintf(&path, "%s%s,%s ", command, point.X, point.Y)

			// Only the readings themselves get a dot to hover over. A smoothed
			// line is not a reading and there is nothing to say about a point
			// on it that the line does not already say.
			if !one.Dashed {
				line.Points = append(line.Points, point)
			}
		}

		line.Path = strings.TrimSpace(path.String())
		if line.Path == "" {
			continue
		}

		drawing.Lines = append(drawing.Lines, line)
	}

	if len(drawing.Lines) == 0 {
		drawing.Empty = true
		return drawing
	}

	// The horizontal labels come from the first series, which is the readings.
	// Anything past about ten of them is unreadable at this size, so they are
	// thinned rather than shrunk.
	if len(series) > 0 {
		step := len(series[0].Values) / 8
		if step < 1 {
			step = 1
		}

		for at, value := range series[0].Values {
			if at%step != 0 || value.Label == "" {
				continue
			}

			drawing.XTicks = append(drawing.XTicks, Tick{At: number(x(at)), Label: value.Label})
		}
	}

	for _, at := range steps(low, high, ticks) {
		drawing.YTicks = append(drawing.YTicks, Tick{
			At:    number(y(at)),
			Text:  number(y(at) + 3.5),
			Label: trim(at),
		})
	}

	return drawing
}

// bounds is the lowest and highest value across every series, and how many
// readings the longest of them has.
func bounds(series []Series) (low float64, high float64, count int) {
	low, high = math.Inf(1), math.Inf(-1)

	for _, one := range series {
		if len(one.Values) > count {
			count = len(one.Values)
		}

		for _, value := range one.Values {
			if value.Missing {
				continue
			}

			low = math.Min(low, value.Y)
			high = math.Max(high, value.Y)
		}
	}

	if math.IsInf(low, 1) {
		return 0, 0, 0
	}

	return low, high, count
}

// pad opens the vertical range out a little so the line does not run along the
// edge of the box, and gives a flat series something to sit in the middle of.
func pad(low float64, high float64) (float64, float64) {
	if high-low < 1e-9 {
		if low == 0 {
			return -1, 1
		}

		margin := math.Abs(low) * 0.1
		return low - margin, high + margin
	}

	margin := (high - low) * 0.08

	return low - margin, high + margin
}

// steps picks the horizontal gridlines: round numbers inside the range, at
// roughly the requested count.
func steps(low float64, high float64, want int) []float64 {
	if want < 2 {
		want = 2
	}

	span := high - low
	if span <= 0 {
		return []float64{low}
	}

	// The step is the power of ten below the rough spacing, nudged up to a 2 or
	// a 5 so the labels read as numbers a person would choose.
	rough := span / float64(want)
	magnitude := math.Pow(10, math.Floor(math.Log10(rough)))

	step := magnitude
	for _, multiple := range []float64{1, 2, 5, 10} {
		step = magnitude * multiple
		if step >= rough {
			break
		}
	}

	out := []float64{}
	for at := math.Ceil(low/step) * step; at <= high; at += step {
		out = append(out, at)

		if len(out) > 20 {
			break
		}
	}

	return out
}

// number formats a coordinate. Two decimals is far more than an svg needs and
// far less than a float prints, and it keeps the markup readable.
func number(value float64) string {
	return trimZeros(fmt.Sprintf("%.2f", value))
}

// trim formats an axis label: whole numbers plainly, fractions to one place.
func trim(value float64) string {
	if math.Abs(value-math.Round(value)) < 0.001 {
		return fmt.Sprintf("%.0f", value)
	}

	return trimZeros(fmt.Sprintf("%.2f", value))
}

// trimZeros takes the trailing noughts off a decimal, and the point with them.
func trimZeros(text string) string {
	if !strings.Contains(text, ".") {
		return text
	}

	text = strings.TrimRight(text, "0")

	return strings.TrimSuffix(text, ".")
}
