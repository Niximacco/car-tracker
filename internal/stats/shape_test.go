package stats

import (
	"math"
	"testing"
	"time"

	"github.com/Niximacco/car-tracker/internal/types"
)

// close reports whether two floats agree to within a tolerance, because every
// figure in this file is a division and none of them lands on a round number.
func close(got float64, want float64, within float64) bool {
	return math.Abs(got-want) <= within
}

// A month nothing happened in is still a month.
//
// This is the whole reason the timeline exists rather than the charts grouping
// the fills themselves. A line that skips an empty month draws a flat stretch
// where there should be a dip, which turns a fortnight of not driving into a
// fortnight of driving normally.
func TestTheTimelineKeepsTheMonthsNothingHappenedIn(t *testing.T) {
	fills := []types.Fillup{
		fill("2024-01-05", 10300, 10, 300),
		// Nothing at all in February or March.
		fill("2024-04-10", 11300, 12, 320),
	}

	report := Analyze(wagon(), fills, nil, now)

	if got := len(report.Timeline); got != 4 {
		t.Fatalf("the timeline has %d months, want 4 - January through April", got)
	}

	for at, want := range []string{"2024-01", "2024-02", "2024-03", "2024-04"} {
		if got := report.Timeline[at].Key; got != want {
			t.Errorf("month %d is %q, want %q", at, got, want)
		}
	}

	if february := report.Timeline[1]; february.Any() || february.TotalCents != 0 {
		t.Errorf("February reads as %+v, want an empty month", february)
	}

	if got := report.Timeline[0].FuelCents; got != 3000 {
		t.Errorf("January cost %d cents, want 3000", got)
	}
}

// A month's mpg is worked out over the tanks that can carry one, the same way a
// year's is. Dividing measured miles by every gallon bought that month would
// drag any month with a splash-and-go in it downwards.
func TestAMonthsMPGOnlyCountsTheTanksThatCarryOne(t *testing.T) {
	fills := []types.Fillup{
		fill("2024-01-05", 10000, 10, 300),
		fill("2024-01-19", 10300, 10, 300), // 30 mpg, measured
		fill("2024-01-26", 10400, 20, 300), // a partial: gallons, no mpg
	}

	fills[2].Partial = true

	report := Analyze(wagon(), fills, nil, now)

	january := report.Timeline[0]

	if !close(january.MPG, 30, 0.01) {
		t.Errorf("January mpg = %v, want 30 - the partial's gallons do not belong in it", january.MPG)
	}

	if !close(january.Gallons, 40, 0.01) {
		t.Errorf("January gallons = %v, want 40 - you bought all of them", january.Gallons)
	}
}

// The spread is the part of the story the average leaves out.
//
// Two cars can average the same and be nothing alike, so the median, the
// deviation and the band eight tanks in ten fall inside all have to come off
// the readings rather than off the total.
func TestTheSpreadDescribesTheTanksRatherThanTheirAverage(t *testing.T) {
	// Six tanks alternating either side of 30, on ten gallons each: 300, 270,
	// 330, 300, 270 and 330 miles.
	fills := []types.Fillup{
		fill("2024-01-01", 10000, 10, 300),
		fill("2024-01-11", 10300, 10, 300),
		fill("2024-01-21", 10570, 10, 300),
		fill("2024-02-01", 10900, 10, 300),
		fill("2024-02-11", 11200, 10, 300),
		fill("2024-02-21", 11470, 10, 300),
		fill("2024-03-01", 11800, 10, 300),
	}

	shape := Analyze(wagon(), fills, nil, now).Spread

	if !shape.Ready {
		t.Fatal("six measured tanks did not produce a spread")
	}

	if shape.Fills != 6 {
		t.Errorf("the spread rests on %d tanks, want 6", shape.Fills)
	}

	if !close(shape.MPGMean, 30, 0.01) {
		t.Errorf("mean = %v, want 30", shape.MPGMean)
	}

	// Two tanks at 27, two at 30 and two at 33. The sample deviation divides by
	// five rather than six, which puts it at 2.68 - nearly a tenth of the mean,
	// and a real fact about how this car gets driven.
	if !close(shape.MPGStdDev, 2.683, 0.01) {
		t.Errorf("standard deviation = %v, want about 2.68", shape.MPGStdDev)
	}

	if !close(shape.MPGVariation, 8.94, 0.05) {
		t.Errorf("variation = %v%%, want about 8.9%%", shape.MPGVariation)
	}

	if shape.MPGLow >= shape.MPGMedian || shape.MPGHigh <= shape.MPGMedian {
		t.Errorf("the 10th to 90th band (%v to %v) does not straddle the median (%v)",
			shape.MPGLow, shape.MPGHigh, shape.MPGMedian)
	}
}

