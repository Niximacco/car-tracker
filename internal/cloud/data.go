// Package data is everything that touches the database.
//
// It is a SQLite file of this service's own, on the host. The deploy makes it
// and its tables - users, vehicles, fillups and services, declared in
// pillar.json - before the container starts, and says where it is in
// DATABASE_PATH.
//
// The user half is carried over from wild-games and niximacco-recipes, because
// the allow list it implements is the same allow list. What is this site's own
// is the vehicle, and the two kinds of entry that hang off it.
//
// Fill-ups and shop visits carry their vehicle's slug, as a column that
// references the car and is deleted with it. Two things follow from that and
// both of them matter. Everything a car's pages ask for is one indexed read of
// one table, so pasting four years of history and then looking at it cannot
// show a car with half its fill-ups missing. And an entry's id only has to be
// unique inside its own car - the primary key is the pair - which is what lets
// the id be the day and the odometer reading rather than a random string, so
// importing the same rows twice updates them instead of doubling them.
package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Niximacco/car-tracker/internal/types"

	// SQLite in pure Go, so the container's CGO_ENABLED=0 build keeps working.
	_ "modernc.org/sqlite"
)

var db *sql.DB
var ctx = context.Background()

var AlreadyExistsErr = errors.New("entity already exists")

var (
	UserNotFoundErr = errors.New("user not found")
	UserDisabledErr = errors.New("user is disabled")

	VehicleNotFoundErr = errors.New("vehicle not found")
	FillupNotFoundErr  = errors.New("fill-up not found")
	ServiceNotFoundErr = errors.New("service record not found")
	NoVehiclesErr      = errors.New("no vehicles have been added yet")
)

// The list caps.
//
// A car's fuel and service history is ordered by the database, on the primary
// key's own table, so a cap takes the oldest entries rather than an arbitrary
// page of them. The vehicle and user lists are ordered here instead, because
// what they sort on - a car's retirement and the name it falls back to, a
// person's address - is worked out in types rather than stored.
const (
	USER_LIST_LIMIT    = 500
	VEHICLE_LIST_LIMIT = 200
	FILLUP_LIST_LIMIT  = 5000
	SERVICE_LIST_LIMIT = 1000
)

func Initialize() {
	path := os.Getenv("DATABASE_PATH")
	if path == "" {
		log.Fatal(`You need to set the environment variable "DATABASE_PATH"`)
	}

	if err := Open(path); err != nil {
		log.Fatalf("Could not open the database at %s: %v", path, err)
	}
}

// Open opens the database at path, and checks that its tables are there. A file
// without them is a deploy that did not make them, and is better found out by
// the container not coming up than by every page failing.
//
// busy_timeout has a writer wait for another rather than fail. foreign_keys is
// what makes a fill-up's reference to its vehicle do anything - it is per
// connection in SQLite, and deleting a car relies on the cascade. And
// _txlock=immediate has a transaction take the write lock when it begins, so
// two that read and then write - two drivers saving the same car - queue rather
// than deadlock.
func Open(path string) error {
	opened, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return err
	}

	for _, table := range []string{"users", "vehicles", "fillups", "services"} {
		if _, err := opened.ExecContext(ctx, `SELECT 1 FROM `+table+` LIMIT 1`); err != nil {
			opened.Close()
			return err
		}
	}

	if db != nil {
		db.Close()
	}
	db = opened

	return nil
}

