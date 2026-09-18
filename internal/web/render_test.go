package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Niximacco/car-tracker/internal/importer"
	"github.com/Niximacco/car-tracker/internal/stats"
	"github.com/Niximacco/car-tracker/internal/types"
	"github.com/gin-gonic/gin"
)

// Templates fail at render time, not at compile time, so every page gets
// rendered here with everything populated. A typo in a field name shows up as a
// truncated page rather than a build error - which is a page that looks fine
// until the one section below the typo is missing.
func render(t *testing.T, name string, page Page) string {
	t.Helper()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	Render(c, http.StatusOK, name, page)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Render(%s) wrote status %d, want 200", name, recorder.Code)
	}

	body := recorder.Body.String()
	if !strings.HasPrefix(body, "<!doctype html>") || !strings.Contains(body, "</html>") {
		t.Fatalf("Render(%s) did not produce a whole page:\n%s", name, body)
	}

	return body
}

// history is a short but complete run: a purchase, five fill-ups with a partial
// in the middle of them, and two visits to a shop.
//
// The partial is there on purpose. It is the one shape in the data that makes a
// row render differently from its neighbours, and the templates that would
// break on it are the ones that assume every fill has an mpg.
func history() (types.Vehicle, stats.Report) {
	vehicle := types.Vehicle{
		Slug:               "the-wagon",
		Name:               "The wagon",
		Year:               2016,
		Make:               "Volkswagen",
		Model:              "Golf SportWagen",
		Trim:               "S",
		Plate:              "ABC 123",
		VIN:                "0000000000000000",
		Owner:              "anthony@example.com",
		SharedWith:         []string{"someone@example.com"},
		PurchasedOn:        "2021-12-28",
		PurchaseOdometer:   68074,
		PurchasePriceCents: 1750000,
		TankGallons:        13.5,
		Created:            1640000000,
	}

	fills := []types.Fillup{
		{On: "2021-12-30", At: "13:47", Odometer: 68350, Gallons: 12, PricePerGallonCents: 350, Station: "Crystal Holiday"},
		{On: "2022-01-12", Odometer: 68720, Gallons: 11.366, PricePerGallonCents: 346, Station: "Apple Valley Holiday"},
		{On: "2022-02-21", Odometer: 69605, Gallons: 13.481, PricePerGallonCents: 380, Station: "Crystal Holiday",
			Note: "Gas spilled during fill-up", Partial: true},
		{On: "2022-11-19", Odometer: 76307, Gallons: 12.475, PricePerGallonCents: 392, FuelSaverCents: 128,
			Station: "Hy-Vee Apple Valley"},
		{On: "2023-06-18", Odometer: 79974, Gallons: 12.508, PricePerGallonCents: 344, FuelSaverCents: 55,
			Station: "Hy-Vee New Hope", Missed: true},
	}

	for at := range fills {
		fills[at].Vehicle = vehicle.Slug
		fills[at].ID = types.FillupID(fills[at].On, fills[at].Odometer)
		fills[at].By = "anthony@example.com"
	}

	services := []types.Service{
		{On: "2022-01-03", Odometer: 68516, Shop: "West Side VW", CostCents: 76794,
			Work: "DSG 70k maintenance\nCabin air filter"},
		{On: "2023-06-15", Odometer: 79934, Shop: "West Side VW", CostCents: 108954,
			Work: "80k service (oil and filter)\nAlignment", Note: "Rear pads were down to 3mm"},
	}

	for at := range services {
		services[at].Vehicle = vehicle.Slug
		services[at].ID = types.ServiceID(services[at].On, services[at].Odometer)
	}

	now := time.Date(2023, 8, 1, 12, 0, 0, 0, time.UTC)

	return vehicle, stats.Analyze(vehicle, fills, services, now)
}

