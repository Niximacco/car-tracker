// Package stats is the block of formulas from the bottom of the sheet, moved
// into code.
//
// Nothing in here is stored. Every figure is worked out from the fill-ups and
// the shop visits each time a page is drawn, which is the whole reason for the
// move: a stored total is one forgotten fill-up away from being a lie, and a
// spreadsheet formula is one sorted column away from the same. Sixty rows is
// nothing to add up on every request.
//
// The rule the package is built around is that a fill-up is only worth a miles
// per gallon figure if the tank it measures started full and ended full. Every
// other number - gallons, money, what the discount saved - counts every fill,
// because you paid for all of them.
package stats

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Niximacco/car-tracker/internal/types"
)

// daysInMonth is what a per-day figure is multiplied by to read as a per-month
// one. It is 365/12 rather than 30, which is what the sheet used and is what
// makes these numbers match the ones that have been looked at for four years.
const daysInMonth = 365.0 / 12.0

// rollingWindow is how many measured fills the smoothed mpg line averages over.
// One tank in five is unrepresentative - a road trip, a month of short cold
// runs - and five is about where the line stops chasing them and still turns
// inside a season.
const rollingWindow = 5

// Fill is one fill-up with everything that can be worked out from it and the
// fill before it.
//
// The embedded Fillup is exactly what was typed in. Everything beside it is
// derived, and is derived here rather than stored precisely because it depends
// on a neighbour: adding a forgotten fill-up in the middle of the history
// changes the row after it, and a stored figure would not notice.
type Fill struct {
	types.Fillup

	// Miles is the distance covered on the tank this fill replaces, from the
	// previous fill's reading to this one's. The first fill has none.
	Miles int
	// Days is how long that tank lasted.
	Days int
	// MPG is the miles per gallon that tank returned, and Measured says whether
	// it means anything. A partial fill, a known gap, or the first row of the
	// history all give a fill that is real money and no mpg.
	MPG      float64
	Measured bool
	// Rolling is the average of the last few measured fills including this one,
	// which is the line worth looking at: one tank tells you about the weather,
	// five tell you about the car.
	Rolling float64

	// CostCents is what this tank cost, SavedCents is what the discount took
	// off it, and PumpPriceCents is what the sign said before it did.
	CostCents      int64
	SavedCents     int64
	PumpPriceCents int64

	// The running totals as at this fill, which is what the sheet's last four
	// columns were reaching for. DaysOwned counts from the day the car was
	// bought, so the early rows have a cost per day that is still settling
	// down - which is honest, and is why the column is worth having.
	RunningMiles     int
	RunningGallons   float64
	RunningFuelCents int64
	RunningCents     int64
	DaysOwned        int
	CostPerDayCents  float64
}

// Suspect reports whether this fill is worth a second look before it is
// believed: a reading that went backwards, a tank bigger than the car holds, or
// a miles per gallon that no petrol engine has ever returned.
//
// It is a flag on a row rather than a refusal at the form, because every one of
// these is occasionally true. An odometer does go backwards when a cluster is
// replaced, and a fuel figure does look absurd when the tank before it was a
// partial nobody ticked the box for. Refusing them would mean a history that
// cannot record what happened; flagging them means the one wrong row is the one
// with the mark against it.
func (f Fill) Suspect(tank float64) bool {
	if f.Miles < 0 {
		return true
	}

	if tank > 0 && f.Gallons > tank*1.15 {
		return true
	}

	return f.Measured && (f.MPG > 150 || f.MPG < 3)
}

// Report is everything a vehicle's pages can show. It is built in one pass over
// the history so that no two figures on a page can be worked out from different
// readings of it.
type Report struct {
	Vehicle types.Vehicle

	// Fills and Services are the history in the order it happened, oldest
	// first. The pages reverse them for display; the arithmetic needs them
	// forwards.
	Fills    []Fill
	Services []types.Service

	Summary  Summary
	Years    []Year
	Stations []Station
	Seasons  []Month
	Records  Records
	Outlook  Outlook
	Shop     ShopStats
}

// Any reports whether there is enough history here to be worth drawing a page
// about.
func (r Report) Any() bool {
	return len(r.Fills) > 0 || len(r.Services) > 0
}

