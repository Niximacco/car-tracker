// The shape of the history, as opposed to its totals.
//
// Everything in stats.go answers "how much": how many gallons, how many
// dollars, how many miles a day. This file answers the questions a column of
// totals cannot. How the numbers are spread, which way they are moving, which
// months they land in, and what time of day you actually stop for fuel.
//
// None of it changes a figure anywhere else. A median cannot disagree with a
// total, and nothing here is stored, so the worst a mistake in this file can do
// is draw a chart nobody believes rather than quietly alter what the car cost.
package stats

import (
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/Niximacco/car-tracker/internal/types"
)

// ------------------------------------------------------------- timeline ----

// Period is one calendar month of the history.
//
// A month with nothing in it is still a Period, with zeroes in it. That is the
// whole reason this exists rather than the charts grouping the fills
// themselves: a month you did not drive is a fact about the year, and a line
// that skips it draws a flat stretch where there should be a dip.
type Period struct {
	// Key is "2024-03", which sorts correctly as text and is what the months
	// are bucketed under. Label is "Mar 24", which is what an axis says.
	Key   string
	Label string
	Year  int
	Month time.Month

	Fills   int
	Visits  int
	Miles   int
	Gallons float64
	MPG     float64

	FuelCents     int64
	ServiceCents  int64
	TotalCents    int64
	SavedCents    int64
	AvgPriceCents float64
}

// Any reports whether anything at all happened in this month, so a table can
// leave the empty ones out while a chart keeps them.
func (p Period) Any() bool {
	return p.Fills > 0 || p.Visits > 0
}

// FuelDollars, ServiceDollars and TotalDollars are the month's spending as
// plain numbers, because a chart plots dollars and every sum stays in cents.
func (p Period) FuelDollars() float64 { return float64(p.FuelCents) / 100 }

func (p Period) ServiceDollars() float64 { return float64(p.ServiceCents) / 100 }

func (p Period) TotalDollars() float64 { return float64(p.TotalCents) / 100 }

// timeline buckets the whole history by calendar month, oldest first, with
// every month between the first entry and the last present whether or not
// anything happened in it.
func timeline(fills []Fill, visits []types.Service) []Period {
	index := map[string]*Period{}

	var first, last time.Time

	span := func(date time.Time) {
		if first.IsZero() || date.Before(first) {
			first = date
		}

		if date.After(last) {
			last = date
		}
	}

	at := func(date time.Time) *Period {
		key := monthKey(date)

		found, seen := index[key]
		if !seen {
			found = &Period{
				Key:   key,
				Label: date.Format("Jan 06"),
				Year:  date.Year(),
				Month: date.Month(),
			}

			index[key] = found
		}

		return found
	}

	// The measured miles and gallons are kept beside the bucket rather than in
	// it, for the same reason a year's mpg is: a month with a partial fill in
	// it would otherwise divide its measured distance by gallons that never
	// carried one.
	measured := map[string][2]float64{}

	for _, fill := range fills {
		date := fill.Date()
		if date.IsZero() {
			continue
		}

		span(date)

		month := at(date)
		month.Fills++
		month.Gallons += fill.Gallons
		month.FuelCents += fill.CostCents
		month.SavedCents += fill.SavedCents

		if fill.Measured {
			carried := measured[month.Key]
			measured[month.Key] = [2]float64{carried[0] + float64(fill.Miles), carried[1] + fill.Gallons}

			month.Miles += fill.Miles
		}
	}

	for _, visit := range visits {
		date := visit.Date()
		if date.IsZero() {
			continue
		}

		span(date)

		month := at(date)
		month.Visits++
		month.ServiceCents += visit.CostCents
	}

	if first.IsZero() {
		return nil
	}

	// Walk every month from the first to the last, so the gaps are in the
	// series rather than missing from it.
	out := []Period{}

	cursor := time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, time.UTC)

	for !cursor.After(end) {
		key := monthKey(cursor)

		month := Period{
			Key:   key,
			Label: cursor.Format("Jan 06"),
			Year:  cursor.Year(),
			Month: cursor.Month(),
		}

		if found, seen := index[key]; seen {
			month = *found
		}

		if carried := measured[key]; carried[1] > 0 {
			month.MPG = carried[0] / carried[1]
		}

		if month.Gallons > 0 {
			month.AvgPriceCents = float64(month.FuelCents) / month.Gallons
		}

		month.TotalCents = month.FuelCents + month.ServiceCents

		out = append(out, month)
		cursor = cursor.AddDate(0, 1, 0)
	}

	return out
}