// inTransaction runs change in a transaction, committed if it returns nil and
// rolled back if not.
func inTransaction(change func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := change(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// changed is whether a statement wrote a row, for telling a write to something
// that is not there from one that worked.
func changed(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}

	rows, err := result.RowsAffected()
	return rows > 0, err
}

// flag is a bool as a STRICT integer column wants it.
func flag(value bool) int64 {
	if value {
		return 1
	}

	return 0
}

// scanner is a row from a query or a single-row lookup, which scan the same way.
type scanner interface {
	Scan(into ...any) error
}

// rowQuerier is the database or a transaction on it, for a lookup that is the
// same either way.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// execer is the database or a transaction on it, for a write that is the same
// either way.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ---------------------------------------------------------------- users ----

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidAddress is a deliberately loose check. The real check is whether the
// address is in the users table; this only catches obvious junk before we
// bother the database, or auth.ajn.me, with it.
func ValidAddress(email string) bool {
	if len(email) < 3 || len(email) > 254 {
		return false
	}

	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return false
	}

	domain := email[at+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	return !strings.ContainsAny(email, " \t\r\n<>\"")
}

const userColumns = `email, name, created, last_login, admin, view_only, disabled, default_vehicle`

func scanUser(row scanner) (user types.User, err error) {
	err = row.Scan(&user.Email, &user.Name, &user.Created, &user.LastLogin,
		&user.Admin, &user.ViewOnly, &user.Disabled, &user.DefaultVehicle)
	return user, err
}

func GetUser(email string) (user types.User, err error) {
	user, err = scanUser(db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, NormalizeEmail(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return types.User{}, UserNotFoundErr
	}
	if err != nil {
		return types.User{}, err
	}

	if user.Disabled {
		return user, UserDisabledErr
	}

	return user, nil
}

// NewUser adds an address to the allow list. An account is either an admin, a
// view-only reader, or an ordinary member - admin and view only are opposites,
// so an account asked to be both is made an admin.
//
// An address already on the list is AlreadyExistsErr rather than an overwrite,
// so re-adding somebody can never quietly reset what they may do or lose when
// they last signed in.
func NewUser(email string, name string, admin bool, viewOnly bool) (user types.User, err error) {
	user = types.User{
		Email:    NormalizeEmail(email),
		Name:     strings.TrimSpace(name),
		Created:  time.Now().Unix(),
		Admin:    admin,
		ViewOnly: viewOnly && !admin,
	}

	added, err := changed(db.ExecContext(ctx,
		`INSERT INTO users (email, name, created, admin, view_only) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT (email) DO NOTHING`,
		user.Email, user.Name, user.Created, flag(user.Admin), flag(user.ViewOnly)))
	if err != nil {
		return types.User{}, err
	}
	if !added {
		return types.User{}, AlreadyExistsErr
	}

	return user, nil
}

func ListUsers(limit int) ([]types.User, error) {
	query, args := withLimit(`SELECT `+userColumns+` FROM users ORDER BY email`, limit, nil)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := []types.User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}

		users = append(users, user)
	}

	return users, rows.Err()
}

// UpdateUser changes what an account is allowed to do.
//
// Admin and view-only cannot both be true: one manages the site, the other may
// not change anything at all. Rather than refusing the combination, the flag
// being set wins and the other is cleared - "make them view only" is a clear
// instruction from an admin who can see both boxes, and answering it with an
// error about a state nobody asked for would be pedantry.
func UpdateUser(email string, name *string, admin *bool, viewOnly *bool,
	disabled *bool) (user types.User, err error) {
	email = NormalizeEmail(email)

	var sets []string
	var values []any

	set := func(column string, value any) {
		sets = append(sets, column+" = ?")
		values = append(values, value)
	}

	if name != nil {
		set("name", strings.TrimSpace(*name))
	}
	if admin != nil {
		set("admin", flag(*admin))
		if *admin {
			set("view_only", flag(false))
		}
	}
	if viewOnly != nil {
		set("view_only", flag(*viewOnly))
		if *viewOnly {
			set("admin", flag(false))
		}
	}
	if disabled != nil {
		set("disabled", flag(*disabled))
	}

	if len(sets) == 0 {
		return types.User{}, nil
	}

	if err = setUserColumns(email, sets, values); err != nil {
		return types.User{}, err
	}

	// Read it back so the caller gets what is actually stored. GetUser refuses
	// to return a disabled user, so build that case from what we just wrote.
	user, err = GetUser(email)
	if errors.Is(err, UserDisabledErr) {
		user.Email = email
		user.Disabled = true
		return user, nil
	}

	return user, err
}

// SetDefaultVehicle remembers which car an account opens on, or forgets it when
// the slug is empty.
//
// It is its own call rather than another pointer on UpdateUser because it is a
// different kind of change: UpdateUser is an admin deciding what an account may
// do, and this is an account deciding where it lands. Nothing here needs to
// check the slug - the handler has already made sure this account can see that
// car, and the page it points at checks again on every visit. It is deliberately
// not a reference to the vehicles table either, so retiring and deleting a car
// is never blocked by somebody having it starred.
func SetDefaultVehicle(email string, slug string) error {
	return setUserColumns(NormalizeEmail(email),
		[]string{"default_vehicle = ?"}, []any{strings.TrimSpace(slug)})
}