// Summary is the block at the bottom of the sheet: what it has cost to own this
// car, three ways, over the whole time you have had it.
type Summary struct {
	// Whether there is anything to summarise, and whether the vehicle carries
	// the purchase date every per-day figure is divided by. A page reads these
	// rather than testing a total against zero, because nought miles and no
	// history are different things to say.
	HasFills    bool
	HasServices bool
	Owned       bool

	FirstOn string
	LastOn  string

	// TotalMiles is measured from the day it was bought, not from the first
	// fill-up that got logged. That is why it is larger than the distance
	// between the first and last rows, and why it is the honest answer to "how
	// far have I driven this".
	TotalMiles int
	// MeasuredMiles is the distance the fill-ups actually cover, which is what
	// the mpg is worked out over.
	MeasuredMiles int
	// LatestOdometer is the last reading from any entry, fill-up or shop visit,
	// and LatestOn is the day that reading was taken.
	LatestOdometer int
	LatestOn       string

	DaysOwned      int
	DaysAtLastFill int
	// DaysMeasured is the span the mileage actually covers: from the day it was
	// bought to the day of the reading TotalMiles ends at.
	//
	// Miles a day is divided by this rather than by DaysOwned, which is the one
	// place the sheet used a different denominator for the distance than for
	// the money - and was right to. The odometer is only known up to the last
	// entry, so dividing the distance by the days since then as well would
	// report a car that had been sitting still for a fortnight as one that
	// covers slightly less ground. The money genuinely was spent across the
	// whole time you have owned it, so the costs keep DaysOwned.
	DaysMeasured int
	MilesPerDay  float64

	Fills          int
	TotalGallons   float64
	AvgPriceCents  float64
	AvgPumpCents   float64
	SavedCents     int64
	SavedPercent   float64
	SavedPerGallon float64

	FuelCents    int64
	ServiceCents int64
	TotalCents   int64

	// The three costs, each read three ways. Per mile is what the car costs to
	// use, per day is what it costs to have, and per month is the one that
	// reads like a bill.
	FuelPerMile    float64
	ServicePerMile float64
	TotalPerMile   float64

	FuelPerDay    float64
	ServicePerDay float64
	TotalPerDay   float64

	FuelPerMonth    float64
	ServicePerMonth float64
	TotalPerMonth   float64

	// CumulativeMPG is the sheet's headline: every mile since the day you
	// bought it, over every gallon you have logged. It is a fraction low,
	// because the miles include the ones driven before the logging started and
	// the gallons do not - and it is kept exactly as the sheet had it, because
	// it is the number that has been watched for four years.
	CumulativeMPG float64
	// MeasuredMPG is the same question asked strictly: only tanks that started
	// full and ended full, over only the gallons that filled them. It is the
	// one to quote at somebody.
	MeasuredMPG float64
	// MeasuredFills is how many tanks that figure rests on.
	MeasuredFills int

	// The cost of the car itself, for a vehicle that records what it cost. It
	// is deliberately separate from every figure above: what it costs to run is
	// a different question from what it has cost to own, and mixing them makes
	// the first one useless.
	HasPurchase       bool
	PurchaseCents     int64
	SoldCents         int64
	OwnershipCents    int64
	OwnershipPerDay   float64
	OwnershipPerMile  float64
	OwnershipPerMonth float64
}

// Year is one calendar year of running the car. Miles and mpg count only the
// tanks that can carry them; money counts everything.
type Year struct {
	Year          int
	Fills         int
	Miles         int
	Gallons       float64
	MPG           float64
	AvgPriceCents float64
	FuelCents     int64
	ServiceCents  int64
	ServiceVisits int
	TotalCents    int64
	SavedCents    int64
}

// PumpCents is what the fuel would have cost with no discount on it, which is
// the only number that makes what the discount saved mean anything.
func (y Year) PumpCents() int64 {
	return y.FuelCents + y.SavedCents
}

// TotalCentsFloat is the year's spending as a float, purely so a template can
// draw a bar in proportion to it. Every actual sum stays in whole cents.
func (y Year) TotalCentsFloat() float64 {
	return float64(y.TotalCents)
}

// Station is one place you buy fuel, and how it has treated you. The mpg column
// is not a claim about the fuel - it is mostly a claim about the roads you are
// on when you stop there - but the price column is exactly what it looks like.
type Station struct {
	Name          string
	Fills         int
	Share         float64
	Gallons       float64
	FuelCents     int64
	SavedCents    int64
	AvgPriceCents float64
	AvgPumpCents  float64
	MPG           float64
	FirstOn       string
	LastOn        string
}

// FillsFloat is the visit count as a float, for the same reason Year has one:
// a bar needs a proportion and a template cannot make one.
func (s Station) FillsFloat() float64 {
	return float64(s.Fills)
}

// Month is one month of the calendar across every year of the history, which is
// where the weather shows up. A car returns noticeably less in February than in
// July and the difference is worth seeing rather than wondering about.
type Month struct {
	Month         time.Month
	Label         string
	Fills         int
	Miles         int
	Gallons       float64
	MPG           float64
	AvgPriceCents float64
}

// Records is the extremes, and the averages the extremes are extreme against.
type Records struct {
	Best  Fill
	Worst Fill
	// Cheapest and Dearest are by what was actually paid a gallon, which is the
	// number that comes out of your account rather than the one on the sign.
	Cheapest Fill
	Dearest  Fill
	// Biggest is the most that has ever gone in at once, which is the closest
	// thing to a measurement of the tank.
	Biggest Fill
	// Longest is the most miles ever covered on one tank.
	Longest Fill
	// Slowest is the longest a single tank has ever taken to use up.
	Slowest Fill
	// Dearest single visit to a shop.
	BiggestService types.Service

	AvgDaysBetween  float64
	AvgMilesBetween float64
	AvgGallons      float64
	AvgFillCents    float64

	// Summer is April to September, winter is October to March. Six months
	// each, split where the heating goes on rather than at the solstice.
	SummerMPG   float64
	WinterMPG   float64
	SummerFills int
	WinterFills int
	// SeasonalGap is how much better summer is, as a percentage of winter.
	SeasonalGap float64
}

