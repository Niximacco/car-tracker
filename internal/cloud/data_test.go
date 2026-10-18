package data

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Niximacco/car-tracker/internal/types"
)

// useTestDatabase points the package at a new file in a temporary directory,
// with the tables pillar.json declares made in it the way the deploy makes
// them. Nothing here needs an emulator or a project, so these tests run
// everywhere the rest of them do - which is the point of the move off
// datastore as much as anything the service does at runtime.
func useTestDatabase(t *testing.T) {
	t.Helper()

	pillar, err := os.ReadFile(filepath.Join("..", "..", "pillar.json"))
	if err != nil {
		t.Fatal(err)
	}
	statements, err := Tables(pillar)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "car-tracker.sqlite")
	made, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range statements {
		if _, err := made.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	made.Close()

	if err := Open(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		db = nil
	})
}

// aVehicle is a car with the two facts every lifetime figure is measured from,
// for a test that cares about something else.
func aVehicle(t *testing.T, slug string, owner string) types.Vehicle {
	t.Helper()

	vehicle, err := NewVehicle(types.Vehicle{
		Slug:             slug,
		Name:             slug,
		Owner:            owner,
		PurchasedOn:      "2021-12-28",
		PurchaseOdometer: 68074,
	})
	if err != nil {
		t.Fatal(err)
	}

	return vehicle
}

func aFillup(vehicle string, on string, odometer int, gallons float64) types.Fillup {
	return types.Fillup{
		Vehicle:             vehicle,
		On:                  on,
		Odometer:            odometer,
		Gallons:             gallons,
		PricePerGallonCents: 299,
		Station:             "Hy-Vee New Hope",
	}
}

// ValidAddress stands in front of both the users table and the call to
// auth.ajn.me, and is worth pinning: the header injection case at the bottom of
// the invalid list is the reason it refuses control characters.
func TestValidAddress(t *testing.T) {
	valid := []string{"a@b.co", "anthony@nixon.dev", "first.last+tag@sub.example.com"}
	for _, address := range valid {
		if !ValidAddress(address) {
			t.Errorf("ValidAddress(%q) = false, want true", address)
		}
	}

	invalid := []string{
		"",
		"nobody",
		"@example.com",
		"nobody@",
		"nobody@localhost",
		"nobody@.com",
		"nobody@example.",
		"no body@example.com",
		"a@b.co\r\nbcc: someone@else.com",
		strings.Repeat("a", 250) + "@example.com",
	}
	for _, address := range invalid {
		if ValidAddress(address) {
			t.Errorf("ValidAddress(%q) = true, want false", address)
		}
	}
}

// A missing database is the container not coming up, rather than every page
// failing one request at a time.
func TestOpenWithoutTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sqlite")
	if err := Open(path); err == nil {
		t.Fatal("Open on a file with no tables in it = nil, want an error")
	}
}

