package importer

import (
	"strings"
	"testing"

	"github.com/Niximacco/car-tracker/internal/types"
)

var wagon = types.Vehicle{Slug: "the-wagon", TankGallons: 13.5}

func parse(t *testing.T, text string) Preview {
	t.Helper()

	preview, err := Parse(text, wagon, wagon.TankGallons)
	if err != nil {
		t.Fatalf("Parse() failed: %s", err.Error())
	}

	return preview
}

// The exact shape the Google Sheet exports: the form's own column headings, the
// six columns of arithmetic this site does for itself, and a blank tail.
func TestTheSheetsOwnExport(t *testing.T) {
	preview := parse(t, `Timestamp,Log Type,Note / Location,Maintenance Done,Cost,Odometer ,Gallons Purchased,Price per Gallon,Fuel Saver,Difference,MPG,Running Cost,Days owned,Cost per Day
12/30/2021 1:47:10,Gas Fill-up,Crystal Holiday,,,68350,12,3.5,,,,,,
1/12/2022 1:47:23,Gas Fill-up,Apple Valley Holiday,,,68720,11.366,3.46,,370,32.55322893,,,
11/19/2022 14:17:56,,Hy-Vee apple valley. 1.28 of fuel saver,,,76307,12.475,3.92,1.28,442,35.43086172,,,
,,,,,,,,,,,,,
,,,,,,,,,,,,,`)

	if preview.Fillups != 3 {
		t.Fatalf("read %d fill-ups, want 3", preview.Fillups)
	}

	if preview.Bad != 0 {
		t.Errorf("%d rows would not read", preview.Bad)
	}

	// The blank tail is the end of the sheet, not a hundred errors.
	if len(preview.Rows) != 3 {
		t.Errorf("kept %d rows, want 3 - the blank lines at the end are not entries", len(preview.Rows))
	}

	first := preview.Rows[0].Fillup
	if first.On != "2021-12-30" {
		t.Errorf("first date = %q, want 2021-12-30", first.On)
	}

	// The timestamp carries a time of day and it is worth keeping.
	if first.At != "01:47" {
		t.Errorf("first time = %q, want 01:47", first.At)
	}

	if first.Odometer != 68350 || first.Gallons != 12 || first.PricePerGallonCents != 350 {
		t.Errorf("first fill = %d miles, %v gallons, %d cents", first.Odometer, first.Gallons, first.PricePerGallonCents)
	}

	// The columns this site works out for itself must not come in. Reading the
	// sheet's Difference column into anything would mean an import that carries
	// its own stale arithmetic.
	if first.CostCents != 0 {
		t.Errorf("first fill stored a total of %d cents from a sheet that has no total on it", first.CostCents)
	}

	third := preview.Rows[2].Fillup
	if third.FuelSaverCents != 128 {
		t.Errorf("fuel saver = %d cents, want 128", third.FuelSaverCents)
	}
}

// The maintenance sheet is the same reader with different columns filled in,
// and its work cell has line breaks inside it.
func TestTheMaintenanceSheet(t *testing.T) {
	preview := parse(t, `Date,Note / Location,Maintenance Done,Cost,Odometer
01/03/2022,West Side VW,"DSG 70k Maintenance
Cabin Air Filter / Pollen Filter",767.94,68516
8/19/2022,West Side VW,Key Programming,"$861.40",73932`)

	if preview.Services != 2 || preview.Fillups != 0 {
		t.Fatalf("read %d visits and %d fill-ups, want 2 and 0", preview.Services, preview.Fillups)
	}

	first := preview.Rows[0].Service
	if first.CostCents != 76794 || first.Odometer != 68516 {
		t.Errorf("first visit = %d cents at %d miles", first.CostCents, first.Odometer)
	}

	if len(first.Items()) != 2 {
		t.Errorf("first visit has %d items, want 2 - the line break inside the cell was lost", len(first.Items()))
	}

	// A cost typed with a symbol and a comma is a cost.
	if preview.Rows[1].Service.CostCents != 86140 {
		t.Errorf("second visit = %d cents, want 86140", preview.Rows[1].Service.CostCents)
	}
}

// A copy and paste out of the browser arrives tab separated, and a pasted cell
// can easily contain a comma.
func TestAPasteFromTheBrowser(t *testing.T) {
	preview := parse(t, "Date\tNote / Location\tOdometer\tGallons Purchased\tPrice per Gallon\n"+
		"8/13/2022\tWilson, Wisconsin\t73563\t10.713\t4.5")

	if preview.Fillups != 1 {
		t.Fatalf("read %d fill-ups, want 1", preview.Fillups)
	}

	if got := preview.Rows[0].Fillup.Station; got != "Wilson, Wisconsin" {
		t.Errorf("station = %q, want %q - the comma inside the cell split it", got, "Wilson, Wisconsin")
	}
}

// One bad line must not cost you the other sixty-six.
func TestABadLineDoesNotStopTheRest(t *testing.T) {
	preview := parse(t, `Date,Note / Location,Odometer,Gallons Purchased,Price per Gallon
1/1/2024,Somewhere,90000,12,3.20
not a date,Somewhere,90400,12,3.20
1/20/2024,Somewhere,,12,3.20
2/1/2024,Somewhere,90800,12,3.20`)

	if preview.Fillups != 2 {
		t.Errorf("read %d good fill-ups, want 2", preview.Fillups)
	}

	if preview.Bad != 2 {
		t.Errorf("found %d bad lines, want 2", preview.Bad)
	}

	// Every bad row keeps its line number and its text, because "row 3 is
	// wrong" is actionable and "something is wrong" is not.
	for _, row := range preview.Rows {
		if row.Readable() {
			continue
		}

		if row.Line == 0 || row.Raw == "" || row.Problem == "" {
			t.Errorf("a bad row came back without enough to find it: %+v", row)
		}
	}

	// Nothing unreadable goes anywhere near datastore.
	if len(preview.FillupsToImport()) != 2 {
		t.Errorf("%d fill-ups would be written, want 2", len(preview.FillupsToImport()))
	}
}