// Outlook is the arithmetic pointed forwards. Everything in it is the recent
// past extended in a straight line, which is worth saying on the page: it
// answers "if nothing changes", and something always changes.
type Outlook struct {
	// Whether there is enough history to extend at all.
	Ready bool

	MilesPerDay float64
	// EstimatedOdometer is where the clock probably is today, given the last
	// reading and how fast it usually moves. It is what makes "due in 400
	// miles" mean something between fill-ups.
	EstimatedOdometer int
	// OdometerInAYear is the same line drawn twelve months out.
	OdometerInAYear int

	// NextFillOn is when the tank probably runs out, from the last fill and how
	// long a tank usually lasts.
	NextFillOn   string
	NextFillDays int
	// Overdue is set when that day has already been and gone, which usually
	// means a fill-up has not been logged rather than that the car has not
	// moved.
	Overdue bool

	// The next twelve months at the rate of the last twelve.
	YearFuelCents    int64
	YearServiceCents int64
	YearTotalCents   int64
	YearMiles        int
	// BasedOnDays is how much history those three rest on, so a page can say
	// "at the rate of the last 11 months" rather than implying a full year.
	BasedOnDays int
}

// ShopStats is the maintenance history read as a maintenance history rather
// than as a column of money.
type ShopStats struct {
	Visits     int
	TotalCents int64
	AvgCents   int64
	Shops      []Shop

	// The intervals, which are what a service history is actually for.
	AvgMilesBetween float64
	AvgDaysBetween  float64

	// Oil is tracked on its own because it is the one job on a fixed interval
	// that you are expected to keep track of yourself. It is found by reading
	// the work text rather than by a checkbox, because the invoices already say
	// it and nobody is going to go back and tick sixty boxes.
	LastOil          types.Service
	HasOil           bool
	OilIntervalMiles int
	MilesSinceOil    int
	DaysSinceOil     int
	// OilDueIn is miles remaining against the usual interval, negative when it
	// is overdue. It rests on the estimated odometer, so it moves between
	// fill-ups.
	OilDueIn   int
	OilOverdue bool
}

// Shop is one place that has worked on the car.
type Shop struct {
	Name       string
	Visits     int
	TotalCents int64
	LastOn     string
}

// Analyze reads a vehicle's whole history and works out everything the pages
// can show.
//
// It sorts its own copies rather than trusting what it was handed. Every figure
// in here is read from a fill against its neighbour, so an unsorted slice would
// not produce a wrong-looking page - it would produce a page of plausible
// numbers that are all quietly wrong, which is worse.
func Analyze(vehicle types.Vehicle, fills []types.Fillup, services []types.Service, now time.Time) Report {
	ordered := append([]types.Fillup{}, fills...)
	types.ByDate(ordered)

	visits := append([]types.Service{}, services...)
	types.ServicesByDate(visits)

	report := Report{
		Vehicle:  vehicle,
		Fills:    derive(vehicle, ordered, visits, now),
		Services: visits,
	}

	report.Summary = summarize(vehicle, report.Fills, visits, now)
	report.Years = years(report.Fills, visits)
	report.Stations = stations(report.Fills)
	report.Seasons = months(report.Fills)
	report.Records = records(report.Fills, visits)
	report.Shop = shopStats(visits, report.Summary)
	report.Outlook = outlook(report.Fills, visits, report.Summary, report.Shop, now)

	// The oil figures need the estimated odometer, which needs the outlook,
	// which needs the shop stats. Rather than untangling that into two passes
	// over the same data, the one field that closes the loop is filled in here.
	report.Shop = oilOutlook(report.Shop, report.Outlook)

	return report
}