// extremeMonths is the month that cost the most and the month that covered the
// most ground.
//
// Both come back as whole Periods rather than as a label and a number, so a
// page can say what else was going on in the month it names - a month that cost
// six hundred dollars because the transmission went reads differently from one
// that cost six hundred dollars in gas.
func extremeMonths(timeline []Period) (costliest Period, farthest Period) {
	for _, month := range timeline {
		if month.TotalCents > costliest.TotalCents {
			costliest = month
		}

		if month.Miles > farthest.Miles {
			farthest = month
		}
	}

	return costliest, farthest
}

// monthKey is the string a month is bucketed under: "2024-03".
func monthKey(date time.Time) string {
	return strconv.Itoa(date.Year()) + "-" + twoDigits(int(date.Month()))
}

// twoDigits pads a month number so the keys sort as text.
func twoDigits(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}

	return strconv.Itoa(value)
}

// --------------------------------------------------------------- spread ----

// Spread is the shape of the two columns that have one worth looking at: what a
// tank returns, and what a gallon costs.
//
// An average cannot tell a car that returns 32 every time from one that
// alternates 26 and 38, and those are different cars to own - the first is
// predictable, and the second is telling you something about how it gets
// driven. The standard deviation is the honest answer, and the percentage
// beside it is the one that can be compared against another car.
type Spread struct {
	Ready bool
	Fills int

	MPGMedian float64
	MPGMean   float64
	MPGStdDev float64
	// MPGVariation is the standard deviation as a percentage of the mean. Under
	// about five percent is a car driven the same way every week; over fifteen
	// is a car doing two quite different jobs.
	MPGVariation float64
	// The tenth and ninetieth percentiles, which are the honest version of
	// "best" and "worst": the single best tank in four years is a fluke, and
	// the band eight tanks in ten fall inside is not.
	MPGLow  float64
	MPGHigh float64
	MPGIQR  float64

	PriceReady  bool
	PriceMedian float64
	PriceStdDev float64
	PriceLow    float64
	PriceHigh   float64
	// PriceNow is the most recent price paid, and PriceVsAverage is how far it
	// sits above or below the lifetime average, as a percentage.
	PriceNow       float64
	PriceVsAverage float64
}

// spread works out the distribution of the measured tanks and of the prices
// paid.
func spread(fills []Fill) Spread {
	shape := Spread{}

	economy := []float64{}
	prices := []float64{}

	var latest float64

	for _, fill := range fills {
		if fill.Measured {
			economy = append(economy, fill.MPG)
		}

		if fill.PricePerGallonCents > 0 {
			price := float64(fill.PricePerGallonCents) / 100

			prices = append(prices, price)
			latest = price
		}
	}

	// Three readings is the least that has a spread worth quoting. Two have a
	// standard deviation, and it is half the distance between them dressed up
	// as statistics.
	if len(economy) >= 3 {
		average := mean(economy)
		spreadOf := deviation(economy)

		sort.Float64s(economy)

		shape.Ready = true
		shape.Fills = len(economy)
		shape.MPGMean = average
		shape.MPGStdDev = spreadOf
		shape.MPGMedian = quantile(economy, 0.5)
		shape.MPGLow = quantile(economy, 0.1)
		shape.MPGHigh = quantile(economy, 0.9)
		shape.MPGIQR = quantile(economy, 0.75) - quantile(economy, 0.25)

		if average > 0 {
			shape.MPGVariation = spreadOf / average * 100
		}
	}

	if len(prices) >= 3 {
		average := mean(prices)

		shape.PriceStdDev = deviation(prices)

		sort.Float64s(prices)

		shape.PriceReady = true
		shape.PriceMedian = quantile(prices, 0.5)
		shape.PriceLow = quantile(prices, 0.1)
		shape.PriceHigh = quantile(prices, 0.9)
		shape.PriceNow = latest

		if average > 0 {
			shape.PriceVsAverage = (latest - average) / average * 100
		}
	}

	return shape
}

// ---------------------------------------------------------------- trend ----

