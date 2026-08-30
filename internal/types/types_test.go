package types

import (
	"testing"
	"time"
)

// The id is what makes an import safe to run twice, and what makes correcting a
// date move an entry rather than duplicate it. It is worth pinning exactly.
func TestEntryIDsAreTheDayAndTheReading(t *testing.T) {
	if got := FillupID("2026-08-11", 95176); got != "2026-08-11-95176" {
		t.Errorf("FillupID = %q", got)
	}

	if got := ServiceID("2026-08-07", 95077); got != "2026-08-07-95077" {
		t.Errorf("ServiceID = %q", got)
	}
}

// Cost is worked out from the gallons and the price, except on the one receipt
// that does not come out even.
func TestCost(t *testing.T) {
	fill := Fillup{Gallons: 12.475, PricePerGallonCents: 392}

	// 12.475 * 3.92 = 48.902, which a pump rounds to 48.90.
	if got := fill.Cost(); got != 4890 {
		t.Errorf("Cost() = %d, want 4890", got)
	}

	fill.CostCents = 4900
	if got := fill.Cost(); got != 4900 {
		t.Errorf("a stored total was ignored: %d", got)
	}
}

// The saver is per gallon, so what it is worth depends on the size of the tank,
// and the pump price is the two added back together.
func TestTheFuelSaverArithmetic(t *testing.T) {
	fill := Fillup{Gallons: 12.678, PricePerGallonCents: 255, FuelSaverCents: 171}

	if got := fill.PumpPriceCents(); got != 426 {
		t.Errorf("PumpPriceCents() = %d, want 426", got)
	}

	// 12.678 * 1.71 = 21.679, rounding to 21.68 - which is what the sheet's own
	// fuel saver tab says for this row.
	if got := fill.Saved(); got != 2168 {
		t.Errorf("Saved() = %d, want 2168", got)
	}

	if !fill.Discounted() {
		t.Error("a fill with a discount on it says it has none")
	}

	if (Fillup{Gallons: 10, PricePerGallonCents: 300}).Saved() != 0 {
		t.Error("a fill with no discount saved something")
	}
}

func TestFullTank(t *testing.T) {
	if !(Fillup{}).FullTank() {
		t.Error("an ordinary fill does not count as a full tank")
	}

	if (Fillup{Partial: true}).FullTank() {
		t.Error("a partial fill counts as a full tank")
	}

	if (Fillup{Missed: true}).FullTank() {
		t.Error("a fill with a known gap before it counts as a full tank")
	}
}

// Days are counted between days rather than between instants, so two entries a
// few hours apart on one date are nought days apart - and a span across a
// daylight saving change is still a whole number of them.
func TestDaysBetween(t *testing.T) {
	morning := time.Date(2024, 3, 9, 7, 0, 0, 0, Local)
	evening := time.Date(2024, 3, 9, 22, 0, 0, 0, Local)

	if got := DaysBetween(morning, evening); got != 0 {
		t.Errorf("two times on one day are %d days apart", got)
	}

	// American daylight saving started on 10 March 2024, so this span is 23
	// hours long and is still one day.
	across := time.Date(2024, 3, 10, 7, 0, 0, 0, Local)
	if got := DaysBetween(morning, across); got != 1 {
		t.Errorf("across the clocks going forward = %d days, want 1", got)
	}

	// The sheet's own span: 28 December 2021 to 29 August 2026.
	if got := DaysBetween(ParseDate("2021-12-28"), ParseDate("2026-08-29")); got != 1705 {
		t.Errorf("the sheet's ownership span = %d days, want 1705", got)
	}

	if got := DaysBetween(time.Time{}, across); got != 0 {
		t.Errorf("a span from nothing = %d days, want 0", got)
	}
}

func TestParseDate(t *testing.T) {
	if got := ParseDate("2026-08-11"); got.IsZero() {
		t.Error("a stored date would not parse")
	}

	for _, bad := range []string{"", "not a date", "08/11/2026", "2026-13-45"} {
		if !ParseDate(bad).IsZero() {
			t.Errorf("ParseDate(%q) came back with a date", bad)
		}
	}
}