// derive walks the fill-ups in order, working each one out against the one
// before it and carrying the running totals along.
func derive(vehicle types.Vehicle, fills []types.Fillup, visits []types.Service, now time.Time) []Fill {
	derived := make([]Fill, 0, len(fills))

	purchased := vehicle.PurchaseDate()
	origin := vehicle.PurchaseOdometer

	// Shop visits contribute to the running total, so they have to be spent
	// into it in date order alongside the fills. This is the cursor into them.
	visit := 0

	var runningGallons float64
	var runningFuel, runningService int64
	var measured []float64

	for at, fillup := range fills {
		fill := Fill{
			Fillup:         fillup,
			CostCents:      fillup.Cost(),
			SavedCents:     fillup.Saved(),
			PumpPriceCents: fillup.PumpPriceCents(),
		}

		if at > 0 {
			previous := fills[at-1]

			fill.Miles = fillup.Odometer - previous.Odometer
			fill.Days = types.DaysBetween(previous.Date(), fillup.Date())

			// A tank is only worth an mpg if it started full and ended full.
			// Both ends have to be clean: this fill running to full is what
			// makes the gallons the tank's, and the one before it running to
			// full is what makes the distance the tank's.
			if fillup.FullTank() && !previous.Partial && fill.Miles > 0 && fillup.Gallons > 0 {
				fill.MPG = float64(fill.Miles) / fillup.Gallons
				fill.Measured = true

				measured = append(measured, fill.MPG)
				fill.Rolling = tail(measured, rollingWindow)
			}
		}

		runningGallons += fillup.Gallons
		runningFuel += fill.CostCents

		// Everything the shop did on or before this day is spent by now.
		for visit < len(visits) && visits[visit].On <= fillup.On {
			runningService += visits[visit].CostCents
			visit++
		}

		fill.RunningGallons = runningGallons
		fill.RunningFuelCents = runningFuel
		fill.RunningCents = runningFuel + runningService

		if origin > 0 || !purchased.IsZero() {
			fill.RunningMiles = fillup.Odometer - origin
		}

		if !purchased.IsZero() {
			fill.DaysOwned = types.DaysBetween(purchased, fillup.Date())
			if fill.DaysOwned > 0 {
				fill.CostPerDayCents = float64(fill.RunningCents) / float64(fill.DaysOwned)
			}
		}

		derived = append(derived, fill)
	}

	return derived
}

// summarize is the block at the bottom of the sheet.
func summarize(vehicle types.Vehicle, fills []Fill, visits []types.Service, now time.Time) Summary {
	summary := Summary{
		HasFills:    len(fills) > 0,
		HasServices: len(visits) > 0,
		Owned:       vehicle.Owned(),
		Fills:       len(fills),
	}

	var measuredMiles int
	var measuredGallons float64
	var savedGallons float64

	for _, fill := range fills {
		summary.TotalGallons += fill.Gallons
		summary.FuelCents += fill.CostCents
		summary.SavedCents += fill.SavedCents

		if fill.Discounted() {
			savedGallons += fill.Gallons
		}

		if fill.Measured {
			measuredMiles += fill.Miles
			measuredGallons += fill.Gallons
			summary.MeasuredFills++
		}

		if fill.Odometer > summary.LatestOdometer {
			summary.LatestOdometer, summary.LatestOn = fill.Odometer, fill.On
		}
	}

	for _, visit := range visits {
		summary.ServiceCents += visit.CostCents

		if visit.Odometer > summary.LatestOdometer {
			summary.LatestOdometer, summary.LatestOn = visit.Odometer, visit.On
		}
	}

	summary.MeasuredMiles = measuredMiles
	summary.TotalCents = summary.FuelCents + summary.ServiceCents

	if len(fills) > 0 {
		summary.FirstOn = fills[0].On
		summary.LastOn = fills[len(fills)-1].On
	}

	// How far it has gone. Measured from the reading the day it was bought
	// where that is known, which is what makes it larger than the distance the
	// fill-ups cover, and from the first logged reading where it is not.
	// measuredTo is the day TotalMiles is measured up to, which is what miles a
	// day is divided by the span to.
	measuredTo := summary.LatestOn

	switch {
	case vehicle.Retired && vehicle.SoldOdometer > vehicle.PurchaseOdometer:
		summary.TotalMiles = vehicle.SoldOdometer - vehicle.PurchaseOdometer

		if vehicle.SoldOn != "" {
			measuredTo = vehicle.SoldOn
		}

	case vehicle.PurchaseOdometer > 0 && summary.LatestOdometer > vehicle.PurchaseOdometer:
		summary.TotalMiles = summary.LatestOdometer - vehicle.PurchaseOdometer

	default:
		summary.TotalMiles = measuredMiles
	}

	// How long it has been yours. A retired car stops counting on the day it
	// went, which is the one thing that keeps every per-day figure on its page
	// from drifting down forever.
	lastEntry := lastEntryDate(fills, visits)
	if summary.Owned {
		summary.DaysOwned = types.DaysBetween(vehicle.PurchaseDate(), vehicle.EndOfOwnership(now, lastEntry))

		if summary.HasFills {
			summary.DaysAtLastFill = fills[len(fills)-1].DaysOwned
		}

		summary.DaysMeasured = types.DaysBetween(vehicle.PurchaseDate(), types.ParseDate(measuredTo))
	}

	if summary.DaysMeasured > 0 {
		summary.MilesPerDay = float64(summary.TotalMiles) / float64(summary.DaysMeasured)
	}

	if summary.DaysOwned > 0 {
		summary.FuelPerDay = float64(summary.FuelCents) / float64(summary.DaysOwned)
		summary.ServicePerDay = float64(summary.ServiceCents) / float64(summary.DaysOwned)
		summary.TotalPerDay = float64(summary.TotalCents) / float64(summary.DaysOwned)

		summary.FuelPerMonth = summary.FuelPerDay * daysInMonth
		summary.ServicePerMonth = summary.ServicePerDay * daysInMonth
		summary.TotalPerMonth = summary.TotalPerDay * daysInMonth
	}

	if summary.TotalMiles > 0 {
		summary.FuelPerMile = float64(summary.FuelCents) / float64(summary.TotalMiles)
		summary.ServicePerMile = float64(summary.ServiceCents) / float64(summary.TotalMiles)
		summary.TotalPerMile = float64(summary.TotalCents) / float64(summary.TotalMiles)
	}

	if summary.TotalGallons > 0 {
		summary.AvgPriceCents = float64(summary.FuelCents) / summary.TotalGallons
		summary.AvgPumpCents = float64(summary.FuelCents+summary.SavedCents) / summary.TotalGallons
		summary.CumulativeMPG = float64(summary.TotalMiles) / summary.TotalGallons
	}

	if measuredGallons > 0 {
		summary.MeasuredMPG = float64(measuredMiles) / measuredGallons
	}

	if summary.SavedCents > 0 {
		summary.SavedPercent = percent(summary.SavedCents, summary.FuelCents+summary.SavedCents)

		if savedGallons > 0 {
			summary.SavedPerGallon = float64(summary.SavedCents) / savedGallons
		}
	}

	// What the car has cost to own, as opposed to what it costs to run. It is
	// only worked out when the purchase price is on file, because a zero here
	// would read as a car that was free rather than as a price nobody entered.
	if vehicle.PurchasePriceCents > 0 {
		summary.HasPurchase = true
		summary.PurchaseCents = vehicle.PurchasePriceCents
		summary.SoldCents = vehicle.SoldPriceCents
		summary.OwnershipCents = vehicle.PurchasePriceCents - vehicle.SoldPriceCents + summary.TotalCents

		if summary.DaysOwned > 0 {
			summary.OwnershipPerDay = float64(summary.OwnershipCents) / float64(summary.DaysOwned)
			summary.OwnershipPerMonth = summary.OwnershipPerDay * daysInMonth
		}

		if summary.TotalMiles > 0 {
			summary.OwnershipPerMile = float64(summary.OwnershipCents) / float64(summary.TotalMiles)
		}
	}

	return summary
}