func TestUsers(t *testing.T) {
	useTestDatabase(t)

	if _, err := GetUser("nobody@example.com"); !errors.Is(err, UserNotFoundErr) {
		t.Fatalf("GetUser on an unknown address = %v, want UserNotFoundErr", err)
	}

	user, err := NewUser(" Anthony@Example.com ", " Anthony ", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "anthony@example.com" || user.Name != "Anthony" {
		t.Errorf("NewUser stored %q / %q, want the address normalized and the name trimmed", user.Email, user.Name)
	}
	// Admin and view-only are opposites, and admin wins.
	if !user.Admin || user.ViewOnly {
		t.Errorf("NewUser(admin, viewOnly) = admin %v / view only %v, want an admin", user.Admin, user.ViewOnly)
	}

	if _, err := NewUser("anthony@example.com", "Somebody Else", false, false); !errors.Is(err, AlreadyExistsErr) {
		t.Fatalf("NewUser on an address already on the list = %v, want AlreadyExistsErr", err)
	}

	stored, err := GetUser("ANTHONY@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Admin || stored.Name != "Anthony" {
		t.Errorf("GetUser = %+v, want the admin that was added", stored)
	}

	// Making somebody view only takes admin away, rather than refusing a
	// combination nobody asked for.
	viewOnly := true
	updated, err := UpdateUser("anthony@example.com", nil, nil, &viewOnly, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Admin || !updated.ViewOnly {
		t.Errorf("UpdateUser(viewOnly) = admin %v / view only %v, want view only alone", updated.Admin, updated.ViewOnly)
	}

	if err := SetDefaultVehicle("anthony@example.com", " the-wagon "); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1700000000, 0)
	if err := MarkLoggedIn("anthony@example.com", at); err != nil {
		t.Fatal(err)
	}

	stored, err = GetUser("anthony@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if stored.DefaultVehicle != "the-wagon" {
		t.Errorf("DefaultVehicle = %q, want %q", stored.DefaultVehicle, "the-wagon")
	}
	if stored.LastLogin != at.Unix() {
		t.Errorf("LastLogin = %d, want %d", stored.LastLogin, at.Unix())
	}

	// A disabled account is kept for history and refused at the door, and the
	// caller still gets enough of it to say so.
	disabled := true
	if _, err := UpdateUser("anthony@example.com", nil, nil, nil, &disabled); err != nil {
		t.Fatal(err)
	}
	blocked, err := GetUser("anthony@example.com")
	if !errors.Is(err, UserDisabledErr) {
		t.Fatalf("GetUser on a disabled account = %v, want UserDisabledErr", err)
	}
	if blocked.Email != "anthony@example.com" || !blocked.Disabled {
		t.Errorf("GetUser on a disabled account = %+v, want the address and the flag", blocked)
	}

	// Writing to an address that is not on the list says so rather than
	// quietly writing nothing.
	if err := MarkLoggedIn("nobody@example.com", at); !errors.Is(err, UserNotFoundErr) {
		t.Fatalf("MarkLoggedIn on an unknown address = %v, want UserNotFoundErr", err)
	}

	if _, err := NewUser("beth@example.com", "Beth", false, false); err != nil {
		t.Fatal(err)
	}
	users, err := ListUsers(USER_LIST_LIMIT)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Email != "anthony@example.com" || users[1].Email != "beth@example.com" {
		t.Errorf("ListUsers = %+v, want both accounts by address", users)
	}
}

func TestVehicles(t *testing.T) {
	useTestDatabase(t)

	if _, err := GetVehicle("nothing"); !errors.Is(err, VehicleNotFoundErr) {
		t.Fatalf("GetVehicle on an unknown slug = %v, want VehicleNotFoundErr", err)
	}

	if _, err := NewVehicle(types.Vehicle{Slug: "  "}); err == nil {
		t.Fatal("NewVehicle with no slug = nil, want an error")
	}

	// The share list is JSON in a text column, and has to survive the round
	// trip: the owner dropped, the rest lower-cased, sorted and deduplicated.
	wagon, err := NewVehicle(types.Vehicle{
		Slug:       "the-wagon",
		Name:       "Golf SportWagen",
		Year:       2016,
		Make:       "Volkswagen",
		Owner:      "Anthony@example.com",
		SharedWith: []string{"Beth@Example.com", "beth@example.com", "", "anthony@example.com", "cal@example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(wagon.SharedWith, ","); got != "beth@example.com,cal@example.com" {
		t.Errorf("NewVehicle share list = %q, want beth and cal alone", got)
	}

	stored, err := GetVehicle("the-wagon")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stored.SharedWith, ","); got != "beth@example.com,cal@example.com" {
		t.Errorf("GetVehicle share list = %q, want what was written", got)
	}
	if stored.Year != 2016 || stored.Make != "Volkswagen" {
		t.Errorf("GetVehicle = %+v, want the car that was added", stored)
	}

	if _, err := NewVehicle(types.Vehicle{Slug: "the-wagon", Owner: "beth@example.com"}); !errors.Is(err, AlreadyExistsErr) {
		t.Fatalf("NewVehicle on a slug that is taken = %v, want AlreadyExistsErr", err)
	}

	// A car with no share list at all reads as nobody rather than as nothing,
	// because the column holds its default.
	plain := aVehicle(t, "the-truck", "beth@example.com")
	if plain.SharedWith == nil || len(plain.SharedWith) != 0 {
		t.Errorf("a car shared with nobody = %#v, want an empty list", plain.SharedWith)
	}

	updated, err := UpdateVehicle("the-wagon", func(vehicle *types.Vehicle) error {
		vehicle.Retired = true
		vehicle.SoldOn = "2026-01-15"
		vehicle.SoldPriceCents = 750000
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Retired || updated.SoldPriceCents != 750000 {
		t.Errorf("UpdateVehicle = %+v, want it retired and sold", updated)
	}
	if updated.Updated < updated.Created {
		t.Errorf("UpdateVehicle left Updated at %d, behind Created at %d", updated.Updated, updated.Created)
	}

	// A change function that fails leaves the row exactly as it was.
	refused := errors.New("no")
	if _, err := UpdateVehicle("the-wagon", func(vehicle *types.Vehicle) error {
		vehicle.Name = "something else"
		return refused
	}); !errors.Is(err, refused) {
		t.Fatalf("UpdateVehicle with a change that fails = %v, want the change's own error", err)
	}
	stored, err = GetVehicle("the-wagon")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Golf SportWagen" {
		t.Errorf("a refused change left the name as %q", stored.Name)
	}

	if _, err := UpdateVehicle("nothing", func(*types.Vehicle) error { return nil }); !errors.Is(err, VehicleNotFoundErr) {
		t.Fatalf("UpdateVehicle on an unknown slug = %v, want VehicleNotFoundErr", err)
	}

	// Cars still in the driveway first, then by name, whatever order they went
	// in in.
	vehicles, err := ListVehicles()
	if err != nil {
		t.Fatal(err)
	}
	if len(vehicles) != 2 || vehicles[0].Slug != "the-truck" || vehicles[1].Slug != "the-wagon" {
		t.Errorf("ListVehicles = %+v, want the retired wagon last", vehicles)
	}
}

func TestVehiclesFor(t *testing.T) {
	useTestDatabase(t)

	aVehicle(t, "anthonys", "anthony@example.com")
	aVehicle(t, "beths", "beth@example.com")
	if _, err := UpdateVehicle("beths", func(vehicle *types.Vehicle) error {
		vehicle.SharedWith = []string{"anthony@example.com"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		user types.User
		want string
	}{
		{types.User{Email: "anthony@example.com"}, "anthonys,beths"},
		{types.User{Email: "beth@example.com"}, "beths"},
		{types.User{Email: "cal@example.com"}, ""},
		{types.User{Email: "cal@example.com", Admin: true}, "anthonys,beths"},
	} {
		vehicles, err := VehiclesFor(test.user)
		if err != nil {
			t.Fatal(err)
		}

		slugs := []string{}
		for _, vehicle := range vehicles {
			slugs = append(slugs, vehicle.Slug)
		}

		if got := strings.Join(slugs, ","); got != test.want {
			t.Errorf("VehiclesFor(%s, admin %v) = %q, want %q", test.user.Email, test.user.Admin, got, test.want)
		}
	}
}

func TestFillups(t *testing.T) {
	useTestDatabase(t)
	aVehicle(t, "the-wagon", "anthony@example.com")

	// An entry needs a car that exists. The reference on the column would
	// refuse it anyway; this is the error the pages know how to show.
	if _, err := SaveFillup(aFillup("nothing", "2026-08-11", 95176, 11.2), "", "anthony@example.com"); !errors.Is(err, VehicleNotFoundErr) {
		t.Fatalf("SaveFillup against an unknown car = %v, want VehicleNotFoundErr", err)
	}
	if _, err := SaveFillup(aFillup("", "2026-08-11", 95176, 11.2), "", "anthony@example.com"); !errors.Is(err, VehicleNotFoundErr) {
		t.Fatalf("SaveFillup with no car = %v, want VehicleNotFoundErr", err)
	}

	first, err := SaveFillup(aFillup("the-wagon", "2026-08-11", 95176, 11.2), "", "Anthony@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "2026-08-11-95176" {
		t.Errorf("SaveFillup id = %q, want the day and the reading", first.ID)
	}
	if first.By != "anthony@example.com" {
		t.Errorf("SaveFillup logged by %q, want the address normalized", first.By)
	}

	// An edit is an edit: it keeps who logged it first and when.
	edited := first
	edited.Gallons = 11.9
	edited.Note = "gas spilled during fill-up"
	again, err := SaveFillup(edited, first.ID, "beth@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if again.By != "anthony@example.com" || again.Created != first.Created {
		t.Errorf("an edit re-attributed the fill-up to %q at %d", again.By, again.Created)
	}

	// Correcting the reading writes a new row and takes the old one with it,
	// because the id is the day and the reading.
	moved := again
	moved.Odometer = 95178
	moved, err = SaveFillup(moved, again.ID, "anthony@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID != "2026-08-11-95178" {
		t.Errorf("a corrected reading kept the id %q", moved.ID)
	}
	if _, err := GetFillup("the-wagon", again.ID); !errors.Is(err, FillupNotFoundErr) {
		t.Fatalf("the fill-up being replaced is still there: %v", err)
	}

	// Oldest first, on the day and then the reading, whatever order they
	// arrived in.
	for _, fillup := range []types.Fillup{
		aFillup("the-wagon", "2026-07-02", 94800, 10.1),
		aFillup("the-wagon", "2026-08-11", 95177, 1.0),
	} {
		if _, err := SaveFillup(fillup, "", "anthony@example.com"); err != nil {
			t.Fatal(err)
		}
	}

	fillups, err := ListFillups("the-wagon")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, fillup := range fillups {
		ids = append(ids, fillup.ID)
	}
	if got := strings.Join(ids, ","); got != "2026-07-02-94800,2026-08-11-95177,2026-08-11-95178" {
		t.Errorf("ListFillups = %q, want them oldest first", got)
	}

	if err := DeleteFillup("the-wagon", "2026-07-02-94800"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteFillup("the-wagon", "2026-07-02-94800"); !errors.Is(err, FillupNotFoundErr) {
		t.Fatalf("DeleteFillup on something already gone = %v, want FillupNotFoundErr", err)
	}

	if fillups, err = ListFillups("the-wagon"); err != nil || len(fillups) != 2 {
		t.Errorf("ListFillups after a delete = %d entries (%v), want 2", len(fillups), err)
	}

	// A car with nothing logged against it is an empty history rather than an
	// error, and never nil.
	aVehicle(t, "the-truck", "anthony@example.com")
	empty, err := ListFillups("the-truck")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("ListFillups on a new car = %#v (%v), want an empty list", empty, err)
	}
}

// An import is something you can get wrong and simply do again: the same rows
// twice update the same entries rather than doubling them, and the counts say
// which was which.
func TestImportIsSafeToRunTwice(t *testing.T) {
	useTestDatabase(t)
	aVehicle(t, "the-wagon", "anthony@example.com")

	fillups := []types.Fillup{
		aFillup("the-wagon", "2026-07-02", 94800, 10.1),
		aFillup("the-wagon", "2026-08-11", 95176, 11.2),
	}
	services := []types.Service{
		{Vehicle: "the-wagon", On: "2026-08-07", Odometer: 95077, Shop: "West Side VW", Work: "oil", CostCents: 8995},
	}

	added, updated, err := ImportFillups(fillups, "anthony@example.com")
	if err != nil || added != 2 || updated != 0 {
		t.Fatalf("the first import = %d added, %d updated, %v; want 2 and 0", added, updated, err)
	}

	added, updated, err = ImportFillups(fillups, "beth@example.com")
	if err != nil || added != 0 || updated != 2 {
		t.Fatalf("the second import = %d added, %d updated, %v; want 0 and 2", added, updated, err)
	}

	if stored, err := ListFillups("the-wagon"); err != nil || len(stored) != 2 {
		t.Errorf("two imports of the same rows left %d entries (%v), want 2", len(stored), err)
	}

	added, updated, err = ImportServices(services, "anthony@example.com")
	if err != nil || added != 1 || updated != 0 {
		t.Fatalf("the first service import = %d added, %d updated, %v; want 1 and 0", added, updated, err)
	}

	// A batch with a car in it that does not exist writes none of itself, so an
	// import that fails partway leaves the history as it found it.
	broken := append([]types.Fillup{aFillup("the-wagon", "2026-09-01", 95400, 9.8)}, aFillup("nothing", "2026-09-02", 95500, 9.9))
	if _, _, err := ImportFillups(broken, "anthony@example.com"); !errors.Is(err, VehicleNotFoundErr) {
		t.Fatalf("an import naming an unknown car = %v, want VehicleNotFoundErr", err)
	}
	if stored, err := ListFillups("the-wagon"); err != nil || len(stored) != 2 {
		t.Errorf("a failed import left %d entries (%v), want the 2 that were there", len(stored), err)
	}
}

func TestServices(t *testing.T) {
	useTestDatabase(t)
	aVehicle(t, "the-wagon", "anthony@example.com")

	visit, err := SaveService(types.Service{
		Vehicle:   "the-wagon",
		On:        "2026-08-07",
		Odometer:  95077,
		Shop:      "West Side VW",
		Work:      "oil and filter\nrear wiper",
		CostCents: 8995,
	}, "", "Anthony@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if visit.ID != "2026-08-07-95077" || visit.By != "anthony@example.com" {
		t.Errorf("SaveService = %q by %q, want the day and the reading, and the address normalized", visit.ID, visit.By)
	}

	stored, err := GetService("the-wagon", visit.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Items()) != 2 || stored.CostCents != 8995 {
		t.Errorf("GetService = %+v, want the two lines of work and the cost", stored)
	}

	if _, err := GetService("the-wagon", "2020-01-01-1"); !errors.Is(err, ServiceNotFoundErr) {
		t.Fatalf("GetService on an unknown id = %v, want ServiceNotFoundErr", err)
	}

	if err := DeleteService("the-wagon", visit.ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteService("the-wagon", visit.ID); !errors.Is(err, ServiceNotFoundErr) {
		t.Fatalf("DeleteService on something already gone = %v, want ServiceNotFoundErr", err)
	}
}

// Deleting a car takes its history with it, through the reference on the
// column rather than a loop here. Entries that outlived their vehicle would be
// rows the site has no page for and no way to delete.
func TestDeleteVehicleTakesItsHistory(t *testing.T) {
	useTestDatabase(t)
	aVehicle(t, "the-wagon", "anthony@example.com")
	aVehicle(t, "the-truck", "anthony@example.com")

	for _, slug := range []string{"the-wagon", "the-truck"} {
		if _, err := SaveFillup(aFillup(slug, "2026-08-11", 95176, 11.2), "", "anthony@example.com"); err != nil {
			t.Fatal(err)
		}
		if _, err := SaveService(types.Service{Vehicle: slug, On: "2026-08-07", Odometer: 95077, Shop: "West Side VW"}, "", "anthony@example.com"); err != nil {
			t.Fatal(err)
		}
	}

	if err := DeleteVehicle("the-wagon"); err != nil {
		t.Fatal(err)
	}

	if fillups, err := ListFillups("the-wagon"); err != nil || len(fillups) != 0 {
		t.Errorf("the deleted car still has %d fill-ups (%v)", len(fillups), err)
	}
	if services, err := ListServices("the-wagon"); err != nil || len(services) != 0 {
		t.Errorf("the deleted car still has %d shop visits (%v)", len(services), err)
	}

	// And only its own.
	if fillups, err := ListFillups("the-truck"); err != nil || len(fillups) != 1 {
		t.Errorf("the other car has %d fill-ups (%v), want 1", len(fillups), err)
	}
	if services, err := ListServices("the-truck"); err != nil || len(services) != 1 {
		t.Errorf("the other car has %d shop visits (%v), want 1", len(services), err)
	}

	if err := DeleteVehicle("the-wagon"); !errors.Is(err, VehicleNotFoundErr) {
		t.Fatalf("DeleteVehicle on a car already gone = %v, want VehicleNotFoundErr", err)
	}
}
