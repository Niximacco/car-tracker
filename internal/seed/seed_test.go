package seed

import (
	"testing"
	"time"

	"github.com/Niximacco/car-tracker/internal/stats"
	"github.com/Niximacco/car-tracker/internal/types"
)

// The whole point of this test is that these numbers came off the bottom of the
// spreadsheet rather than out of this code.
//
// They are what has been looked at for four years, so if a change to the stats
// package moves one of them, that is either a bug or a decision - and either
// way it is worth finding out here rather than by noticing that the front page
// says something different from what it said last week.
//
// The summary block read, on the day the sheet was transcribed:
//
//	Total Miles              27,102        16.07 miles/day
//	Days Owned Total          1,705
//	Days Owned @ Last Fill    1,687
//	Total Gallons           786.852        $3.61/gal average
//	Total Fuel Cost        $2,839.63       0.105/mile   $1.67/day   $50.66/month
//	Total Maintenance      $5,759.35       0.213/mile   $3.38/day  $102.75/month
//	Total Cost             $8,598.98       0.317/mile   $5.04/day  $153.40/month
//	Cumulative MPG        34.44358024
const (
	sheetMiles         = 27102
	sheetDaysOwned     = 1705
	sheetDaysAtFill    = 1687
	sheetGallons       = 786.852
	sheetMaintenance   = 575935
	sheetCumulativeMPG = 34.44358024
)

// asOf is the day the sheet's summary was read. Days owned counts to today, so
// the test has to name the day or it fails once a night.
var asOf = time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

func report(t *testing.T) stats.Report {
	t.Helper()

	vehicle := Vehicle("anthony@example.com")

	preview, err := Load(vehicle)
	if err != nil {
		t.Fatalf("Load() failed: %s", err.Error())
	}

	if preview.Bad != 0 {
		for _, row := range preview.Rows {
			if !row.Readable() {
				t.Errorf("line %d does not read: %s (%s)", row.Line, row.Problem, row.Raw)
			}
		}

		t.Fatalf("%d rows of the built-in spreadsheet do not read", preview.Bad)
	}

	return stats.Analyze(vehicle, preview.FillupsToImport(), preview.ServicesToImport(), asOf)
}

func TestTheHistoryIsAllThere(t *testing.T) {
	got := report(t)

	if len(got.Fills) != 67 {
		t.Errorf("read %d fill-ups, want 67", len(got.Fills))
	}

	if len(got.Services) != 11 {
		t.Errorf("read %d shop visits, want 11", len(got.Services))
	}

	// The last row of the sheet, which is also where every lifetime figure
	// stops.
	last := got.Fills[len(got.Fills)-1]
	if last.On != "2026-08-11" || last.Odometer != 95176 {
		t.Errorf("last fill-up is %s at %d, want 2026-08-11 at 95176", last.On, last.Odometer)
	}
}

func TestTheSummaryMatchesTheSpreadsheet(t *testing.T) {
	summary := report(t).Summary

	if summary.TotalMiles != sheetMiles {
		t.Errorf("total miles = %d, the sheet says %d", summary.TotalMiles, sheetMiles)
	}

	if summary.DaysOwned != sheetDaysOwned {
		t.Errorf("days owned = %d, the sheet says %d", summary.DaysOwned, sheetDaysOwned)
	}

	if summary.DaysAtLastFill != sheetDaysAtFill {
		t.Errorf("days owned at the last fill = %d, the sheet says %d", summary.DaysAtLastFill, sheetDaysAtFill)
	}

	if !near(summary.TotalGallons, sheetGallons, 0.0005) {
		t.Errorf("total gallons = %.3f, the sheet says %.3f", summary.TotalGallons, sheetGallons)
	}

	if summary.ServiceCents != sheetMaintenance {
		t.Errorf("total maintenance = %d cents, the sheet says %d", summary.ServiceCents, sheetMaintenance)
	}

	// The fuel total is allowed a few cents of daylight. The sheet multiplies
	// every row and adds the products; this site rounds each receipt to whole
	// cents and adds those, because that is what you actually paid. Over 67
	// fill-ups the two answers differ by less than a nickel, and the one here
	// is the one that can be checked against a bank statement.
	if !nearCents(summary.FuelCents, 283963, 10) {
		t.Errorf("total fuel = %d cents, the sheet says 283963", summary.FuelCents)
	}

	if !nearCents(summary.TotalCents, 859898, 10) {
		t.Errorf("total cost = %d cents, the sheet says 859898", summary.TotalCents)
	}

	if !near(summary.CumulativeMPG, sheetCumulativeMPG, 0.0001) {
		t.Errorf("cumulative mpg = %.6f, the sheet says %.6f", summary.CumulativeMPG, sheetCumulativeMPG)
	}

	if !near(summary.MilesPerDay, 16.07, 0.01) {
		t.Errorf("miles a day = %.2f, the sheet says 16.07", summary.MilesPerDay)
	}
}