// years groups the history by calendar year.
func years(fills []Fill, visits []types.Service) []Year {
	index := map[int]*Year{}

	get := func(date time.Time) *Year {
		if date.IsZero() {
			return nil
		}

		year, seen := index[date.Year()]
		if !seen {
			year = &Year{Year: date.Year()}
			index[date.Year()] = year
		}

		return year
	}

	for _, fill := range fills {
		year := get(fill.Date())
		if year == nil {
			continue
		}

		year.Fills++
		year.Gallons += fill.Gallons
		year.FuelCents += fill.CostCents
		year.SavedCents += fill.SavedCents

		if fill.Measured {
			year.Miles += fill.Miles
		}
	}

	for _, visit := range visits {
		year := get(visit.Date())
		if year == nil {
			continue
		}

		year.ServiceVisits++
		year.ServiceCents += visit.CostCents
	}

	out := make([]Year, 0, len(index))
	for _, year := range index {
		// The mpg for a year is worked out from the tanks that can carry one,
		// so it is miles over the gallons of those tanks rather than over every
		// gallon bought that year. Dividing by all of them would drag every
		// year with a partial fill in it downwards.
		year.MPG = yearMPG(fills, year.Year)

		if year.Gallons > 0 {
			year.AvgPriceCents = float64(year.FuelCents) / year.Gallons
		}

		year.TotalCents = year.FuelCents + year.ServiceCents
		out = append(out, *year)
	}

	sort.Slice(out, func(a int, b int) bool { return out[a].Year > out[b].Year })

	return out
}

// yearMPG is the measured miles and measured gallons of one calendar year.
func yearMPG(fills []Fill, year int) float64 {
	var miles int
	var gallons float64

	for _, fill := range fills {
		date := fill.Date()
		if date.IsZero() || date.Year() != year || !fill.Measured {
			continue
		}

		miles += fill.Miles
		gallons += fill.Gallons
	}

	if gallons <= 0 {
		return 0
	}

	return float64(miles) / gallons
}