// Sorting is not a presentation choice here: every figure downstream is read
// from a fill against its neighbour, so the tie-break matters as much as the
// order does. The sheet has two fill-ups on 13 August 2022.
func TestByDateBreaksTiesOnTheOdometer(t *testing.T) {
	fills := []Fillup{
		{On: "2022-08-13", Odometer: 73563},
		{On: "2022-08-10", Odometer: 72855},
		{On: "2022-08-13", Odometer: 73137},
	}

	ByDate(fills)

	want := []int{72855, 73137, 73563}
	for at, odometer := range want {
		if fills[at].Odometer != odometer {
			t.Errorf("position %d is %d, want %d", at, fills[at].Odometer, odometer)
		}
	}
}

// Sharing is per-vehicle, and the two questions it answers are different: who
// may log against this car, and who may change what the car is.
func TestWhoCanSeeAndManageAVehicle(t *testing.T) {
	vehicle := Vehicle{
		Owner:      "anthony@example.com",
		SharedWith: []string{"partner@example.com"},
	}

	for _, address := range []string{"anthony@example.com", "ANTHONY@example.com", "partner@example.com"} {
		if !vehicle.CanBeSeenBy(address) {
			t.Errorf("%s cannot see a vehicle they own or are shared on", address)
		}
	}

	for _, address := range []string{"", "stranger@example.com"} {
		if vehicle.CanBeSeenBy(address) {
			t.Errorf("%q can see a vehicle nobody shared with them", address)
		}
	}

	owner := User{Email: "anthony@example.com"}
	shared := User{Email: "partner@example.com"}
	admin := User{Email: "admin@example.com", Admin: true}
	readOnly := User{Email: "anthony@example.com", ViewOnly: true}

	if !vehicle.CanBeManagedBy(owner) {
		t.Error("the owner cannot manage their own vehicle")
	}

	if vehicle.CanBeManagedBy(shared) {
		t.Error("somebody shared on a vehicle can change what it is")
	}

	if !vehicle.CanBeManagedBy(admin) {
		t.Error("an admin cannot manage a vehicle")
	}

	if vehicle.CanBeManagedBy(readOnly) {
		t.Error("a view-only account can manage a vehicle, even its own")
	}
}

func TestVehicleNaming(t *testing.T) {
	full := Vehicle{Slug: "the-wagon", Name: "The wagon", Year: 2016, Make: "Volkswagen", Model: "Golf SportWagen", Trim: "S"}

	if got := full.Described(); got != "2016 Volkswagen Golf SportWagen S" {
		t.Errorf("Described() = %q", got)
	}

	if got := full.Called(); got != "The wagon" {
		t.Errorf("Called() = %q", got)
	}

	// A vehicle always has something to be called, however little is on it.
	bare := Vehicle{Slug: "the-wagon"}
	if got := bare.Called(); got != "the-wagon" {
		t.Errorf("a vehicle with only a slug is called %q", got)
	}

	if got := (Vehicle{Slug: "x", Make: "Volkswagen"}).Called(); got != "Volkswagen" {
		t.Errorf("a vehicle with a make and no name is called %q", got)
	}
}

func TestServiceWork(t *testing.T) {
	visit := Service{Work: "Oil change\n\nTire rotation\n  Cabin filter  "}

	items := visit.Items()
	if len(items) != 3 {
		t.Fatalf("Items() = %v, want three", items)
	}

	if items[2] != "Cabin filter" {
		t.Errorf("Items()[2] = %q, want it trimmed", items[2])
	}

	if got := visit.Summary(); got != "Oil change, and 2 more" {
		t.Errorf("Summary() = %q", got)
	}

	if got := (Service{Work: "Oil change"}).Summary(); got != "Oil change" {
		t.Errorf("a single item summarises as %q", got)
	}

	if got := (Service{}).Summary(); got != "Service" {
		t.Errorf("a visit with no work summarises as %q", got)
	}

	// The oil interval is found by reading the invoice, so the match has to be
	// case-insensitive - "Oil Change", "oil and filter", "80k service (oil)".
	if !visit.Mentions("OIL") {
		t.Error("Mentions is case-sensitive, so half the invoices would not match")
	}
}

func TestAccountName(t *testing.T) {
	cases := map[string]string{
		"anthony@example.com": "anthony",
		"":                    "somebody",
		"nonsense":            "nonsense",
	}

	for address, want := range cases {
		if got := AccountName(address); got != want {
			t.Errorf("AccountName(%q) = %q, want %q", address, got, want)
		}
	}

	if got := (User{Email: "anthony@example.com", Name: "Anthony"}).Called(); got != "Anthony" {
		t.Errorf("User.Called() = %q", got)
	}
}