// Trend is which way the history is moving: a straight line fitted through
// every measured tank, and the last twelve months held against the twelve
// before them.
//
// The slope is worth more than it looks. A car losing half a mile per gallon a
// year is a car with something slowly going wrong with it, and that is
// invisible in a column of tanks that bounce four miles per gallon either side
// of the average every winter.
type Trend struct {
	Ready bool
	// MPGPerYear is the slope of a least-squares line through the measured
	// tanks, in miles per gallon gained or lost per year. Fills is how many
	// readings it rests on.
	MPGPerYear float64
	Fills      int

	// PricePerYear is the same line through what was paid per gallon, in
	// dollars a year. It is mostly a chart of the fuel market rather than of
	// the car, which is exactly why it is worth having beside the spending.
	PriceReady   bool
	PricePerYear float64

	// The last twelve months against the twelve before them. This is the
	// comparison that answers "is it costing more than it used to", which the
	// year-by-year table only answers in January.
	HasPrior bool

	RecentFuelCents    int64
	RecentServiceCents int64
	RecentTotalCents   int64
	RecentMiles        int
	RecentMPG          float64

	// The recent window read as a monthly bill, which is the figure somebody
	// actually wants when they ask what the car costs. It is divided by the
	// days the window really covers rather than by twelve, so a car bought
	// seven months ago reports what seven months cost per month instead of
	// five months of nothing dragging it down.
	RecentDays            int
	RecentFuelPerMonth    float64
	RecentServicePerMonth float64
	RecentPerMonth        float64
	// RecentReady is whether that window holds enough to be worth quoting. Two
	// fill-ups in a fortnight can be scaled up to a monthly figure, and the
	// answer will be nonsense.
	RecentReady bool

	PriorTotalCents int64
	PriorMiles      int
	PriorMPG        float64

	SpendChange float64
	MilesChange float64
	MPGChange   float64
}

// trend fits the lines and compares the two windows.
func trend(fills []Fill, visits []types.Service, now time.Time) Trend {
	moving := Trend{}

	// The x axis of both fits is years since the first fill rather than the
	// reading's position in the history. Fills are not evenly spaced, and a
	// slope quoted per fill is a slope in a unit nobody thinks in.
	var origin time.Time

	for _, fill := range fills {
		if date := fill.Date(); !date.IsZero() {
			origin = date
			break
		}
	}

	if origin.IsZero() {
		return moving
	}

	years := func(date time.Time) float64 {
		return float64(types.DaysBetween(origin, date)) / 365.25
	}

	economyX, economyY := []float64{}, []float64{}
	priceX, priceY := []float64{}, []float64{}

	for _, fill := range fills {
		date := fill.Date()
		if date.IsZero() {
			continue
		}

		if fill.Measured {
			economyX = append(economyX, years(date))
			economyY = append(economyY, fill.MPG)
		}

		if fill.PricePerGallonCents > 0 {
			priceX = append(priceX, years(date))
			priceY = append(priceY, float64(fill.PricePerGallonCents)/100)
		}
	}

	// Four readings for a slope. Three points with a straight line through them
	// is a shape, not a trend.
	if len(economyY) >= 4 {
		if slope, ok := fit(economyX, economyY); ok {
			moving.Ready = true
			moving.MPGPerYear = slope
			moving.Fills = len(economyY)
		}
	}

	if len(priceY) >= 4 {
		if slope, ok := fit(priceX, priceY); ok {
			moving.PriceReady = true
			moving.PricePerYear = slope
		}
	}

	// The two windows. "Twelve months back" and "twenty-four months back" are
	// calendar dates rather than counts of 365 days, so the comparison lines up
	// with the seasons on both sides of it - which matters for a number that
	// moves with the weather.
	today := types.Today(now)
	recentFrom := today.AddDate(-1, 0, 0)
	priorFrom := today.AddDate(-2, 0, 0)

	recent := window(fills, visits, recentFrom, today.AddDate(0, 0, 1))
	prior := window(fills, visits, priorFrom, recentFrom)

	moving.RecentFuelCents = recent.fuel
	moving.RecentServiceCents = recent.service
	moving.RecentTotalCents = recent.fuel + recent.service
	moving.RecentMiles = recent.miles
	moving.RecentMPG = recent.mpg()

	// How much of the last twelve months this car has actually been owned for.
	from := recentFrom
	if origin.After(from) {
		from = origin
	}

	moving.RecentDays = types.DaysBetween(from, today)

	// A month of history is the least that can be scaled to a month.
	if moving.RecentDays >= 30 && recent.fills > 0 {
		moving.RecentReady = true

		perDay := func(cents int64) float64 {
			return float64(cents) / float64(moving.RecentDays) * daysInMonth
		}

		moving.RecentFuelPerMonth = perDay(recent.fuel)
		moving.RecentServicePerMonth = perDay(recent.service)
		moving.RecentPerMonth = perDay(recent.fuel + recent.service)
	}

	moving.PriorTotalCents = prior.fuel + prior.service
	moving.PriorMiles = prior.miles
	moving.PriorMPG = prior.mpg()

	// A comparison needs both sides. A car bought fourteen months ago has a
	// prior window with two months in it, and holding a full year against two
	// months would report a collapse in spending that is really a shortage of
	// history.
	if prior.fills > 0 && !origin.After(priorFrom) {
		moving.HasPrior = true
		moving.SpendChange = change(float64(moving.RecentTotalCents), float64(moving.PriorTotalCents))
		moving.MilesChange = change(float64(moving.RecentMiles), float64(moving.PriorMiles))
		moving.MPGChange = change(moving.RecentMPG, moving.PriorMPG)
	}

	return moving
}