// stations groups the fills by where they happened.
func stations(fills []Fill) []Station {
	type bucket struct {
		Station
		measuredMiles   int
		measuredGallons float64
	}

	index := map[string]*bucket{}
	order := []string{}

	for _, fill := range fills {
		name := StationName(fill.Station)
		// Grouped on a folded key rather than on what was typed, so that
		// "Hy-Vee new hope" and "Hy-Vee New Hope" are one station rather than
		// two rows that each tell you half the story. The first spelling seen
		// is the one the page shows, because picking a winner between two
		// spellings of the same place is not worth code.
		key := StationKey(name)

		found, seen := index[key]
		if !seen {
			found = &bucket{Station: Station{Name: name, FirstOn: fill.On}}
			index[key] = found
			order = append(order, key)
		}

		found.Fills++
		found.Gallons += fill.Gallons
		found.FuelCents += fill.CostCents
		found.SavedCents += fill.SavedCents
		found.LastOn = fill.On

		if fill.Measured {
			found.measuredMiles += fill.Miles
			found.measuredGallons += fill.Gallons
		}
	}

	out := make([]Station, 0, len(order))
	for _, key := range order {
		found := index[key]

		if found.Gallons > 0 {
			found.AvgPriceCents = float64(found.FuelCents) / found.Gallons
			found.AvgPumpCents = float64(found.FuelCents+found.SavedCents) / found.Gallons
		}

		if found.measuredGallons > 0 {
			found.MPG = float64(found.measuredMiles) / found.measuredGallons
		}

		found.Share = percent(int64(found.Fills), int64(len(fills)))
		out = append(out, found.Station)
	}

	// Most visited first, and alphabetically inside a tie so the order does not
	// wander between requests.
	sort.SliceStable(out, func(a int, b int) bool {
		if out[a].Fills != out[b].Fills {
			return out[a].Fills > out[b].Fills
		}

		return out[a].Name < out[b].Name
	})

	return out
}

// StationName folds what somebody typed into the name it is grouped under.
// "Hy-Vee new hope", "Hy-Vee New Hope " and "hyvee new hope" are one station,
// and a breakdown that lists them as three is a breakdown of nothing.
//
// It is deliberately only case, spacing and punctuation. Anything cleverer -
// deciding that "Speedway Crystal" and "Crystal speedway" are the same place -
// would be guessing, and a station list you cannot trust is worse than one with
// two rows you have to read together.
func StationName(typed string) string {
	trimmed := strings.Join(strings.Fields(typed), " ")
	if trimmed == "" {
		return "Not recorded"
	}

	return trimmed
}

// StationKey is what two station names are compared on: lower case, with the
// hyphens and full stops taken out, so "Hy-Vee" and "hyvee" land together.
func StationKey(name string) string {
	folded := strings.ToLower(strings.Join(strings.Fields(name), " "))

	return strings.NewReplacer("-", "", ".", "", "'", "").Replace(folded)
}

// months averages every fill by the month of the year it happened in, which is
// where the weather is.
func months(fills []Fill) []Month {
	miles := [13]int{}
	gallons := [13]float64{}
	spend := [13]int64{}
	bought := [13]float64{}
	count := [13]int{}

	for _, fill := range fills {
		date := fill.Date()
		if date.IsZero() {
			continue
		}

		month := int(date.Month())

		count[month]++
		spend[month] += fill.CostCents
		bought[month] += fill.Gallons

		if fill.Measured {
			miles[month] += fill.Miles
			gallons[month] += fill.Gallons
		}
	}

	out := make([]Month, 0, 12)
	for month := 1; month <= 12; month++ {
		entry := Month{
			Month:   time.Month(month),
			Label:   time.Month(month).String()[:3],
			Fills:   count[month],
			Miles:   miles[month],
			Gallons: gallons[month],
		}

		if gallons[month] > 0 {
			entry.MPG = float64(miles[month]) / gallons[month]
		}

		if bought[month] > 0 {
			entry.AvgPriceCents = float64(spend[month]) / bought[month]
		}

		out = append(out, entry)
	}

	return out
}

// records finds the extremes and the averages they stand out from.
func records(fills []Fill, visits []types.Service) Records {
	found := Records{}

	var gaps, distances int
	var spans int
	var gallons float64
	var spend int64

	var summerMiles, winterMiles int
	var summerGallons, winterGallons float64

	for _, fill := range fills {
		gallons += fill.Gallons
		spend += fill.CostCents

		if fill.Days > 0 {
			gaps += fill.Days
			spans++
		}

		if fill.Miles > 0 {
			distances += fill.Miles

			if fill.Miles > found.Longest.Miles {
				found.Longest = fill
			}
		}

		if fill.Days > found.Slowest.Days {
			found.Slowest = fill
		}

		if fill.Gallons > found.Biggest.Gallons {
			found.Biggest = fill
		}

		if fill.PricePerGallonCents > 0 {
			if found.Cheapest.PricePerGallonCents == 0 || fill.PricePerGallonCents < found.Cheapest.PricePerGallonCents {
				found.Cheapest = fill
			}

			if fill.PricePerGallonCents > found.Dearest.PricePerGallonCents {
				found.Dearest = fill
			}
		}

		if fill.Measured {
			if fill.MPG > found.Best.MPG {
				found.Best = fill
			}

			if found.Worst.MPG == 0 || fill.MPG < found.Worst.MPG {
				found.Worst = fill
			}

			if Summer(fill.Date()) {
				summerMiles += fill.Miles
				summerGallons += fill.Gallons
				found.SummerFills++
			} else {
				winterMiles += fill.Miles
				winterGallons += fill.Gallons
				found.WinterFills++
			}
		}
	}

	if spans > 0 {
		found.AvgDaysBetween = float64(gaps) / float64(spans)
	}

	if count := len(fills) - 1; count > 0 {
		found.AvgMilesBetween = float64(distances) / float64(count)
	}

	if len(fills) > 0 {
		found.AvgGallons = gallons / float64(len(fills))
		found.AvgFillCents = float64(spend) / float64(len(fills))
	}

	if summerGallons > 0 {
		found.SummerMPG = float64(summerMiles) / summerGallons
	}

	if winterGallons > 0 {
		found.WinterMPG = float64(winterMiles) / winterGallons
	}

	if found.SummerMPG > 0 && found.WinterMPG > 0 {
		found.SeasonalGap = (found.SummerMPG - found.WinterMPG) / found.WinterMPG * 100
	}

	for _, visit := range visits {
		if visit.CostCents > found.BiggestService.CostCents {
			found.BiggestService = visit
		}
	}

	return found
}