// The three costs, each read three ways. These are the twelve cells the sheet
// carried in its summary block, and they are what "what does this car cost"
// actually means.
func TestTheThreeCostsReadThreeWays(t *testing.T) {
	summary := report(t).Summary

	cases := []struct {
		name string
		got  float64
		want float64
		tol  float64
	}{
		{"fuel a mile", summary.FuelPerMile / 100, 0.105, 0.0005},
		{"fuel a day", summary.FuelPerDay / 100, 1.67, 0.005},
		{"fuel a month", summary.FuelPerMonth / 100, 50.66, 0.02},

		{"maintenance a mile", summary.ServicePerMile / 100, 0.213, 0.0005},
		{"maintenance a day", summary.ServicePerDay / 100, 3.38, 0.005},
		{"maintenance a month", summary.ServicePerMonth / 100, 102.75, 0.02},

		{"total a mile", summary.TotalPerMile / 100, 0.317, 0.0005},
		{"total a day", summary.TotalPerDay / 100, 5.04, 0.005},
		{"total a month", summary.TotalPerMonth / 100, 153.40, 0.02},

		{"average a gallon", summary.AvgPriceCents / 100, 3.61, 0.005},
	}

	for _, one := range cases {
		if !near(one.got, one.want, one.tol) {
			t.Errorf("%s = %.4f, the sheet says %.4f", one.name, one.got, one.want)
		}
	}
}

// The fuel saver sheet kept its own per-year totals. They are the one figure in
// the workbook this site works out from the same column the sheet did, so they
// should agree exactly.
func TestTheFuelSaverTotalsMatchTheSheet(t *testing.T) {
	years := map[int]int64{}
	for _, year := range report(t).Years {
		years[year.Year] = year.SavedCents
	}

	// 2022 and 2023 are the sheet's own numbers. 2024 onwards the sheet has two
	// of its columns the wrong way round for a stretch of rows, which is
	// exactly the sort of thing that stops being possible once the arithmetic
	// is done from the data rather than typed beside it - so those years are
	// checked against what the fill-up rows actually say.
	want := map[int]int64{
		2022: 2995,
		2023: 17344,
	}

	for year, cents := range want {
		if years[year] != cents {
			t.Errorf("saved in %d = %d cents, the sheet says %d", year, years[year], cents)
		}
	}

	if total := report(t).Summary.SavedCents; total < 45000 || total > 55000 {
		t.Errorf("lifetime fuel saver = %d cents, which is nowhere near the sheet's ~$506", total)
	}
}

// Every fill-up but the first should carry an mpg, because the history has no
// partials and no gaps marked in it. A change that quietly stops measuring
// tanks would show up here before it showed up as a chart with holes in it.
func TestEveryTankButTheFirstIsMeasured(t *testing.T) {
	got := report(t)

	if got.Summary.MeasuredFills != len(got.Fills)-1 {
		t.Errorf("%d of %d fill-ups are measured, want all but the first",
			got.Summary.MeasuredFills, len(got.Fills))
	}

	if got.Fills[0].Measured {
		t.Error("the first fill-up carries an mpg, which it has nothing to measure against")
	}

	// The sheet's own second row: 370 miles on 11.366 gallons.
	second := got.Fills[1]
	if second.Miles != 370 || !near(second.MPG, 32.55322893, 0.00001) {
		t.Errorf("second fill = %d miles at %.6f mpg, the sheet says 370 at 32.553229",
			second.Miles, second.MPG)
	}
}

// The stations are the reason the transcription folds "Hy-Vee new hope" and
// "Hy-Vee New Hope" together. A breakdown that lists one place twice is a
// breakdown of nothing.
func TestTheStationsAreOnePlaceEach(t *testing.T) {
	got := report(t)

	seen := map[string]bool{}
	for _, station := range got.Stations {
		key := stats.StationKey(station.Name)
		if seen[key] {
			t.Errorf("%q appears twice in the station breakdown", station.Name)
		}

		seen[key] = true
	}

	// Hy-Vee New Hope is far and away the usual stop, so it has to be first.
	if len(got.Stations) == 0 || stats.StationKey(got.Stations[0].Name) != "hyvee new hope" {
		t.Errorf("the most-used station is %v, want Hy-Vee New Hope", got.Stations)
	}
}

func TestVehicleCarriesWhereTheClockStarts(t *testing.T) {
	vehicle := Vehicle("anthony@example.com")

	if vehicle.PurchasedOn != PurchasedOn || vehicle.PurchaseOdometer != PurchaseOdometer {
		t.Errorf("the seeded vehicle starts at %s / %d, want %s / %d",
			vehicle.PurchasedOn, vehicle.PurchaseOdometer, PurchasedOn, PurchaseOdometer)
	}

	if !vehicle.Owned() {
		t.Error("the seeded vehicle has no readable purchase date, so every per-day figure would be missing")
	}

	if vehicle.Owner != "anthony@example.com" {
		t.Errorf("the seeded vehicle belongs to %q", vehicle.Owner)
	}
}

func near(got float64, want float64, tolerance float64) bool {
	difference := got - want
	if difference < 0 {
		difference = -difference
	}

	return difference <= tolerance
}

func nearCents(got int64, want int64, tolerance int64) bool {
	difference := got - want
	if difference < 0 {
		difference = -difference
	}

	return difference <= tolerance
}

// The site's clock has to be a real zone or every date in the history shifts.
func TestTheSiteClockLoaded(t *testing.T) {
	if types.Local == nil {
		t.Fatal("the site has no location loaded")
	}
}