// Two readings have a standard deviation and it is half the distance between
// them dressed up as statistics. The page has to be able to leave the block out
// rather than print a number that sounds like it knows something.
func TestASpreadNeedsMoreThanTwoReadings(t *testing.T) {
	fills := []types.Fillup{
		fill("2024-01-01", 10000, 10, 300),
		fill("2024-01-11", 10300, 10, 300),
	}

	if shape := Analyze(wagon(), fills, nil, now).Spread; shape.Ready {
		t.Error("one measured tank produced a spread")
	}
}

// The slope is the point of the trend: a car quietly losing half a mile per
// gallon a year is invisible in a column that bounces four either side of the
// average every winter.
func TestTheTrendFindsASlopeThroughTheNoise(t *testing.T) {
	// Ten tanks on ten gallons, falling steadily from 320 miles to 275 over
	// nine months: about six miles per gallon a year, downhill.
	fills := []types.Fillup{}

	odometer := 10000
	miles := []int{320, 315, 310, 305, 300, 295, 290, 285, 280, 275}
	dates := []string{
		"2023-09-01", "2023-10-01", "2023-11-01", "2023-12-01", "2024-01-01",
		"2024-02-01", "2024-03-01", "2024-04-01", "2024-05-01", "2024-06-01",
	}

	fills = append(fills, fill("2023-08-01", odometer, 10, 300))

	for at, gap := range miles {
		odometer += gap
		fills = append(fills, fill(dates[at], odometer, 10, 300))
	}

	moving := Analyze(wagon(), fills, nil, now).Trend

	if !moving.Ready {
		t.Fatal("ten measured tanks did not produce a trend")
	}

	if moving.MPGPerYear >= 0 {
		t.Errorf("slope = %v mpg a year, want a negative one - the car is getting worse",
			moving.MPGPerYear)
	}

	if !close(moving.MPGPerYear, -6, 1) {
		t.Errorf("slope = %v mpg a year, want about -6", moving.MPGPerYear)
	}
}

// The last twelve months against the twelve before them only means something
// when there were twelve before them. A car bought fourteen months ago has two
// months in its prior window, and holding a full year against two months
// reports a collapse in spending that is really a shortage of history.
func TestTheYearOnYearComparisonWaitsForAYearToCompareAgainst(t *testing.T) {
	vehicle := wagon()
	vehicle.PurchasedOn = "2024-01-01"

	fills := []types.Fillup{
		fill("2024-01-05", 10300, 10, 300),
		fill("2024-02-05", 10600, 10, 300),
		fill("2024-03-05", 10900, 10, 300),
	}

	if moving := Analyze(vehicle, fills, nil, now).Trend; moving.HasPrior {
		t.Error("five months of history produced a year-on-year comparison")
	}
}

// The monthly bill is what somebody actually means when they ask what a car
// costs, so it is divided by the days the window really covers rather than by
// twelve - otherwise a car bought seven months ago reports five months of
// nothing dragging its average down.
func TestTheRecentMonthlyCostUsesTheDaysItActuallyCovers(t *testing.T) {
	vehicle := wagon()
	vehicle.PurchasedOn = "2024-03-01"

	// Three months to the day, at $100 of fuel a month.
	fills := []types.Fillup{
		fill("2024-03-01", 10000, 10, 1000),
		fill("2024-04-01", 10300, 10, 1000),
		fill("2024-05-01", 10600, 10, 1000),
		fill("2024-06-01", 10900, 10, 1000),
	}

	moving := Analyze(vehicle, fills, nil, now).Trend

	if !moving.RecentReady {
		t.Fatal("three months of history did not produce a monthly figure")
	}

	if got := moving.RecentDays; got != 92 {
		t.Errorf("the recent window covers %d days, want 92 - March to June", got)
	}

	// $400 over 92 days is about $132 a month, not the $33 that dividing by
	// twelve would give.
	if !close(moving.RecentPerMonth, 13224, 100) {
		t.Errorf("recent cost = %v cents a month, want about 13224", moving.RecentPerMonth)
	}
}

