package stats

import (
	"testing"
	"time"

	"github.com/Niximacco/car-tracker/internal/types"
)

var now = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

func wagon() types.Vehicle {
	return types.Vehicle{
		Slug:             "the-wagon",
		Name:             "The wagon",
		PurchasedOn:      "2024-01-01",
		PurchaseOdometer: 10000,
		TankGallons:      13.5,
	}
}

func fill(on string, odometer int, gallons float64, price int64) types.Fillup {
	return types.Fillup{
		ID: types.FillupID(on, odometer), Vehicle: "the-wagon",
		On: on, Odometer: odometer, Gallons: gallons, PricePerGallonCents: price,
	}
}

// The rule the whole package is built on: a tank is only worth a miles per
// gallon figure if it started full and ended full.
//
// Everything else - gallons, money, what the discount saved - counts every
// fill, because you paid for all of them. These four cases are the four ways a
// tank fails that rule, and getting any of them wrong produces a page of
// plausible numbers that are quietly wrong, which is the worst kind.
func TestOnlyAFullTankCarriesAnMPG(t *testing.T) {
	fills := []types.Fillup{
		fill("2024-01-05", 10300, 10, 300),
		fill("2024-01-19", 10700, 12, 300),
		fill("2024-02-02", 11000, 6, 300),
		fill("2024-02-16", 11400, 12, 300),
		fill("2024-03-01", 11800, 12, 300),
		fill("2024-03-15", 12200, 12, 300),
	}

	// The third is a splash and go, and the fifth had one nobody wrote down.
	fills[2].Partial = true
	fills[4].Missed = true

	report := Analyze(wagon(), fills, nil, now)

	want := []bool{
		false, // the first has nothing behind it to measure against
		true,
		false, // a partial tank: its gallons are not a tankful
		false, // the tank behind this one did not end full
		false, // a known gap: the miles are real, the gallons are not all of them
		true,
	}

	for at, expected := range want {
		if report.Fills[at].Measured != expected {
			t.Errorf("fill %d (%s) measured = %v, want %v",
				at, report.Fills[at].On, report.Fills[at].Measured, expected)
		}
	}

	// Every gallon and every dollar still counts.
	if got := report.Summary.TotalGallons; got != 64 {
		t.Errorf("total gallons = %v, want 64 - a partial fill is still fuel you bought", got)
	}

	if got := report.Summary.FuelCents; got != 19200 {
		t.Errorf("total fuel = %d cents, want 19200", got)
	}

	// The measured mpg rests only on the two tanks that can carry one:
	// 400 miles on 12 gallons, twice.
	if got := report.Summary.MeasuredMPG; got < 33.32 || got > 33.34 {
		t.Errorf("measured mpg = %v, want 400/12", got)
	}

	if report.Summary.MeasuredFills != 2 {
		t.Errorf("%d fills are measured, want 2", report.Summary.MeasuredFills)
	}
}

// Total miles is measured from the day the car was bought, not from the first
// fill-up that got logged. That difference is the reason the sheet's lifetime
// mileage is bigger than the distance between its first and last rows.
func TestTotalMilesCountsFromThePurchaseReading(t *testing.T) {
	report := Analyze(wagon(), []types.Fillup{
		fill("2024-01-05", 10300, 10, 300),
		fill("2024-01-19", 10700, 12, 300),
	}, nil, now)

	if got := report.Summary.TotalMiles; got != 700 {
		t.Errorf("total miles = %d, want 700 - measured from the purchase reading", got)
	}

	if got := report.Summary.MeasuredMiles; got != 400 {
		t.Errorf("measured miles = %d, want 400 - what the fill-ups actually cover", got)
	}
}

// A car with no purchase date can still say what the fuel cost. What it cannot
// say is what the car costs, and it has to leave those blank rather than
// dividing by nothing.
func TestAVehicleWithNoPurchaseDateLeavesThePerDayFiguresAlone(t *testing.T) {
	vehicle := types.Vehicle{Slug: "the-wagon"}

	report := Analyze(vehicle, []types.Fillup{
		fill("2024-01-05", 10300, 10, 300),
		fill("2024-01-19", 10700, 12, 300),
	}, nil, now)

	if report.Summary.Owned {
		t.Error("a vehicle with no purchase date reports itself as owned")
	}

	if report.Summary.DaysOwned != 0 || report.Summary.TotalPerDay != 0 {
		t.Errorf("days owned = %d and cost a day = %v, want both zero",
			report.Summary.DaysOwned, report.Summary.TotalPerDay)
	}

	// Falling back to the distance the fill-ups cover is the honest answer when
	// there is no purchase reading to measure from.
	if got := report.Summary.TotalMiles; got != 400 {
		t.Errorf("total miles = %d, want 400", got)
	}

	if got := report.Summary.FuelCents; got != 6600 {
		t.Errorf("total fuel = %d, want 6600 - the money is knowable either way", got)
	}
}