// Three things read perfectly and are still worth a second look. None of them
// is refused, because every one is occasionally true.
func TestTheOddRowsAreFlaggedRatherThanRefused(t *testing.T) {
	preview := parse(t, `Date,Note / Location,Odometer,Gallons Purchased,Price per Gallon,Fuel Saver
1/1/2024,Somewhere,90000,12,3.20,
1/15/2024,Somewhere,90400,40,3.20,
2/1/2024,Somewhere,89000,12,3.20,
2/15/2024,Somewhere,91000,12,0.81,3.09`)

	if preview.Fillups != 4 {
		t.Fatalf("read %d fill-ups, want all 4 kept", preview.Fillups)
	}

	if preview.Bad != 0 {
		t.Errorf("%d rows were refused - none of these should be", preview.Bad)
	}

	flags := []string{}
	for _, row := range preview.Rows {
		flags = append(flags, row.Odd)
	}

	if !strings.Contains(flags[1], "tank") {
		t.Errorf("40 gallons was not flagged as bigger than the tank: %q", flags[1])
	}

	if !strings.Contains(flags[2], "backwards") {
		t.Errorf("an odometer going from 90400 to 89000 was not flagged: %q", flags[2])
	}

	if !strings.Contains(flags[3], "right way round") {
		t.Errorf("a saver larger than the price was not flagged: %q", flags[3])
	}
}

// Importing the same export twice has to update rather than double, and the id
// is what makes that true.
func TestTheIDIsTheDayAndTheReading(t *testing.T) {
	preview := parse(t, `Date,Note / Location,Odometer,Gallons Purchased,Price per Gallon
8/11/2026,Hy-Vee New Hope,95176,12.818,4.95`)

	if got := preview.Rows[0].Fillup.ID; got != "2026-08-11-95176" {
		t.Errorf("id = %q, want 2026-08-11-95176", got)
	}
}

// A header that is not a header is worth refusing outright, because the
// alternative is reading the first row of data as column names and then
// reporting every remaining line as broken.
func TestAMissingHeaderIsRefused(t *testing.T) {
	for _, text := range []string{
		"",
		"12/30/2021,Crystal Holiday,68350,12,3.5",
		"Date,Note / Location,Gallons Purchased\n1/1/2024,Somewhere,12",
	} {
		if _, err := Parse(text, wagon, 13.5); err == nil {
			t.Errorf("Parse(%q) was accepted, want an error naming the missing column", text)
		}
	}
}

// A receipt whose total is not the gallons times the price is a real receipt,
// and it is the total that is right.
func TestATotalThatDisagreesIsKept(t *testing.T) {
	preview := parse(t, `Date,Odometer,Gallons Purchased,Price per Gallon,Cost
1/1/2024,90000,12,3.20,38.40
1/15/2024,90400,12,3.20,41.00`)

	if got := preview.Rows[0].Fillup.CostCents; got != 0 {
		t.Errorf("a total that agrees with the arithmetic was stored anyway: %d", got)
	}

	if got := preview.Rows[1].Fillup.CostCents; got != 4100 {
		t.Errorf("a total that disagrees was dropped: %d, want 4100", got)
	}

	if got := preview.Rows[1].Fillup.Cost(); got != 4100 {
		t.Errorf("the stored total is not what the fill-up costs: %d", got)
	}
}

// A row with a total and no unit price still has a unit price.
func TestAUnitPriceIsWorkedOutFromATotal(t *testing.T) {
	preview := parse(t, `Date,Odometer,Gallons Purchased,Cost
1/1/2024,90000,10,32.00`)

	if got := preview.Rows[0].Fillup.PricePerGallonCents; got != 320 {
		t.Errorf("price a gallon = %d, want 320", got)
	}
}

func TestParseWhen(t *testing.T) {
	cases := []struct {
		typed string
		date  string
		clock string
	}{
		{"12/30/2021 1:47:10", "2021-12-30", "01:47"},
		{"01/03/2022", "2022-01-03", ""},
		{"8/19/2022", "2022-08-19", ""},
		{"1/15/24", "2024-01-15", ""},
		{"2026-08-11", "2026-08-11", ""},
		{"Aug 11, 2026", "2026-08-11", ""},
		// Midnight is what a date with no time on it parses to, so it is not
		// reported as a time of day - "00:00" on every row of a four year
		// import would be noise claiming to be data.
		{"1/1/2024 00:00:00", "2024-01-01", ""},
	}

	for _, one := range cases {
		date, clock, err := ParseWhen(one.typed)
		if err != nil {
			t.Errorf("ParseWhen(%q) failed: %s", one.typed, err.Error())
			continue
		}

		if got := date.Format(types.DateLayout); got != one.date {
			t.Errorf("ParseWhen(%q) = %s, want %s", one.typed, got, one.date)
		}

		if clock != one.clock {
			t.Errorf("ParseWhen(%q) clock = %q, want %q", one.typed, clock, one.clock)
		}
	}

	for _, typed := range []string{"", "not a date", "13/45/2021"} {
		if _, _, err := ParseWhen(typed); err == nil {
			t.Errorf("ParseWhen(%q) was accepted", typed)
		}
	}
}