// slice is one window of the history, added up.
type slice struct {
	fills   int
	miles   int
	gallons float64
	fuel    int64
	service int64
}

// mpg is the window's measured economy, or zero when nothing in it could carry
// one.
func (s slice) mpg() float64 {
	if s.gallons <= 0 {
		return 0
	}

	return float64(s.miles) / s.gallons
}

// window adds up everything between two dates: from is included, to is not, so
// two windows laid end to end cannot both claim the same day.
func window(fills []Fill, visits []types.Service, from time.Time, to time.Time) slice {
	found := slice{}

	for _, fill := range fills {
		date := fill.Date()
		if date.IsZero() || date.Before(from) || !date.Before(to) {
			continue
		}

		found.fills++
		found.fuel += fill.CostCents

		if fill.Measured {
			found.miles += fill.Miles
			found.gallons += fill.Gallons
		}
	}

	for _, visit := range visits {
		date := visit.Date()
		if date.IsZero() || date.Before(from) || !date.Before(to) {
			continue
		}

		found.service += visit.CostCents
	}

	return found
}

// --------------------------------------------------------------- habits ----

// Habits is when you buy fuel, which is a fact about your week rather than
// about the car.
//
// It earns its place because the fuel log already knows it and nothing else
// does. The day of the week comes from the date every fill-up carries; the time
// of day comes from the clock field, which is optional, so the block that shows
// it says how many fills it could read.
type Habits struct {
	Ready bool
	Fills int

	Days []DayOfWeek
	// The days the most and the fewest fill-ups happen on.
	BusiestDay   string
	QuietestDay  string
	WeekendShare float64

	// Times is the four parts of the day, and Clocked is how many fill-ups
	// carried a time at all. Without that count the breakdown reads as if the
	// fills with no clock on them happened at midnight.
	HasClock bool
	Clocked  int
	Times    []TimeOfDay
}

// DayOfWeek is one day's worth of stopping for fuel.
type DayOfWeek struct {
	Day     time.Weekday
	Label   string
	Fills   int
	Share   float64
	Gallons float64
	// AvgPriceCents is what a gallon cost on this day of the week. It is not
	// advice - a pump does not know what day it is - but a run of Monday fills
	// at a highway station does show up here.
	AvgPriceCents float64
}

// FillsFloat is the count as a float, for a bar that needs a proportion.
func (d DayOfWeek) FillsFloat() float64 {
	return float64(d.Fills)
}

// TimeOfDay is one stretch of the clock.
type TimeOfDay struct {
	Label string
	From  int
	To    int
	Fills int
	Share float64
}

// FillsFloat is the count as a float, for the same reason DayOfWeek has one.
func (t TimeOfDay) FillsFloat() float64 {
	return float64(t.Fills)
}