// A retired car's clock stops on the day it went. Letting it run to today would
// go on quietly lowering its cost per day forever, which turns a finished
// record into a number that changes every night.
func TestARetiredVehicleStopsCounting(t *testing.T) {
	vehicle := wagon()
	vehicle.Retired = true
	vehicle.SoldOn = "2024-03-01"
	vehicle.SoldOdometer = 12000

	fills := []types.Fillup{
		fill("2024-01-05", 10300, 10, 300),
		fill("2024-01-19", 10700, 12, 300),
	}

	// A year after it went, the figures must be exactly what they were the day
	// it went.
	early := Analyze(vehicle, fills, nil, time.Date(2024, 3, 2, 12, 0, 0, 0, time.UTC))
	late := Analyze(vehicle, fills, nil, time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC))

	if early.Summary.DaysOwned != late.Summary.DaysOwned {
		t.Errorf("days owned drifted from %d to %d after it was sold",
			early.Summary.DaysOwned, late.Summary.DaysOwned)
	}

	if early.Summary.TotalPerDay != late.Summary.TotalPerDay {
		t.Errorf("cost a day drifted from %v to %v after it was sold",
			early.Summary.TotalPerDay, late.Summary.TotalPerDay)
	}

	// 1 January to 1 March.
	if got := late.Summary.DaysOwned; got != 60 {
		t.Errorf("days owned = %d, want 60", got)
	}

	if got := late.Summary.TotalMiles; got != 2000 {
		t.Errorf("total miles = %d, want 2000 - measured to the sale reading", got)
	}
}

// The cost of the car and the cost of running it are different questions, and
// mixing them makes the first one useless. The purchase price only appears when
// it is known, because a zero would read as a car that was free.
func TestOwnershipIsSeparateFromRunningCost(t *testing.T) {
	vehicle := wagon()

	without := Analyze(vehicle, []types.Fillup{fill("2024-01-05", 10300, 10, 300)}, nil, now)
	if without.Summary.HasPurchase || without.Summary.OwnershipCents != 0 {
		t.Error("a vehicle with no purchase price reports a cost of ownership anyway")
	}

	vehicle.PurchasePriceCents = 1750000
	vehicle.SoldPriceCents = 500000

	with := Analyze(vehicle, []types.Fillup{fill("2024-01-05", 10300, 10, 300)}, nil, now)

	// Bought for 17,500, sold for 5,000, 30 of fuel: 12,530.
	if got := with.Summary.OwnershipCents; got != 1750000-500000+3000 {
		t.Errorf("cost of ownership = %d cents, want 1253000", got)
	}

	if with.Summary.TotalCents != 3000 {
		t.Errorf("running cost = %d, want 3000 - the purchase does not belong in it", with.Summary.TotalCents)
	}
}

// The fuel saver is a per-gallon discount, so what it is worth on a tank
// depends on how big the tank was.
func TestTheFuelSaver(t *testing.T) {
	one := fill("2024-01-05", 10300, 10, 255)
	one.FuelSaverCents = 171

	two := fill("2024-01-19", 10700, 12, 300)

	report := Analyze(wagon(), []types.Fillup{one, two}, nil, now)

	if got := report.Summary.SavedCents; got != 1710 {
		t.Errorf("saved = %d cents, want 1710", got)
	}

	// Paid 25.50 + 36.00 = 61.50, would have been 78.60.
	if got := report.Summary.FuelCents; got != 6150 {
		t.Errorf("paid = %d cents, want 6150", got)
	}

	if got := report.Summary.SavedPercent; got < 21.7 || got > 21.9 {
		t.Errorf("saved = %.2f%% of the pump price, want about 21.8", got)
	}

	// The per-gallon figure is over the gallons a discount was actually used
	// on, not over every gallon - otherwise a year with one discounted fill
	// reports a discount of tuppence.
	if got := report.Summary.SavedPerGallon; got != 171 {
		t.Errorf("saved a gallon = %v cents, want 171", got)
	}
}

// One place, spelled three ways, is one place.
func TestStationsAreGroupedRegardlessOfSpelling(t *testing.T) {
	fills := []types.Fillup{
		fill("2024-01-05", 10300, 10, 300),
		fill("2024-01-19", 10700, 12, 300),
		fill("2024-02-02", 11100, 12, 300),
		fill("2024-02-16", 11500, 12, 300),
	}

	fills[0].Station = "Hy-Vee New Hope"
	fills[1].Station = "hy-vee new hope"
	fills[2].Station = "HyVee  New   Hope"
	fills[3].Station = ""

	report := Analyze(wagon(), fills, nil, now)

	if len(report.Stations) != 2 {
		t.Fatalf("%d stations, want 2 - one place and the unrecorded one: %+v", len(report.Stations), report.Stations)
	}

	if report.Stations[0].Fills != 3 {
		t.Errorf("the grouped station has %d fills, want 3", report.Stations[0].Fills)
	}

	// The first spelling seen is what the page shows.
	if report.Stations[0].Name != "Hy-Vee New Hope" {
		t.Errorf("station is called %q, want the first spelling seen", report.Stations[0].Name)
	}

	if report.Stations[1].Name != "Not recorded" {
		t.Errorf("a blank station is called %q", report.Stations[1].Name)
	}
}