func MarkLoggedIn(email string, at time.Time) error {
	return setUserColumns(NormalizeEmail(email), []string{"last_login = ?"}, []any{at.Unix()})
}

// setUserColumns writes columns on one user, and says so when there is no such
// user rather than quietly writing nothing.
func setUserColumns(email string, sets []string, values []any) error {
	written, err := changed(db.ExecContext(ctx,
		`UPDATE users SET `+strings.Join(sets, ", ")+` WHERE email = ?`, append(values, email)...))
	if err != nil {
		return err
	}
	if !written {
		return UserNotFoundErr
	}

	return nil
}

// ------------------------------------------------------------- vehicles ----

const vehicleColumns = `slug, name, year, make, model, trim, plate, vin, owner, shared_with, ` +
	`purchased_on, purchase_odometer, purchase_price_cents, tank_gallons, ` +
	`retired, sold_on, sold_odometer, sold_price_cents, created, updated`

func scanVehicle(row scanner) (vehicle types.Vehicle, err error) {
	var sharedWith string

	if err = row.Scan(&vehicle.Slug, &vehicle.Name, &vehicle.Year, &vehicle.Make,
		&vehicle.Model, &vehicle.Trim, &vehicle.Plate, &vehicle.VIN,
		&vehicle.Owner, &sharedWith, &vehicle.PurchasedOn, &vehicle.PurchaseOdometer,
		&vehicle.PurchasePriceCents, &vehicle.TankGallons, &vehicle.Retired,
		&vehicle.SoldOn, &vehicle.SoldOdometer, &vehicle.SoldPriceCents,
		&vehicle.Created, &vehicle.Updated); err != nil {
		return types.Vehicle{}, err
	}

	vehicle.SharedWith = readAddresses(sharedWith)

	return vehicle, nil
}

func vehicleValues(vehicle types.Vehicle) []any {
	return []any{vehicle.Slug, vehicle.Name, int64(vehicle.Year), vehicle.Make,
		vehicle.Model, vehicle.Trim, vehicle.Plate, vehicle.VIN,
		vehicle.Owner, writeAddresses(vehicle.SharedWith), vehicle.PurchasedOn, int64(vehicle.PurchaseOdometer),
		vehicle.PurchasePriceCents, vehicle.TankGallons, flag(vehicle.Retired),
		vehicle.SoldOn, int64(vehicle.SoldOdometer), vehicle.SoldPriceCents,
		vehicle.Created, vehicle.Updated}
}

// readAddresses is a share list as it is stored, which is JSON in a text
// column. A column that is empty, or that holds something other than a list of
// addresses, reads as no shares: a car nobody can see but its owner is a
// recoverable sort of wrong, and refusing to load the car at all is not.
func readAddresses(stored string) []string {
	if strings.TrimSpace(stored) == "" {
		return []string{}
	}

	var addresses []string
	if err := json.Unmarshal([]byte(stored), &addresses); err != nil {
		log.Printf("could not read a vehicle's share list: %s", err.Error())
		return []string{}
	}

	if addresses == nil {
		return []string{}
	}

	return addresses
}

func writeAddresses(addresses []string) string {
	if addresses == nil {
		addresses = []string{}
	}

	stored, err := json.Marshal(addresses)
	if err != nil {
		// json.Marshal of a []string cannot fail. An empty list rather than a
		// panic is the right answer to the impossible case anyway.
		return "[]"
	}

	return string(stored)
}

