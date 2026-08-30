package web

import (
	"fmt"
	"time"

	"github.com/Niximacco/car-tracker/internal/chart"
	"github.com/Niximacco/car-tracker/internal/money"
	"github.com/Niximacco/car-tracker/internal/stats"
	"github.com/Niximacco/car-tracker/internal/types"
)

// The three pictures worth drawing, built from a report.
//
// Each one answers a question the table underneath it can only answer by being
// read carefully: is the car getting worse, is fuel getting dearer, and is the
// thing settling down. They are built here rather than in a template because
// the geometry is arithmetic and arithmetic belongs somewhere a test can see
// it - and built on the server rather than in the browser because there is
// nothing to fetch and nothing to wait for.

// MPGChart is every measured tank, with a smoothed line through it.
//
// The dots are the readings and the dashed line is the average of the last
// five. Both are drawn because either alone is misleading: the readings on
// their own are a hedge, and the smoothed line on its own hides how wide the
// spread actually is.
func MPGChart(report stats.Report) chart.Chart {
	readings := chart.Series{Name: "Miles per gallon", Class: "mpg"}
	smoothed := chart.Series{Name: "Five-fill average", Class: "mpg-avg", Dashed: true}

	for _, fill := range report.Fills {
		if !fill.Measured {
			readings.Values = append(readings.Values, chart.Value{Missing: true})
			smoothed.Values = append(smoothed.Values, chart.Value{Missing: true})
			continue
		}

		readings.Values = append(readings.Values, chart.Value{
			Y:     fill.MPG,
			Label: label(fill.On),
			Detail: fmt.Sprintf("%s - %.1f mpg over %s miles on %s gallons",
				pretty(fill.On), fill.MPG, commas(fill.Miles), trimZeros(fmt.Sprintf("%.3f", fill.Gallons))),
		})

		smoothed.Values = append(smoothed.Values, chart.Value{Y: fill.Rolling})
	}

	return chart.Draw([]chart.Series{readings, smoothed}, 5)
}

// PriceChart is what a gallon cost, both ways: what came out of the account and
// what the sign by the road said.
//
// The gap between the two lines is the fuel saver, which is the whole reason
// for drawing both. On a history where the discount runs to a dollar and a half
// a gallon, a single price line is a chart of the wrong number.
func PriceChart(report stats.Report) chart.Chart {
	paid := chart.Series{Name: "Paid a gallon", Class: "paid"}
	pump := chart.Series{Name: "Pump price", Class: "pump", Dashed: true}

	discounted := false

	for _, fill := range report.Fills {
		if fill.PricePerGallonCents <= 0 {
			paid.Values = append(paid.Values, chart.Value{Missing: true})
			pump.Values = append(pump.Values, chart.Value{Missing: true})
			continue
		}

		detail := fmt.Sprintf("%s - %s a gallon at %s",
			pretty(fill.On), money.Format(fill.PricePerGallonCents), stats.StationName(fill.Station))

		if fill.Discounted() {
			discounted = true
			detail += fmt.Sprintf(", %s off", money.Format(fill.FuelSaverCents))
		}

		paid.Values = append(paid.Values, chart.Value{
			Y:      float64(fill.PricePerGallonCents) / 100,
			Label:  label(fill.On),
			Detail: detail,
		})

		pump.Values = append(pump.Values, chart.Value{Y: float64(fill.PumpPriceCents) / 100})
	}

	// A history with no discount in it anywhere would draw the two lines on top
	// of each other, and a legend claiming a distinction that is not there.
	if !discounted {
		return chart.Draw([]chart.Series{paid}, 5)
	}

	return chart.Draw([]chart.Series{paid, pump}, 5)
}

// CostChart is what the car has cost a day, running, since the day it was
// bought.
//
// It is the one chart that is supposed to flatten out. The early part of it is
// steep and meaningless - a month of ownership divided into a set of tyres -
// and watching it settle is watching the number become worth quoting. A big
// service shows up as a step, which is exactly what a big service is.
func CostChart(report stats.Report) chart.Chart {
	running := chart.Series{Name: "Cost a day, running", Class: "cost"}

	for _, fill := range report.Fills {
		if fill.DaysOwned <= 0 || fill.CostPerDayCents <= 0 {
			running.Values = append(running.Values, chart.Value{Missing: true})
			continue
		}

		running.Values = append(running.Values, chart.Value{
			Y:     fill.CostPerDayCents / 100,
			Label: label(fill.On),
			Detail: fmt.Sprintf("%s - %s spent over %d days, %s a day",
				pretty(fill.On), money.Format(fill.RunningCents), fill.DaysOwned,
				money.Format(int64(fill.CostPerDayCents))),
		})
	}

	return chart.Draw([]chart.Series{running}, 4)
}

// label is the axis label for a date: the month and the year, which is as much
// as fits and as much as is wanted. Which day in March 2024 a point is has
// never been the question a four-year chart is answering.
func label(stored string) string {
	if date := parse(stored); !date.IsZero() {
		return date.Format("Jan 06")
	}

	return ""
}

// pretty is the date as the hover text says it.
func pretty(stored string) string {
	if date := parse(stored); !date.IsZero() {
		return date.Format("Jan 2, 2006")
	}

	return stored
}

// parse reads a stored date, so the two label helpers above do not each carry
// their own copy of the layout.
func parse(stored string) time.Time {
	return types.ParseDate(stored)
}