// Summer is April to September. The split is where the heating goes on rather
// than at the solstice, because that is where the fuel figure actually moves.
func Summer(date time.Time) bool {
	if date.IsZero() {
		return false
	}

	month := date.Month()

	return month >= time.April && month <= time.September
}

// shopStats reads the maintenance history as a history rather than a column of
// money: how often, how far apart, and when the oil was last done.
func shopStats(visits []types.Service, summary Summary) ShopStats {
	stats := ShopStats{Visits: len(visits)}
	if len(visits) == 0 {
		return stats
	}

	index := map[string]*Shop{}
	order := []string{}

	for _, visit := range visits {
		stats.TotalCents += visit.CostCents

		name := StationName(visit.Shop)
		key := StationKey(name)

		shop, seen := index[key]
		if !seen {
			shop = &Shop{Name: name}
			index[key] = shop
			order = append(order, key)
		}

		shop.Visits++
		shop.TotalCents += visit.CostCents
		shop.LastOn = visit.On

		if visit.Mentions("oil") {
			stats.LastOil = visit
			stats.HasOil = true
		}
	}

	stats.AvgCents = stats.TotalCents / int64(len(visits))

	for _, key := range order {
		stats.Shops = append(stats.Shops, *index[key])
	}

	sort.SliceStable(stats.Shops, func(a int, b int) bool {
		if stats.Shops[a].Visits != stats.Shops[b].Visits {
			return stats.Shops[a].Visits > stats.Shops[b].Visits
		}

		return stats.Shops[a].Name < stats.Shops[b].Name
	})

	// How far apart the visits are, across the whole history rather than
	// between consecutive pairs, so one visit two days after another to fix
	// what the first one broke does not halve the average.
	first, last := visits[0], visits[len(visits)-1]
	if spans := len(visits) - 1; spans > 0 {
		if miles := last.Odometer - first.Odometer; miles > 0 {
			stats.AvgMilesBetween = float64(miles) / float64(spans)
		}

		if days := types.DaysBetween(first.Date(), last.Date()); days > 0 {
			stats.AvgDaysBetween = float64(days) / float64(spans)
		}
	}

	stats.OilIntervalMiles = oilInterval(visits)

	if stats.HasOil {
		if summary.LatestOdometer > stats.LastOil.Odometer {
			stats.MilesSinceOil = summary.LatestOdometer - stats.LastOil.Odometer
		}

		stats.DaysSinceOil = types.DaysBetween(stats.LastOil.Date(), types.Today(time.Now()))
	}

	return stats
}

// oilInterval is the usual distance between oil changes, read from the visits
// that mention one.
//
// It comes from the history rather than from a manufacturer's figure because
// the history is what actually happened, and because a car that gets it done
// every eight thousand miles is not helped by being told it is due at five.
// Two changes are enough to have an interval; one is not.
func oilInterval(visits []types.Service) int {
	readings := []int{}

	for _, visit := range visits {
		if visit.Mentions("oil") && visit.Odometer > 0 {
			readings = append(readings, visit.Odometer)
		}
	}

	if len(readings) < 2 {
		return 0
	}

	sort.Ints(readings)

	gaps := []int{}
	for at := 1; at < len(readings); at++ {
		if gap := readings[at] - readings[at-1]; gap > 0 {
			gaps = append(gaps, gap)
		}
	}

	if len(gaps) == 0 {
		return 0
	}

	// The median rather than the mean. One oil change done early because the
	// car was in for something else should not pull the expected interval down
	// for good.
	sort.Ints(gaps)

	return gaps[len(gaps)/2]
}

// oilOutlook fills in the two oil figures that need to know where the odometer
// probably is right now rather than where it was at the last entry.
func oilOutlook(stats ShopStats, ahead Outlook) ShopStats {
	if !stats.HasOil || stats.OilIntervalMiles <= 0 {
		return stats
	}

	odometer := ahead.EstimatedOdometer
	if odometer <= stats.LastOil.Odometer {
		return stats
	}

	stats.MilesSinceOil = odometer - stats.LastOil.Odometer
	stats.OilDueIn = stats.OilIntervalMiles - stats.MilesSinceOil
	stats.OilOverdue = stats.OilDueIn < 0

	return stats
}

