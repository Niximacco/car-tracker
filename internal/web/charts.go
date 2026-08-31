package web

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Niximacco/car-tracker/internal/chart"
	"github.com/Niximacco/car-tracker/internal/money"
	"github.com/Niximacco/car-tracker/internal/stats"
	"github.com/Niximacco/car-tracker/internal/types"
)

// The pictures worth drawing, built from a report.
//
// Each one answers a question the table underneath it can only answer by being
// read carefully: is the car getting worse, is fuel getting more expensive, is
// the thing settling down, and where does the money actually go. They are built
// here rather than in a template because the geometry is arithmetic and
// arithmetic belongs somewhere a test can see it - and built on the server
// rather than in the browser because there is nothing to fetch and nothing to
// wait for.

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
	paid := chart.Series{Name: "Paid per gallon", Class: "paid"}
	pump := chart.Series{Name: "Pump price", Class: "pump", Dashed: true}

	discounted := false

	for _, fill := range report.Fills {
		if fill.PricePerGallonCents <= 0 {
			paid.Values = append(paid.Values, chart.Value{Missing: true})
			pump.Values = append(pump.Values, chart.Value{Missing: true})
			continue
		}

		detail := fmt.Sprintf("%s - %s per gallon at %s",
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

// CostChart is what the car has cost per day, running, since the day it was
// bought.
//
// It is the one chart that is supposed to flatten out. A big service shows up
// as a step, which is exactly what a big service is.
//
// The settling-in period at the front is left off. A single set of tires
// divided by three weeks of ownership is a hundred dollars a day, and drawing
// it puts the whole of the last four years in the bottom eighth of the box
// where none of it can be read. Those readings are not wrong and they are not
// dropped quietly: the chart carries the count, and the page says so.
func CostChart(report stats.Report) chart.Chart {
	running := chart.Series{Name: "Cost per day, running", Class: "cost"}

	for _, fill := range report.Fills {
		if fill.DaysOwned <= 0 || fill.CostPerDayCents <= 0 {
			running.Values = append(running.Values, chart.Value{Missing: true})
			continue
		}

		running.Values = append(running.Values, chart.Value{
			Y:     fill.CostPerDayCents / 100,
			Label: label(fill.On),
			Detail: fmt.Sprintf("%s - %s spent over %d days, %s per day",
				pretty(fill.On), money.Format(fill.RunningCents), fill.DaysOwned,
				money.Format(int64(fill.CostPerDayCents))),
		})
	}

	return settle(running)
}

// PerMileChart is the same running total against distance rather than time.
//
// It is the honest companion to the per-day line, because the per-day one moves
// when your commute does. Two months of working from home lower the cost per
// day of a car that has become no cheaper per mile, and only having both lines
// tells you which of those happened.
func PerMileChart(report stats.Report) chart.Chart {
	running := chart.Series{Name: "Cost per mile, running", Class: "permile"}

	for _, fill := range report.Fills {
		if fill.RunningMiles <= 0 || fill.CostPerMileCents <= 0 {
			running.Values = append(running.Values, chart.Value{Missing: true})
			continue
		}

		running.Values = append(running.Values, chart.Value{
			Y:     fill.CostPerMileCents / 100,
			Label: label(fill.On),
			Detail: fmt.Sprintf("%s - %s spent over %s miles, $%.3f per mile",
				pretty(fill.On), money.Format(fill.RunningCents), commas(fill.RunningMiles),
				fill.CostPerMileCents/100),
		})
	}

	return settle(running)
}

// SpendChart is what went out each calendar month, fuel against the shop.
//
// The two lines are deliberately different shapes. Fuel is a low steady hum
// that tracks how much you drove and what the market did; the shop is flat at
// nothing for months and then spikes. Averaging them into one line hides the
// only interesting thing about the second one.
func SpendChart(report stats.Report) chart.Chart {
	fuel := chart.Series{Name: "Fuel", Class: "fuel"}
	shop := chart.Series{Name: "Maintenance", Class: "shop"}

	for _, month := range report.Timeline {
		fuel.Values = append(fuel.Values, chart.Value{
			Y:     month.FuelDollars(),
			Label: month.Label,
			Detail: fmt.Sprintf("%s - %s of fuel over %s fill-ups, %s at the shop",
				monthly(month), money.Format(month.FuelCents), commas(month.Fills),
				money.Format(month.ServiceCents)),
		})

		shop.Values = append(shop.Values, chart.Value{
			Y:     month.ServiceDollars(),
			Label: month.Label,
			Detail: fmt.Sprintf("%s - %s at the shop over %s visits",
				monthly(month), money.Format(month.ServiceCents), commas(month.Visits)),
		})
	}

	// A history with nothing but fuel in it would draw the shop line flat along
	// the bottom, and a legend claiming a distinction that is not there.
	if report.Shop.Visits == 0 {
		return chart.Draw([]chart.Series{fuel}, 4)
	}

	return chart.Draw([]chart.Series{fuel, shop}, 4)
}

// MilesChart is how far the car went each calendar month.
//
// It counts measured miles, which is the same distance the mpg is worked out
// over, so a month with a partial fill in it reads low. That is the trade for
// having one distance on the site rather than two that disagree.
func MilesChart(report stats.Report) chart.Chart {
	driven := chart.Series{Name: "Miles", Class: "miles"}

	for _, month := range report.Timeline {
		detail := fmt.Sprintf("%s - %s miles over %s fill-ups",
			monthly(month), commas(month.Miles), commas(month.Fills))

		if month.MPG > 0 {
			detail += fmt.Sprintf(", %.1f mpg", month.MPG)
		}

		driven.Values = append(driven.Values, chart.Value{
			Y:      float64(month.Miles),
			Label:  month.Label,
			Detail: detail,
		})
	}

	return chart.Draw([]chart.Series{driven}, 4)
}

// TotalChart is everything ever spent on the car, adding up.
//
// It only ever goes up, which is the point: the slope is what the car costs to
// run, and the gap that opens between the two lines is every invoice you have
// paid. A line that steepens is a car getting more expensive, and that is far
// easier to see here than in a column of monthly totals.
func TotalChart(report stats.Report) chart.Chart {
	everything := chart.Series{Name: "Everything", Class: "running"}
	fuel := chart.Series{Name: "Fuel alone", Class: "fuel", Dashed: true}

	for _, fill := range report.Fills {
		if fill.RunningCents <= 0 {
			everything.Values = append(everything.Values, chart.Value{Missing: true})
			fuel.Values = append(fuel.Values, chart.Value{Missing: true})
			continue
		}

		everything.Values = append(everything.Values, chart.Value{
			Y:     float64(fill.RunningCents) / 100,
			Label: label(fill.On),
			Detail: fmt.Sprintf("%s - %s spent in all, of which %s is fuel",
				pretty(fill.On), money.Format(fill.RunningCents), money.Format(fill.RunningFuelCents)),
		})

		fuel.Values = append(fuel.Values, chart.Value{Y: float64(fill.RunningFuelCents) / 100})
	}

	if report.Shop.Visits == 0 {
		return chart.Draw([]chart.Series{everything}, 4)
	}

	return chart.Draw([]chart.Series{everything, fuel}, 4)
}

// settle draws a running series with its settling-in period left off the front.
//
// A running average over a short span is arithmetic rather than information: a
// hundred dollars divided by the four days you had owned the car is twenty-five
// dollars a day, and it is true and it means nothing. Left in, it sets the top
// of the scale and squashes every honest reading after it into a band a few
// pixels high.
//
// Only the front is trimmed, and only while the readings are outside the band
// the rest of the column sits in. A step later on is a real event - a
// transmission, a set of tires - and dropping those would be dropping the
// thing the chart is for.
func settle(series chart.Series) chart.Chart {
	from, dropped := settled(series.Values)

	series.Values = series.Values[from:]

	drawing := chart.Draw([]chart.Series{series}, 4)
	drawing.Trimmed = dropped

	return drawing
}

// settled is where a running series stops being nonsense: the first reading
// inside the band the column as a whole sits in, and how many plotted readings
// that leaves off.
//
// Both ends of the band count, because a running figure does not always settle
// from above. Cost per day starts enormous - a tank of gas divided by the four
// days you had owned the car - while cost per mile starts at almost nothing,
// because the odometer has been counting since the day of purchase and the
// spending has only been counting since the first logged fill. Either way the
// first few readings are arithmetic about a short span rather than facts about
// a car, and either way they set the scale and flatten everything after them.
//
// It refuses to trim more than the first fifth of the history. If a fifth of
// the readings are still outside the band there is something to see in them,
// and a picture that has thrown away a fifth of its data to look tidy is worse
// than one that is hard to read.
func settled(values []chart.Value) (from int, dropped int) {
	numbers := make([]float64, 0, len(values))

	for _, value := range values {
		if !value.Missing {
			numbers = append(numbers, value.Y)
		}
	}

	low, high, ok := stats.Fences(numbers)
	if !ok {
		return 0, 0
	}

	outside := func(value chart.Value) bool {
		return value.Missing || value.Y < low || value.Y > high
	}

	limit := len(values) / 5

	for from < limit && outside(values[from]) {
		if !values[from].Missing {
			dropped++
		}

		from++
	}

	return from, dropped
}

// monthly is a calendar month as the hover text says it: "March 2024".
func monthly(month stats.Period) string {
	return month.Month.String() + " " + strconv.Itoa(month.Year)
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