func samplePage(title string) Page {
	vehicle, report := history()

	page := New(title)

	page.Email = "anthony@example.com"
	page.SignedIn = true
	page.IsAdmin = true
	page.Nav = "garage"
	page.Next = "/vehicle/the-wagon"
	page.Error = "an error happened"
	page.Message = "a message"
	page.Notice = "Saved."
	page.ExpiresMinutes = 15

	page.Vehicle = vehicle
	page.Report = report
	page.CanManage = true

	page.Vehicles = []types.Vehicle{vehicle, {Slug: "the-truck", Name: "The truck", Owner: "someone@example.com", Retired: true}}
	page.Cards = []Card{
		{Vehicle: vehicle, Summary: report.Summary, LastOn: report.Summary.LastOn},
		{Vehicle: page.Vehicles[1], Shared: true},
	}

	page.MPGChart = MPGChart(report)
	page.PriceChart = PriceChart(report)
	page.CostChart = CostChart(report)
	page.PerMileChart = PerMileChart(report)
	page.SpendChart = SpendChart(report)
	page.MilesChart = MilesChart(report)
	page.TotalChart = TotalChart(report)

	page.WidestYear = 200000
	page.WidestStation = 3
	page.WidestMonth = 40
	page.WidestPeriod = 200000
	page.WidestDay = 3

	page.Fillup = report.Fills[0].Fillup
	page.Service = report.Services[0]
	page.Editing = report.Fills[0].ID

	page.Users = []types.User{
		{Email: "anthony@example.com", Name: "Anthony", Admin: true, Created: 1640000000, LastLogin: 1690000000},
		{Email: "someone@example.com", ViewOnly: true, Created: 1640000000},
		{Email: "gone@example.com", Disabled: true},
	}

	page.You = page.Users[0]
	page.You.DefaultVehicle = vehicle.Slug

	page.SeedFillups = 67
	page.SeedServices = 11

	return page
}

func TestEveryPageRenders(t *testing.T) {
	for _, name := range []string{
		LoginPage, SentPage, MessagePage, GaragePage, VehiclePage, FillupsPage,
		ServicesPage, InsightsPage, SettingsPage, NewCarPage, ImportPage,
		UsersPage, ProfilePage,
	} {
		t.Run(name, func(t *testing.T) {
			render(t, name, samplePage("A page"))
		})
	}
}

// The pages have to survive a request from somebody who has just signed in and
// has nothing at all: no vehicles, no history, no charts. Every one of them
// draws a shape that the populated case never exercises.
func TestEveryPageRendersEmpty(t *testing.T) {
	for _, name := range []string{
		LoginPage, SentPage, MessagePage, GaragePage, VehiclePage, FillupsPage,
		ServicesPage, InsightsPage, SettingsPage, NewCarPage, ImportPage,
		UsersPage, ProfilePage,
	} {
		t.Run(name, func(t *testing.T) {
			page := New("A page")
			page.SignedIn = true
			page.Email = "nobody@example.com"

			render(t, name, page)
		})
	}
}

// A view-only account must not be offered a button that would come back
// refused, and a nav that offered "Add a vehicle" to somebody who cannot is the
// most visible version of that.
func TestAViewOnlyAccountIsNotOfferedTheAddButton(t *testing.T) {
	page := samplePage("Garage")
	page.ViewOnly = true
	page.IsAdmin = false
	page.CanManage = false

	body := render(t, GaragePage, page)

	if strings.Contains(body, "/vehicles/new") {
		t.Error("the garage offers a view-only account a link to add a vehicle")
	}

	// The same applies to the two things on a card that write: logging against a
	// vehicle, and choosing which one the site opens on.
	for _, offer := range []string{"/fuel#add", "/service#add", "/vehicles/default"} {
		if strings.Contains(body, offer) {
			t.Errorf("the garage offers a view-only account %s", offer)
		}
	}
}

// Logging a fill-up is what somebody standing at a pump opened the site to do,
// so it has to be on the card rather than two pages behind it - and the card
// has to say which vehicle the site now opens on, since that is the only place
// the choice is made.
func TestTheGarageOffersEachCarsLogsAndMarksTheDefault(t *testing.T) {
	page := samplePage("Garage")

	body := render(t, GaragePage, page)

	for _, card := range page.Cards {
		for _, want := range []string{
			"/vehicle/" + card.Vehicle.Slug + "/fuel#add",
			"/vehicle/" + card.Vehicle.Slug + "/service#add",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("the garage does not offer %s", want)
			}
		}
	}

	// The default card's button clears the choice, so it posts nothing; every
	// other card's posts its own slug.
	if !strings.Contains(body, `<input type="hidden" name="slug" value="">`) {
		t.Error("the default vehicle's card does not offer to clear the choice")
	}

	if !strings.Contains(body, `<input type="hidden" name="slug" value="the-truck">`) {
		t.Error("a card that is not the default does not offer to become it")
	}
}

// A card is only the default when the account has actually chosen one. An empty
// preference matching an empty slug would mark every card on the page.
func TestNoCardIsTheDefaultUntilOneIsChosen(t *testing.T) {
	page := samplePage("Garage")
	page.You.DefaultVehicle = ""
	page.Cards = []Card{{Vehicle: types.Vehicle{Name: "Nameless"}}}

	body := render(t, GaragePage, page)

	if strings.Contains(body, "star on") {
		t.Error("a card is marked as the default when the account has not chosen one")
	}
}