// outlook extends the recent past in a straight line. Everything it produces is
// labelled on the page as what it is - an assumption that nothing changes - and
// none of it is stored anywhere.
func outlook(fills []Fill, visits []types.Service, summary Summary, shop ShopStats, now time.Time) Outlook {
	ahead := Outlook{
		MilesPerDay:       summary.MilesPerDay,
		EstimatedOdometer: summary.LatestOdometer,
	}

	if len(fills) == 0 || summary.LatestOdometer == 0 {
		return ahead
	}

	ahead.Ready = true

	last := fills[len(fills)-1]
	lastDate := last.Date()
	if latest := latestEntry(fills, visits); !latest.IsZero() {
		lastDate = latest
	}

	// Where the clock probably is now: the last reading plus however long ago
	// that was, at the usual rate.
	if ahead.MilesPerDay > 0 && !lastDate.IsZero() {
		if since := types.DaysBetween(lastDate, types.Today(now)); since > 0 {
			ahead.EstimatedOdometer = summary.LatestOdometer + int(math.Round(ahead.MilesPerDay*float64(since)))
		}

		ahead.OdometerInAYear = ahead.EstimatedOdometer + int(math.Round(ahead.MilesPerDay*365))
	}

	// When the tank probably runs out.
	if gap := averageGap(fills); gap > 0 && !last.Date().IsZero() {
		due := last.Date().AddDate(0, 0, int(math.Round(gap)))

		ahead.NextFillOn = due.Format(types.DateLayout)
		ahead.NextFillDays = types.DaysBetween(types.Today(now), due)
		ahead.Overdue = ahead.NextFillDays < 0
	}

	// The next twelve months at the rate of the last twelve. A history shorter
	// than a year is scaled up from what there is, and the page says how much
	// that was rather than letting three months look like a year.
	window := types.Today(now).AddDate(-1, 0, 0)

	var fuel, service int64
	var miles int
	earliest := types.Today(now)

	for _, fill := range fills {
		date := fill.Date()
		if date.IsZero() || date.Before(window) {
			continue
		}

		fuel += fill.CostCents

		if fill.Measured {
			miles += fill.Miles
		}

		if date.Before(earliest) {
			earliest = date
		}
	}

	for _, visit := range visits {
		date := visit.Date()
		if date.IsZero() || date.Before(window) {
			continue
		}

		service += visit.CostCents

		if date.Before(earliest) {
			earliest = date
		}
	}

	ahead.BasedOnDays = types.DaysBetween(earliest, types.Today(now))

	if ahead.BasedOnDays > 0 {
		scale := 365.0 / float64(ahead.BasedOnDays)

		ahead.YearFuelCents = types.Round(float64(fuel) * scale)
		ahead.YearServiceCents = types.Round(float64(service) * scale)
		ahead.YearTotalCents = ahead.YearFuelCents + ahead.YearServiceCents
		ahead.YearMiles = int(math.Round(float64(miles) * scale))
	}

	return ahead
}

// averageGap is how many days a tank usually lasts, over the last few fills
// rather than over the whole history. How much you drive changes; how much you
// drove in 2022 is not what predicts next Tuesday.
func averageGap(fills []Fill) float64 {
	const window = 6

	var days, spans int

	for at := len(fills) - 1; at >= 1 && spans < window; at-- {
		if fills[at].Days > 0 {
			days += fills[at].Days
			spans++
		}
	}

	if spans == 0 {
		return 0
	}

	return float64(days) / float64(spans)
}

// latestEntry is the most recent date anything happened, fill-up or shop visit.
func latestEntry(fills []Fill, visits []types.Service) time.Time {
	latest := time.Time{}

	if len(fills) > 0 {
		latest = fills[len(fills)-1].Date()
	}

	if len(visits) > 0 {
		if date := visits[len(visits)-1].Date(); date.After(latest) {
			latest = date
		}
	}

	return latest
}

// lastEntryDate is latestEntry over the stored shapes, for the summary's use
// before the derived fills exist.
func lastEntryDate(fills []Fill, visits []types.Service) time.Time {
	return latestEntry(fills, visits)
}

// tail averages the last few values of a slice, or all of them when there are
// fewer than that.
func tail(values []float64, window int) float64 {
	if len(values) == 0 {
		return 0
	}

	from := len(values) - window
	if from < 0 {
		from = 0
	}

	var sum float64
	for _, value := range values[from:] {
		sum += value
	}

	return sum / float64(len(values)-from)
}

// percent is one number as a percentage of another, and nought rather than a
// panic when the other is nought.
func percent(part int64, whole int64) float64 {
	if whole == 0 {
		return 0
	}

	return float64(part) / float64(whole) * 100
}