// habits counts the fills by the day of the week and the part of the day.
func habits(fills []Fill) Habits {
	found := Habits{}

	counts := [7]int{}
	gallons := [7]float64{}
	spend := [7]int64{}

	// The four parts of the day, split where the reason for stopping changes:
	// before work, during it, on the way home, and late.
	parts := []TimeOfDay{
		{Label: "Overnight", From: 0, To: 5},
		{Label: "Morning", From: 5, To: 12},
		{Label: "Afternoon", From: 12, To: 17},
		{Label: "Evening", From: 17, To: 24},
	}

	var weekend int

	for _, fill := range fills {
		date := fill.Date()
		if date.IsZero() {
			continue
		}

		found.Fills++

		day := int(date.Weekday())
		counts[day]++
		gallons[day] += fill.Gallons
		spend[day] += fill.CostCents

		if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
			weekend++
		}

		if hour, ok := hourOf(fill.At); ok {
			found.Clocked++

			for at := range parts {
				if hour >= parts[at].From && hour < parts[at].To {
					parts[at].Fills++
					break
				}
			}
		}
	}

	if found.Fills == 0 {
		return found
	}

	found.Ready = true
	found.WeekendShare = percent(int64(weekend), int64(found.Fills))

	// Monday first. Go counts the week from Sunday, and a fuel log is read as a
	// working week, so the slice is built in the order it gets read in.
	order := []time.Weekday{
		time.Monday, time.Tuesday, time.Wednesday, time.Thursday,
		time.Friday, time.Saturday, time.Sunday,
	}

	busiest, quietest := 0, 0

	for _, day := range order {
		entry := DayOfWeek{
			Day:     day,
			Label:   day.String()[:3],
			Fills:   counts[int(day)],
			Gallons: gallons[int(day)],
			Share:   percent(int64(counts[int(day)]), int64(found.Fills)),
		}

		if gallons[int(day)] > 0 {
			entry.AvgPriceCents = float64(spend[int(day)]) / gallons[int(day)]
		}

		found.Days = append(found.Days, entry)

		if entry.Fills > found.Days[busiest].Fills {
			busiest = len(found.Days) - 1
		}

		if entry.Fills < found.Days[quietest].Fills {
			quietest = len(found.Days) - 1
		}
	}

	found.BusiestDay = found.Days[busiest].Day.String()
	found.QuietestDay = found.Days[quietest].Day.String()

	if found.Clocked > 0 {
		found.HasClock = true

		for at := range parts {
			parts[at].Share = percent(int64(parts[at].Fills), int64(found.Clocked))
		}

		found.Times = parts
	}

	return found
}

// hourOf reads the hour out of a stored "15:04", and reports false for a fill
// that never carried one.
func hourOf(stored string) (int, bool) {
	parsed, err := time.Parse("15:04", stored)
	if err != nil {
		return 0, false
	}

	return parsed.Hour(), true
}

// ----------------------------------------------------------- arithmetic ----

// Fences is the band a reading has to sit inside to be treated as one of the
// crowd, by Tukey's rule: the middle half of the column, opened out by one and
// a half times its own width at each end.
//
// It is here rather than in the chart package because it is a fact about the
// numbers rather than about the picture, and because a test on it belongs
// beside the other tests about what these numbers mean. What a caller does with
// a reading outside the band is the caller's business - the one thing it must
// not do is quietly change it.
//
// Four readings are the least this can say anything about; below that it
// reports false and every reading stands.
func Fences(values []float64) (low float64, high float64, ok bool) {
	if len(values) < 4 {
		return 0, 0, false
	}

	sorted := append([]float64{}, values...)
	sort.Float64s(sorted)

	first := quantile(sorted, 0.25)
	third := quantile(sorted, 0.75)

	span := third - first
	if span <= 0 {
		return 0, 0, false
	}

	return first - outlierFence*span, third + outlierFence*span, true
}

// quantile reads a percentile off a sorted slice, interpolating between the two
// readings it falls between. The slice must already be in order; sorting it in
// here would mean sorting the same column four times to describe it once.
func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}

	if len(sorted) == 1 {
		return sorted[0]
	}

	at := q * float64(len(sorted)-1)

	lower := int(math.Floor(at))
	upper := int(math.Ceil(at))

	if lower == upper {
		return sorted[lower]
	}

	return sorted[lower] + (sorted[upper]-sorted[lower])*(at-float64(lower))
}

// mean is the average of a slice, and zero for an empty one.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	var sum float64
	for _, value := range values {
		sum += value
	}

	return sum / float64(len(values))
}

// deviation is the sample standard deviation: roughly how far a reading sits
// from the average, in the units of the thing itself.
//
// It divides by one less than the count because these readings are a sample of
// how the car behaves rather than every tank it will ever burn, and the
// uncorrected figure is known to run low on a small sample.
func deviation(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}

	average := mean(values)

	var sum float64
	for _, value := range values {
		sum += (value - average) * (value - average)
	}

	return math.Sqrt(sum / float64(len(values)-1))
}

// fit is the slope of a least-squares straight line through the points, and
// false when there is no line to fit: fewer than two points, or every point at
// the same x.
func fit(xs []float64, ys []float64) (slope float64, ok bool) {
	if len(xs) != len(ys) || len(xs) < 2 {
		return 0, false
	}

	meanX, meanY := mean(xs), mean(ys)

	var top, bottom float64

	for at := range xs {
		gap := xs[at] - meanX

		top += gap * (ys[at] - meanY)
		bottom += gap * gap
	}

	if bottom == 0 {
		return 0, false
	}

	return top / bottom, true
}

// change is one figure against another as a percentage, and zero when there is
// nothing to compare against.
func change(now float64, before float64) float64 {
	if before == 0 {
		return 0
	}

	return (now - before) / before * 100
}