// The import screen is the one page where somebody is being asked to agree to
// something, so what it shows has to be what it would do.
func TestTheImportPreviewShowsProblemsAndOddities(t *testing.T) {
	page := samplePage("Import")
	page.Preview = importer.Preview{
		Fillups:  1,
		Services: 0,
		Bad:      1,
		Odd:      1,
		Rows: []importer.Row{
			{
				Line: 2, Kind: importer.KindFillup,
				Fillup: types.Fillup{On: "2024-01-01", Odometer: 90000, Gallons: 40,
					PricePerGallonCents: 320, Station: "Somewhere"},
				Odd: "40 gallons is more than this vehicle's tank holds",
			},
			{Line: 3, Raw: "not,a,row", Problem: "there is no date on this line"},
		},
	}

	body := render(t, ImportPage, page)

	for _, want := range []string{
		"more than this vehicle&#39;s tank holds",
		"there is no date on this line",
		"not,a,row",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the import preview does not mention %q", want)
		}
	}
}

// Everything a person types goes through html/template, which escapes it for
// the context it lands in. This is the check that nothing on the busiest page
// takes a shortcut around that.
func TestTypedTextIsEscaped(t *testing.T) {
	page := samplePage("Fuel log")
	page.Fillup.Station = `<script>alert(1)</script>`
	page.Fillup.Note = `" onmouseover="alert(1)`

	body := render(t, FillupsPage, page)

	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("a typed station name reached the page as markup")
	}

	if strings.Contains(body, `" onmouseover="alert(1)`) {
		t.Error("a typed note broke out of its attribute")
	}
}

// SafeNext is what stands between a "?next=" and an open redirect.
func TestSafeNext(t *testing.T) {
	safe := []string{"/", "/vehicle/the-wagon", "/vehicle/the-wagon/fuel?edit=1"}
	for _, next := range safe {
		if got := SafeNext(next); got != next {
			t.Errorf("SafeNext(%q) = %q, want it left alone", next, got)
		}
	}

	unsafe := []string{
		"", "https://example.com", "//example.com", "/\\example.com",
		"javascript:alert(1)", "/ok\r\nSet-Cookie: a=b", "vehicle/the-wagon",
	}
	for _, next := range unsafe {
		if got := SafeNext(next); got != "/" {
			t.Errorf("SafeNext(%q) = %q, want /", next, got)
		}
	}
}

// The formatters are what decide whether a column is readable, and three of
// them carry a decision rather than a default: a per-mile figure needs three
// places or it rounds to a dime, a blank is not the same as a zero, and a span
// of days stops being days once it is years.
func TestTheFormatters(t *testing.T) {
	if got := commas(95176); got != "95,176" {
		t.Errorf("commas(95176) = %q", got)
	}

	if got := commas(-1234567); got != "-1,234,567" {
		t.Errorf("commas(-1234567) = %q", got)
	}

	if got := commas(0); got != "0" {
		t.Errorf("commas(0) = %q", got)
	}

	cases := []struct {
		days int
		want string
	}{
		{0, "-"}, {1, "1 day"}, {14, "14 days"}, {99, "99 days"},
		{120, "4 months"}, {365, "1y"}, {1705, "4y 8m"},
	}

	for _, one := range cases {
		if got := humanDays(one.days); got != one.want {
			t.Errorf("humanDays(%d) = %q, want %q", one.days, got, one.want)
		}
	}

	if got := trimZeros("12.000"); got != "12" {
		t.Errorf("trimZeros(12.000) = %q", got)
	}

	if got := trimZeros("12.475"); got != "12.475" {
		t.Errorf("trimZeros(12.475) = %q", got)
	}
}

// The "check your email" page is where the code from the email is typed, and
// it comes back with an error on it when the code was wrong.
func TestTheSentPageTakesTheCodeFromTheEmail(t *testing.T) {
	page := New("Check your email")
	page.Email = "someone@example.com"
	page.Next = "/somewhere"
	page.ExpiresMinutes = 15
	page.Error = "That code didn't work."

	body := render(t, SentPage, page)

	// The address and the next path have to ride along in the form, or the code
	// arrives at the handler with nothing to check it against and nowhere to go.
	for _, want := range []string{
		`action="/login/code"`,
		`name="code"`,
		`autocomplete="one-time-code"`,
		`value="someone@example.com"`,
		`value="/somewhere"`,
		"That code didn",
		"15 minutes",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the sent page is missing %q", want)
		}
	}
}