func getVehicle(from rowQuerier, slug string) (types.Vehicle, error) {
	vehicle, err := scanVehicle(from.QueryRowContext(ctx, `SELECT `+vehicleColumns+` FROM vehicles WHERE slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return types.Vehicle{}, VehicleNotFoundErr
	}

	return vehicle, err
}

func GetVehicle(slug string) (types.Vehicle, error) {
	return getVehicle(db, strings.TrimSpace(slug))
}

// NewVehicle adds a car. The slug is the caller's to choose and is refused if
// it is taken, rather than being made unique behind their back: two people who
// both call theirs "the wagon" should be told, because the second one is about
// to be sharing the first one's history.
func NewVehicle(vehicle types.Vehicle) (types.Vehicle, error) {
	vehicle.Slug = strings.TrimSpace(vehicle.Slug)
	if vehicle.Slug == "" {
		return types.Vehicle{}, errors.New("a vehicle needs a name")
	}

	vehicle.Owner = NormalizeEmail(vehicle.Owner)
	vehicle.SharedWith = normalizeAddresses(vehicle.SharedWith, vehicle.Owner)
	vehicle.Created = time.Now().Unix()
	vehicle.Updated = vehicle.Created

	added, err := changed(db.ExecContext(ctx,
		`INSERT INTO vehicles (`+vehicleColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (slug) DO NOTHING`, vehicleValues(vehicle)...))
	if err != nil {
		return types.Vehicle{}, err
	}
	if !added {
		return types.Vehicle{}, AlreadyExistsErr
	}

	return vehicle, nil
}

// UpdateVehicle reads a vehicle, hands it to the caller to change, and writes
// it back inside a transaction.
//
// The read and the write are in one transaction because two drivers editing the
// same car is the ordinary case here, not a race worth ignoring. A change
// function that returns an error leaves the row exactly as it was.
func UpdateVehicle(slug string, change func(vehicle *types.Vehicle) error) (vehicle types.Vehicle, err error) {
	slug = strings.TrimSpace(slug)

	err = inTransaction(func(tx *sql.Tx) error {
		current, err := getVehicle(tx, slug)
		if err != nil {
			return err
		}

		if err := change(&current); err != nil {
			return err
		}

		current.Slug = slug
		current.Owner = NormalizeEmail(current.Owner)
		current.SharedWith = normalizeAddresses(current.SharedWith, current.Owner)
		current.Updated = time.Now().Unix()

		if _, err := tx.ExecContext(ctx,
			`UPDATE vehicles SET name = ?, year = ?, make = ?, model = ?, trim = ?,
			 plate = ?, vin = ?, owner = ?, shared_with = ?, purchased_on = ?,
			 purchase_odometer = ?, purchase_price_cents = ?, tank_gallons = ?,
			 retired = ?, sold_on = ?, sold_odometer = ?, sold_price_cents = ?,
			 created = ?, updated = ? WHERE slug = ?`,
			append(vehicleValues(current)[1:], current.Slug)...); err != nil {
			return err
		}

		vehicle = current
		return nil
	})

	if err != nil {
		return types.Vehicle{}, err
	}

	return vehicle, nil
}

// ListVehicles is every car on the site. It is the admin view and the input to
// VehiclesFor; ordinary pages go through that.
func ListVehicles() ([]types.Vehicle, error) {
	query, args := withLimit(`SELECT `+vehicleColumns+` FROM vehicles ORDER BY slug`, VEHICLE_LIST_LIMIT, nil)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	vehicles := []types.Vehicle{}
	for rows.Next() {
		vehicle, err := scanVehicle(rows)
		if err != nil {
			return nil, err
		}

		vehicles = append(vehicles, vehicle)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Cars still in the driveway first, then by name. A retired car keeps its
	// whole history and its own page; it just stops being the first thing on
	// the screen. The order is worked out here rather than asked of the
	// database, because the name a car is sorted under is the one Called()
	// falls back to when it has none of its own.
	sort.SliceStable(vehicles, func(a int, b int) bool {
		if vehicles[a].Retired != vehicles[b].Retired {
			return !vehicles[a].Retired
		}

		return strings.ToLower(vehicles[a].Called()) < strings.ToLower(vehicles[b].Called())
	})

	return vehicles, nil
}

// VehiclesFor is every car an account may see: the ones it owns and the ones it
// has been shared with. An admin sees all of them, because somebody has to be
// able to sort out a car whose owner has left.
//
// The filter is done here rather than in the query. The share list is JSON in a
// column, so asking the database to match an address in it would mean either a
// second table or a LIKE over the text - and at a garage's scale, a household
// has a handful of cars rather than a fleet, reading them and sorting them out
// here is one round trip.
func VehiclesFor(user types.User) (vehicles []types.Vehicle, err error) {
	all, err := ListVehicles()
	if err != nil {
		return nil, err
	}

	if user.Admin {
		return all, nil
	}

	vehicles = []types.Vehicle{}
	for _, vehicle := range all {
		if vehicle.CanBeSeenBy(user.Email) {
			vehicles = append(vehicles, vehicle)
		}
	}

	return vehicles, nil
}

// DeleteVehicle removes a car and everything logged against it.
//
// The entries go with it rather than being deleted first: both tables reference
// vehicles.slug on delete cascade, which the foreign_keys pragma in Open is
// what makes real. Entries that outlived their vehicle would be rows the site
// has no page for and no way to delete.
func DeleteVehicle(slug string) error {
	deleted, err := changed(db.ExecContext(ctx, `DELETE FROM vehicles WHERE slug = ?`, strings.TrimSpace(slug)))
	if err != nil {
		return err
	}
	if !deleted {
		return VehicleNotFoundErr
	}

	return nil
}

// normalizeAddresses lower-cases a share list, drops blanks and duplicates, and
// drops the owner - who is already on the vehicle and does not need to be on
// the list twice, where removing them from it would look like it took their
// access away.
func normalizeAddresses(addresses []string, owner string) []string {
	seen := map[string]bool{NormalizeEmail(owner): true}
	out := []string{}

	for _, address := range addresses {
		address = NormalizeEmail(address)
		if address == "" || seen[address] {
			continue
		}

		seen[address] = true
		out = append(out, address)
	}

	sort.Strings(out)

	return out
}

// vehicleExists is the check that stands in front of writing an entry. The
// reference on the column would refuse an entry whose car is not there anyway,
// but as a constraint failure rather than as the error the pages know how to
// show.
func vehicleExists(from rowQuerier, slug string) error {
	var one int
	err := from.QueryRowContext(ctx, `SELECT 1 FROM vehicles WHERE slug = ?`, slug).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return VehicleNotFoundErr
	}

	return err
}

// ------------------------------------------------------------- fill-ups ----

const fillupColumns = `vehicle, id, on_date, at, odometer, gallons, ` +
	`price_per_gallon_cents, fuel_saver_cents, cost_cents, station, note, ` +
	`partial, missed, logged_by, created, updated`

func scanFillup(row scanner) (fillup types.Fillup, err error) {
	err = row.Scan(&fillup.Vehicle, &fillup.ID, &fillup.On, &fillup.At,
		&fillup.Odometer, &fillup.Gallons, &fillup.PricePerGallonCents,
		&fillup.FuelSaverCents, &fillup.CostCents, &fillup.Station, &fillup.Note,
		&fillup.Partial, &fillup.Missed, &fillup.By, &fillup.Created, &fillup.Updated)
	return fillup, err
}

func fillupValues(fillup types.Fillup) []any {
	return []any{fillup.Vehicle, fillup.ID, fillup.On, fillup.At,
		int64(fillup.Odometer), fillup.Gallons, fillup.PricePerGallonCents,
		fillup.FuelSaverCents, fillup.CostCents, fillup.Station, fillup.Note,
		flag(fillup.Partial), flag(fillup.Missed), fillup.By, fillup.Created, fillup.Updated}
}

func getFillup(from rowQuerier, vehicle string, id string) (types.Fillup, error) {
	fillup, err := scanFillup(from.QueryRowContext(ctx,
		`SELECT `+fillupColumns+` FROM fillups WHERE vehicle = ? AND id = ?`, vehicle, id))
	if errors.Is(err, sql.ErrNoRows) {
		return types.Fillup{}, FillupNotFoundErr
	}

	return fillup, err
}

func GetFillup(vehicle string, id string) (types.Fillup, error) {
	return getFillup(db, vehicle, id)
}

// SaveFillup writes a fill-up, creating it or replacing it.
//
// The id is derived from the day and the reading, so saving a fill-up whose
// date or odometer has been corrected writes a new row rather than moving the
// old one. The caller passes the id it was editing so the old one can go in the
// same transaction; for a new entry that is empty.
func SaveFillup(fillup types.Fillup, replacing string, by string) (types.Fillup, error) {
	if fillup.Vehicle == "" {
		return types.Fillup{}, VehicleNotFoundErr
	}

	err := inTransaction(func(tx *sql.Tx) error {
		saved, err := saveFillup(tx, fillup, replacing, by)
		fillup = saved
		return err
	})

	if err != nil {
		return types.Fillup{}, err
	}

	return fillup, nil
}

// saveFillup is one fill-up written inside somebody else's transaction, which
// is what lets an import be a single one.
func saveFillup(tx *sql.Tx, fillup types.Fillup, replacing string, by string) (types.Fillup, error) {
	if err := vehicleExists(tx, fillup.Vehicle); err != nil {
		return types.Fillup{}, err
	}

	fillup.ID = types.FillupID(fillup.On, fillup.Odometer)
	fillup.Updated = time.Now().Unix()

	switch existing, err := getFillup(tx, fillup.Vehicle, fillup.ID); {
	case err == nil:
		// Keep who logged it first and when. An edit is an edit; it does not
		// make the fill-up somebody else's.
		fillup.Created = existing.Created
		fillup.By = existing.By

	case errors.Is(err, FillupNotFoundErr):
		fillup.Created = fillup.Updated
		fillup.By = NormalizeEmail(by)

	default:
		return types.Fillup{}, err
	}

	if fillup.By == "" {
		fillup.By = NormalizeEmail(by)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO fillups (`+fillupColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (vehicle, id) DO UPDATE SET
		   on_date = excluded.on_date, at = excluded.at, odometer = excluded.odometer,
		   gallons = excluded.gallons, price_per_gallon_cents = excluded.price_per_gallon_cents,
		   fuel_saver_cents = excluded.fuel_saver_cents, cost_cents = excluded.cost_cents,
		   station = excluded.station, note = excluded.note, partial = excluded.partial,
		   missed = excluded.missed, logged_by = excluded.logged_by,
		   created = excluded.created, updated = excluded.updated`,
		fillupValues(fillup)...); err != nil {
		return types.Fillup{}, err
	}

	if replacing != "" && replacing != fillup.ID {
		if _, err := tx.ExecContext(ctx, `DELETE FROM fillups WHERE vehicle = ? AND id = ?`,
			fillup.Vehicle, replacing); err != nil {
			return types.Fillup{}, err
		}
	}

	return fillup, nil
}

func DeleteFillup(vehicle string, id string) error {
	deleted, err := changed(db.ExecContext(ctx, `DELETE FROM fillups WHERE vehicle = ? AND id = ?`, vehicle, id))
	if err != nil {
		return err
	}
	if !deleted {
		return FillupNotFoundErr
	}

	return nil
}

// ListFillups is a car's whole fuel history, oldest first.
//
// The order is the database's, on the day and then the reading, which is the
// order everything downstream expects and the same one types.ByDate puts a
// loose slice in.
func ListFillups(vehicle string) ([]types.Fillup, error) {
	query, args := withLimit(`SELECT `+fillupColumns+` FROM fillups WHERE vehicle = ? ORDER BY on_date, odometer`,
		FILLUP_LIST_LIMIT, []any{vehicle})

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fillups := []types.Fillup{}
	for rows.Next() {
		fillup, err := scanFillup(rows)
		if err != nil {
			return nil, err
		}

		fillups = append(fillups, fillup)
	}

	return fillups, rows.Err()
}

// ImportFillups writes a batch, counting what was new and what was already
// there.
//
// It is safe to run twice on purpose. Every id is the day and the reading, so a
// re-paste of the same export updates the same rows - which is what makes
// importing four years of a spreadsheet something you can do wrong and simply
// do again. The whole batch is one transaction, so an import that fails partway
// leaves the history exactly as it found it.
func ImportFillups(fillups []types.Fillup, by string) (added int, updated int, err error) {
	err = inTransaction(func(tx *sql.Tx) error {
		added, updated = 0, 0

		for _, fillup := range fillups {
			id := types.FillupID(fillup.On, fillup.Odometer)

			_, getErr := getFillup(tx, fillup.Vehicle, id)
			switch {
			case getErr == nil:
				updated++

			case errors.Is(getErr, FillupNotFoundErr):
				added++

			default:
				return getErr
			}

			if _, err := saveFillup(tx, fillup, "", by); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return 0, 0, err
	}

	return added, updated, nil
}

// ------------------------------------------------------------- services ----

const serviceColumns = `vehicle, id, on_date, odometer, shop, work, cost_cents, ` +
	`note, logged_by, created, updated`

func scanService(row scanner) (service types.Service, err error) {
	err = row.Scan(&service.Vehicle, &service.ID, &service.On, &service.Odometer,
		&service.Shop, &service.Work, &service.CostCents, &service.Note,
		&service.By, &service.Created, &service.Updated)
	return service, err
}

func serviceValues(service types.Service) []any {
	return []any{service.Vehicle, service.ID, service.On, int64(service.Odometer),
		service.Shop, service.Work, service.CostCents, service.Note,
		service.By, service.Created, service.Updated}
}

func getService(from rowQuerier, vehicle string, id string) (types.Service, error) {
	service, err := scanService(from.QueryRowContext(ctx,
		`SELECT `+serviceColumns+` FROM services WHERE vehicle = ? AND id = ?`, vehicle, id))
	if errors.Is(err, sql.ErrNoRows) {
		return types.Service{}, ServiceNotFoundErr
	}

	return service, err
}

func GetService(vehicle string, id string) (types.Service, error) {
	return getService(db, vehicle, id)
}

// SaveService writes a shop visit, on the same terms a fill-up is written on.
func SaveService(service types.Service, replacing string, by string) (types.Service, error) {
	if service.Vehicle == "" {
		return types.Service{}, VehicleNotFoundErr
	}

	err := inTransaction(func(tx *sql.Tx) error {
		saved, err := saveService(tx, service, replacing, by)
		service = saved
		return err
	})

	if err != nil {
		return types.Service{}, err
	}

	return service, nil
}

func saveService(tx *sql.Tx, service types.Service, replacing string, by string) (types.Service, error) {
	if err := vehicleExists(tx, service.Vehicle); err != nil {
		return types.Service{}, err
	}

	service.ID = types.ServiceID(service.On, service.Odometer)
	service.Updated = time.Now().Unix()

	switch existing, err := getService(tx, service.Vehicle, service.ID); {
	case err == nil:
		service.Created = existing.Created
		service.By = existing.By

	case errors.Is(err, ServiceNotFoundErr):
		service.Created = service.Updated
		service.By = NormalizeEmail(by)

	default:
		return types.Service{}, err
	}

	if service.By == "" {
		service.By = NormalizeEmail(by)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO services (`+serviceColumns+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (vehicle, id) DO UPDATE SET
		   on_date = excluded.on_date, odometer = excluded.odometer, shop = excluded.shop,
		   work = excluded.work, cost_cents = excluded.cost_cents, note = excluded.note,
		   logged_by = excluded.logged_by, created = excluded.created, updated = excluded.updated`,
		serviceValues(service)...); err != nil {
		return types.Service{}, err
	}

	if replacing != "" && replacing != service.ID {
		if _, err := tx.ExecContext(ctx, `DELETE FROM services WHERE vehicle = ? AND id = ?`,
			service.Vehicle, replacing); err != nil {
			return types.Service{}, err
		}
	}

	return service, nil
}

func DeleteService(vehicle string, id string) error {
	deleted, err := changed(db.ExecContext(ctx, `DELETE FROM services WHERE vehicle = ? AND id = ?`, vehicle, id))
	if err != nil {
		return err
	}
	if !deleted {
		return ServiceNotFoundErr
	}

	return nil
}

// ListServices is a car's whole maintenance history, oldest first.
func ListServices(vehicle string) ([]types.Service, error) {
	query, args := withLimit(`SELECT `+serviceColumns+` FROM services WHERE vehicle = ? ORDER BY on_date, odometer`,
		SERVICE_LIST_LIMIT, []any{vehicle})

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	services := []types.Service{}
	for rows.Next() {
		service, err := scanService(rows)
		if err != nil {
			return nil, err
		}

		services = append(services, service)
	}

	return services, rows.Err()
}

// ImportServices writes a batch of shop visits, on the same re-runnable terms
// as ImportFillups.
func ImportServices(services []types.Service, by string) (added int, updated int, err error) {
	err = inTransaction(func(tx *sql.Tx) error {
		added, updated = 0, 0

		for _, service := range services {
			id := types.ServiceID(service.On, service.Odometer)

			_, getErr := getService(tx, service.Vehicle, id)
			switch {
			case getErr == nil:
				updated++

			case errors.Is(getErr, ServiceNotFoundErr):
				added++

			default:
				return getErr
			}

			if _, err := saveService(tx, service, "", by); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return 0, 0, err
	}

	return added, updated, nil
}

// ---------------------------------------------------------------- limits ----

// withLimit puts a cap on a query, as a bound value rather than in the text.
func withLimit(query string, limit int, args []any) (string, []any) {
	if limit > 0 {
		return query + " LIMIT ?", append(args, limit)
	}

	return query, args
}