// The oil interval is read out of the invoices rather than out of a checkbox
// nobody would tick, and it is a median so that one change done early - because
// the car was in for something else - does not lower the expectation for good.
func TestTheOilIntervalComesFromTheInvoices(t *testing.T) {
	visits := []types.Service{
		{On: "2024-01-10", Odometer: 10100, Work: "Oil change", CostCents: 8000},
		{On: "2024-02-10", Odometer: 15100, Work: "Alignment", CostCents: 9000},
		{On: "2024-03-10", Odometer: 18100, Work: "Oil change and filter", CostCents: 8000},
		{On: "2024-04-10", Odometer: 26100, Work: "80k service (oil and filter)", CostCents: 30000},
	}

	report := Analyze(wagon(), []types.Fillup{fill("2024-05-01", 27000, 12, 300)}, visits, now)

	if !report.Shop.HasOil {
		t.Fatal("no oil change was found in invoices that plainly mention one")
	}

	// Gaps of 8,000 and 8,000; the alignment in the middle is not an oil change.
	if got := report.Shop.OilIntervalMiles; got != 8000 {
		t.Errorf("oil interval = %d miles, want 8000", got)
	}

	if report.Shop.LastOil.Odometer != 26100 {
		t.Errorf("last oil change at %d, want 26100", report.Shop.LastOil.Odometer)
	}

	if report.Shop.Visits != 4 || report.Shop.TotalCents != 55000 {
		t.Errorf("shop stats = %d visits, %d cents", report.Shop.Visits, report.Shop.TotalCents)
	}
}

// Nothing in here may panic on an empty history, because a car somebody has
// just added has exactly that.
func TestAnEmptyHistoryIsNotAnError(t *testing.T) {
	report := Analyze(wagon(), nil, nil, now)

	if report.Any() {
		t.Error("an empty report says it has something in it")
	}

	if report.Summary.HasFills || report.Summary.CumulativeMPG != 0 {
		t.Error("an empty report claims a fuel figure")
	}

	if report.Outlook.Ready {
		t.Error("an empty report is willing to predict the future")
	}

	// One fill is still not enough to measure anything, and must not divide by
	// nought trying.
	one := Analyze(wagon(), []types.Fillup{fill("2024-01-05", 10300, 10, 300)}, nil, now)
	if one.Summary.MeasuredMPG != 0 || one.Summary.MeasuredFills != 0 {
		t.Error("a single fill-up produced an mpg out of nothing")
	}
}

// The order the fills arrive in must not change a single figure, because every
// one of them is read against its neighbour.
func TestTheOrderTheHistoryArrivesInDoesNotMatter(t *testing.T) {
	ordered := []types.Fillup{
		fill("2024-01-05", 10300, 10, 300),
		fill("2024-01-19", 10700, 12, 300),
		fill("2024-02-02", 11100, 12, 300),
	}

	shuffled := []types.Fillup{ordered[2], ordered[0], ordered[1]}

	a := Analyze(wagon(), ordered, nil, now)
	b := Analyze(wagon(), shuffled, nil, now)

	if a.Summary != b.Summary {
		t.Errorf("the same history in a different order produced different figures:\n%+v\n%+v", a.Summary, b.Summary)
	}
}

// The site's own copy of the caller's slice is what makes the test above safe;
// reordering somebody else's data is a surprise waiting to happen.
func TestAnalyzeDoesNotReorderWhatItIsGiven(t *testing.T) {
	given := []types.Fillup{
		fill("2024-02-02", 11100, 12, 300),
		fill("2024-01-05", 10300, 10, 300),
	}

	Analyze(wagon(), given, nil, now)

	if given[0].On != "2024-02-02" {
		t.Error("Analyze sorted the caller's slice underneath it")
	}
}

func TestSummerIsAprilToSeptember(t *testing.T) {
	for _, month := range []time.Month{time.April, time.July, time.September} {
		if !Summer(time.Date(2024, month, 15, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("%s is not counted as summer", month)
		}
	}

	for _, month := range []time.Month{time.October, time.January, time.March} {
		if Summer(time.Date(2024, month, 15, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("%s is counted as summer", month)
		}
	}
}
