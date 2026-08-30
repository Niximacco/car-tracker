package chart

import (
	"fmt"
	"strings"
	"testing"
)

func series(values ...float64) Series {
	one := Series{Name: "test", Class: "mpg"}

	for _, value := range values {
		one.Values = append(one.Values, Value{Y: value, Label: "x", Detail: "a point"})
	}

	return one
}

func TestAChartIsDrawnInsideItsBox(t *testing.T) {
	drawing := Draw([]Series{series(30, 35, 32, 38, 31)}, 5)

	if drawing.Empty {
		t.Fatal("five readings produced an empty chart")
	}

	if len(drawing.Lines) != 1 {
		t.Fatalf("%d lines, want 1", len(drawing.Lines))
	}

	if got := len(drawing.Lines[0].Points); got != 5 {
		t.Errorf("%d points plotted, want 5", got)
	}

	if len(drawing.YTicks) < 2 {
		t.Errorf("%d gridlines, want an axis worth reading", len(drawing.YTicks))
	}

	// Everything has to land inside the viewBox, or the picture is clipped.
	for _, point := range drawing.Lines[0].Points {
		if !within(point.X, 0, Width) || !within(point.Y, 0, Height) {
			t.Errorf("a point landed outside the box at %s,%s", point.X, point.Y)
		}
	}

	// Coordinates are strings on purpose: a float rendered by a template comes
	// out as 240.00000000000003 often enough to matter.
	for _, point := range drawing.Lines[0].Points {
		if strings.Count(point.X, ".") > 1 || len(point.X) > 8 {
			t.Errorf("a coordinate is not a tidy number: %q", point.X)
		}
	}
}

// A gap in the data breaks the line rather than being drawn straight through,
// because a line across a gap is a claim that nothing happened there.
func TestAGapBreaksTheLine(t *testing.T) {
	one := series(30, 35, 32, 38, 31)
	one.Values[2].Missing = true

	drawing := Draw([]Series{one}, 5)

	if len(drawing.Lines[0].Points) != 4 {
		t.Errorf("%d points plotted, want 4 - the missing one was drawn", len(drawing.Lines[0].Points))
	}

	// Two strokes: one before the gap and one after, so two move commands.
	if got := strings.Count(drawing.Lines[0].Path, "M"); got != 2 {
		t.Errorf("the path has %d strokes, want 2 either side of the gap: %s", got, drawing.Lines[0].Path)
	}
}

// A smoothed line is not a reading, so there is nothing to hover over on it.
func TestASmoothedLineHasNoPoints(t *testing.T) {
	smoothed := series(30, 31, 32)
	smoothed.Dashed = true

	drawing := Draw([]Series{series(30, 35, 32), smoothed}, 5)

	if len(drawing.Lines) != 2 {
		t.Fatalf("%d lines, want 2", len(drawing.Lines))
	}

	if len(drawing.Lines[1].Points) != 0 {
		t.Error("the smoothed line carries hover points of its own")
	}
}

// One reading is not a chart, and nor is none. Both have to come back as
// something the page can say "not yet" about rather than an empty axis.
func TestTooLittleToPlot(t *testing.T) {
	for _, one := range []Series{{}, series(34)} {
		if drawing := Draw([]Series{one}, 5); !drawing.Empty {
			t.Errorf("Draw(%d readings) produced a chart", len(one.Values))
		}
	}

	// A series that is entirely gaps is the same case.
	gaps := series(30, 31, 32)
	for at := range gaps.Values {
		gaps.Values[at].Missing = true
	}

	if !Draw([]Series{gaps}, 5).Empty {
		t.Error("a series of nothing but gaps produced a chart")
	}
}

// A flat line has no range to scale against, and dividing by that range is the
// obvious way to fill a page with NaN.
func TestAFlatSeriesDoesNotDivideByNothing(t *testing.T) {
	drawing := Draw([]Series{series(34, 34, 34)}, 5)

	if drawing.Empty {
		t.Fatal("a flat series produced no chart")
	}

	for _, point := range drawing.Lines[0].Points {
		if strings.Contains(point.Y, "NaN") || strings.Contains(point.Y, "Inf") {
			t.Fatalf("a flat series produced %q", point.Y)
		}
	}
}

// The axis labels have to be numbers a person would choose, or the chart reads
// as noise with a scale beside it.
func TestTheAxisPicksRoundNumbers(t *testing.T) {
	drawing := Draw([]Series{series(28.4, 41.7, 33.2, 36.9, 30.1)}, 5)

	for _, tick := range drawing.YTicks {
		if strings.Contains(tick.Label, ".") && len(tick.Label) > 5 {
			t.Errorf("an axis label is not a round number: %q", tick.Label)
		}
	}

	if len(drawing.XTicks) == 0 {
		t.Error("the chart has no labels along the bottom")
	}
}

// Eight labels is about what fits; a four year history has sixty-seven points.
func TestTheHorizontalLabelsAreThinnedRatherThanShrunk(t *testing.T) {
	values := make([]float64, 0, 67)
	for at := 0; at < 67; at++ {
		values = append(values, 30+float64(at%7))
	}

	drawing := Draw([]Series{series(values...)}, 5)

	if len(drawing.XTicks) > 12 {
		t.Errorf("%d labels along the bottom of a 67-point chart", len(drawing.XTicks))
	}
}

func within(coordinate string, low float64, high float64) bool {
	var value float64
	if _, err := fmt.Sscanf(coordinate, "%g", &value); err != nil {
		return false
	}

	return value >= low && value <= high
}