// The habits block is a fact about your week rather than about the car, and it
// is the only thing on the site that reads the clock a fill-up carries.
func TestHabitsCountTheDaysAndOnlyTheFillsThatCarryAClock(t *testing.T) {
	// Four Mondays and one Saturday. 2024-01-01 was a Monday.
	fills := []types.Fillup{
		fill("2024-01-01", 10000, 10, 300),
		fill("2024-01-08", 10300, 10, 300),
		fill("2024-01-15", 10600, 10, 300),
		fill("2024-01-22", 10900, 10, 300),
		fill("2024-01-27", 11200, 10, 300),
	}

	fills[0].At = "07:30"
	fills[1].At = "18:15"

	found := Analyze(wagon(), fills, nil, now).Habits

	if !found.Ready {
		t.Fatal("five fills did not produce any habits")
	}

	if found.BusiestDay != "Monday" {
		t.Errorf("the busiest day is %q, want Monday", found.BusiestDay)
	}

	if !close(found.WeekendShare, 20, 0.01) {
		t.Errorf("weekend share = %v%%, want 20%%", found.WeekendShare)
	}

	if found.Clocked != 2 {
		t.Errorf("%d fills carry a clock, want 2 - the other three are left out rather than counted as midnight", found.Clocked)
	}

	// Monday first, so the slice reads as a working week rather than in Go's
	// order.
	if found.Days[0].Day != time.Monday {
		t.Errorf("the week starts on %v, want Monday", found.Days[0].Day)
	}

	if found.Days[0].Fills != 4 {
		t.Errorf("Monday has %d fills, want 4", found.Days[0].Fills)
	}
}

// The fences are what a chart uses to decide a reading is too far outside the
// rest to draw to scale. They have to catch the settling-in period of a running
// average without catching the ordinary spread around it.
func TestFencesCatchTheOutlierAndLeaveTheCrowdAlone(t *testing.T) {
	values := []float64{40, 12, 9, 8.5, 8, 8.2, 7.9, 8.1, 8, 7.8}

	low, high, ok := Fences(values)
	if !ok {
		t.Fatal("ten readings did not produce a pair of fences")
	}

	if 40 <= high {
		t.Errorf("the high fence is %v, which does not exclude the 40 that starts the series", high)
	}

	for _, value := range []float64{8, 8.2, 7.9, 9} {
		if value < low || value > high {
			t.Errorf("%v falls outside the fences (%v to %v) and should not", value, low, high)
		}
	}
}

// A column with no spread in it has no fences worth drawing, and neither does
// one with three readings in it. Both come back as "no answer" rather than as a
// band that would throw away half the data.
func TestFencesRefuseToGuess(t *testing.T) {
	if _, _, ok := Fences([]float64{1, 2, 3}); ok {
		t.Error("three readings produced fences")
	}

	if _, _, ok := Fences([]float64{5, 5, 5, 5, 5}); ok {
		t.Error("a flat column produced fences")
	}
}

// Gallons per hundred miles is the fuel figure the money scales with, and the
// carbon is the one number here taken on trust from somebody else. Both are
// pure arithmetic on figures that are already tested, so what matters is that
// they are wired to the right ones.
func TestTheDerivedFuelFiguresFollowTheMeasuredAverage(t *testing.T) {
	fills := []types.Fillup{
		fill("2024-01-01", 10000, 10, 400),
		fill("2024-01-11", 10300, 10, 400), // 30 mpg on ten gallons
	}

	summary := Analyze(wagon(), fills, nil, now).Summary

	if !close(summary.GallonsPer100Miles, 3.333, 0.01) {
		t.Errorf("gallons per 100 miles = %v, want about 3.33 at 30 mpg", summary.GallonsPer100Miles)
	}

	// Twenty gallons at four dollars.
	if !close(summary.CO2Pounds, 392, 0.5) {
		t.Errorf("carbon = %v lb, want 392 - twenty gallons at 19.6", summary.CO2Pounds)
	}

	// 13.5 gallons at 30 mpg.
	if summary.TankRange != 405 {
		t.Errorf("tank range = %d miles, want 405", summary.TankRange)
	}
}

// The two shares are what the cost-per-month block splits itself on, so they
// have to add up to the whole of what was spent.
func TestFuelAndShopSharesAddUpToEverything(t *testing.T) {
	fills := []types.Fillup{
		fill("2024-01-01", 10000, 10, 300),
		fill("2024-01-11", 10300, 10, 300),
	}

	visits := []types.Service{
		{ID: "2024-01-15-10400", On: "2024-01-15", Odometer: 10400, CostCents: 6000, Work: "Oil change"},
	}

	summary := Analyze(wagon(), fills, visits, now).Summary

	if got := summary.FuelShare + summary.ServiceShare; !close(got, 100, 0.01) {
		t.Errorf("the two shares add up to %v%%, want 100%%", got)
	}

	if !close(summary.FuelShare, 50, 0.01) {
		t.Errorf("fuel share = %v%%, want 50%% - $60 of fuel against $60 at the shop", summary.FuelShare)
	}
}
