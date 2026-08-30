// Package types is the shape of everything this site stores and everything it
// works out from what it stores.
//
// It replaces a Google Form feeding two sheets: one row a fill-up, one row a
// visit to the shop, and a block of formulas underneath adding them up. The
// three shapes here are those two sheets and that block, with the arithmetic
// moved out of a formula and into code that can be tested - which is the whole
// reason for the move. A formula is right until somebody sorts a column.
package types

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Local is the clock every date on this site is read in. Odometer readings and
// fill-up dates are wall-clock facts about a place - "the Tuesday I filled up"
// - and a server running in UTC would put half the evening fill-ups on the
// following day.
var Local *time.Location

// LocalName is the tz database name Local was loaded from, so the pages can say
// which clock they are using.
var LocalName = "America/Chicago"

func init() {
	loaded, err := time.LoadLocation(LocalName)
	if err != nil {
		// The tz database is compiled into the binary through the time/tzdata
		// import in main, so this is unreachable in a built service. UTC at
		// least keeps dates monotonic if it ever is reached.
		loaded, LocalName = time.UTC, "UTC"
	}

	Local = loaded
}

// DateLayout is how every date on this site is stored: sortable, unambiguous,
// and the same string a browser's date input hands back.
const DateLayout = "2006-01-02"

// ---------------------------------------------------------------- users ----

// User is an account that may sign in. Having an entity in the user kind is
// what grants access; there is nothing else to check.
type User struct {
	Email string `json:"email"`
	// Name is what the pages call this person - on a vehicle they share, and
	// beside an entry they logged. Without one the pages fall back to the part
	// of the address before the @.
	Name      string `json:"name"`
	Created   int64  `json:"created"`
	LastLogin int64  `json:"last_login"`
	// Admin lets this user manage who else is allowed to sign in, and reach
	// every vehicle on the site rather than only their own and the ones shared
	// with them.
	Admin bool `json:"admin"`
	// ViewOnly is an account that can read the garage and change nothing - not
	// a vehicle, not an entry, not its own name.
	ViewOnly bool `json:"view_only"`
	// Disabled keeps the entity around for history while blocking logins. An
	// account that logged two years of fill-ups still has its name on them.
	Disabled bool `json:"disabled"`
	// DefaultVehicle is the slug of the car this account opens on, or empty for
	// an account that lands in the garage.
	//
	// It is a preference rather than a permission: it is only ever read to
	// decide where "/" sends somebody, and the car's own page checks who is
	// asking exactly as it would for any other visit. A slug that has since
	// been deleted, or unshared, is nothing worse than a landing that falls
	// back to the garage.
	DefaultVehicle string `json:"default_vehicle"`
}

// CanEdit reports whether this account may change anything at all.
func (u User) CanEdit() bool {
	return !u.ViewOnly && !u.Disabled
}

// Called is what to put on a page for this person: their name if they set one,
// otherwise the part of their address before the @. An empty user is "somebody",
// which is what an entry imported before its logger had an account reads as.
func (u User) Called() string {
	if name := strings.TrimSpace(u.Name); name != "" {
		return name
	}

	return AccountName(u.Email)
}

// AccountName is the local part of an address, for the places a page needs to
// name somebody it has not loaded.
func AccountName(email string) string {
	local, _, found := strings.Cut(email, "@")
	if !found || local == "" {
		if email == "" {
			return "somebody"
		}

		return email
	}

	return local
}

// ------------------------------------------------------------- vehicles ----

// Vehicle is one car, and the two facts every lifetime figure is measured from:
// the day it was bought and what the odometer read that day.
//
// Those two are not optional in practice. Without them "total miles" can only
// be measured between logged fill-ups, and "cost per day" cannot be worked out
// at all - which is most of what the sheet this replaces was for. The site
// still runs without them and says which figures it cannot show.
type Vehicle struct {
	// Slug is the key this vehicle lives at and the url it is read at,
	// "golf-sportwagen". It is made from the name once and never changes,
	// because links to it are worth keeping.
	Slug string `json:"slug"`
	// Name is what the pages call it - "Golf SportWagen", "Mike's truck".
	Name string `json:"name"`
	// Year, Make and Model are the car itself, shown under the name. They are
	// separate fields rather than part of the name so that a garage of several
	// can be read down a column.
	Year  int    `json:"year"`
	Make  string `json:"make"`
	Model string `json:"model"`
	Trim  string `json:"trim"`
	// Plate and VIN are here because the one time you need them you are
	// standing somewhere else. Neither is shown on any page but the vehicle's
	// own, and neither leaves the site.
	Plate string `json:"plate"`
	VIN   string `json:"vin"`

	// Owner is the account that created this vehicle. It may edit and delete
	// it, and it is the only account other than an admin that may change who
	// else can see it.
	Owner string `json:"owner"`
	// SharedWith is everybody else who may read this vehicle and log against
	// it. A car with two drivers is the ordinary case - the fill-ups have to
	// land in one place or none of the arithmetic means anything - and this is
	// how it is done without making every car on the site everybody's business.
	//
	// Addresses are stored normalized and are not required to have accounts
	// yet: sharing with somebody before they have signed in once should work.
	SharedWith []string `json:"shared_with"`

	// PurchasedOn is the day it became yours, stored as "2006-01-02".
	PurchasedOn string `json:"purchased_on"`
	// PurchaseOdometer is what it read that day. Total miles is measured from
	// here rather than from the first logged fill-up, which is why the sheet's
	// lifetime mileage is larger than the distance between its first and last
	// rows.
	PurchaseOdometer int `json:"purchase_odometer"`
	// PurchasePriceCents is what it cost, if you want the true cost of running
	// it rather than the cost of feeding it. Left at zero it is simply left out
	// of every total.
	PurchasePriceCents int64 `json:"purchase_price_cents"`

	// TankGallons is the usable tank size, if known. It is only used to notice
	// a fill-up that could not physically have happened - a typed 112 gallons
	// where 11.2 was meant - so leaving it unset only loses that warning.
	TankGallons float64 `json:"tank_gallons"`

	// Retired is a car you no longer have. Its history stays exactly as it is
	// and every lifetime figure freezes: days owned stops counting on SoldOn
	// rather than running on forever, which is the one thing that would quietly
	// wreck every per-day number on the page.
	Retired      bool   `json:"retired"`
	SoldOn       string `json:"sold_on"`
	SoldOdometer int    `json:"sold_odometer"`
	// SoldPriceCents is what it fetched. It is subtracted from the cost of
	// ownership rather than added to it, so a sold car's true cost is what it
	// actually took out of your pocket.
	SoldPriceCents int64 `json:"sold_price_cents"`

	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

// Described is the line under a vehicle's name: "2016 Volkswagen Golf
// SportWagen S". Any part that is not set is simply left out, so a vehicle
// entered as a name and nothing else reads as a name and nothing else.
func (v Vehicle) Described() string {
	parts := []string{}

	if v.Year > 0 {
		parts = append(parts, strconv.Itoa(v.Year))
	}

	for _, part := range []string{v.Make, v.Model, v.Trim} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}

	return strings.Join(parts, " ")
}

// Called is the vehicle's name, falling back to its description and then to its
// slug. A vehicle always has something to be called.
func (v Vehicle) Called() string {
	if name := strings.TrimSpace(v.Name); name != "" {
		return name
	}

	if described := v.Described(); described != "" {
		return described
	}

	return v.Slug
}

// PurchaseDate is the day it was bought, or the zero time when that has not
// been recorded.
func (v Vehicle) PurchaseDate() time.Time {
	return ParseDate(v.PurchasedOn)
}

// Owned reports whether we know enough to measure a lifetime: the day it was
// bought, which is what every per-day figure is divided by.
func (v Vehicle) Owned() bool {
	return !v.PurchaseDate().IsZero()
}

// EndOfOwnership is the day the clock stops: the day it was sold for a retired
// car, and today for one still in the driveway.
//
// A retired vehicle with no sale date recorded stops at its last entry, which
// the caller passes in. Letting it run to today would keep raising the days and
// lowering the cost per day of a car nobody has driven for two years.
func (v Vehicle) EndOfOwnership(now time.Time, lastEntry time.Time) time.Time {
	if !v.Retired {
		return now
	}

	if sold := ParseDate(v.SoldOn); !sold.IsZero() {
		return sold
	}

	if !lastEntry.IsZero() {
		return lastEntry
	}

	return now
}

// CanBeSeenBy reports whether an account may read this vehicle. Sharing is
// per-vehicle on purpose: a car with two drivers has to have one set of
// fill-ups, and that is a different thing from everybody on the site seeing
// everybody's cars.
func (v Vehicle) CanBeSeenBy(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}

	if strings.EqualFold(v.Owner, email) {
		return true
	}

	for _, shared := range v.SharedWith {
		if strings.EqualFold(shared, email) {
			return true
		}
	}

	return false
}

// CanBeManagedBy reports whether an account may change the vehicle itself - its
// details, who it is shared with, and whether it still exists.
//
// Everybody it is shared with may log against it. Only the owner may change
// what it is, because the purchase date and odometer are the origin every
// lifetime figure on the page is measured from, and somebody correcting their
// own typo should not be able to move them.
func (v Vehicle) CanBeManagedBy(user User) bool {
	if !user.CanEdit() {
		return false
	}

	return user.Admin || strings.EqualFold(v.Owner, user.Email)
}

// -------------------------------------------------------------- fill-ups ----

// Fillup is one visit to a pump: what the odometer read, how much went in, and
// what it cost a gallon.
//
// What is stored is only what was actually observed at the pump. Miles covered,
// miles per gallon, what the tank cost and what the discount saved are all
// worked out in the stats package from this and the fill-up before it, because
// every one of them depends on a neighbour - and a stored number that depends
// on a neighbour is a number that goes wrong the moment a forgotten fill-up is
// added in the middle.
type Fillup struct {
	// ID is "2026-08-11-95176": the date and the odometer reading.
	//
	// It is derived rather than random so that importing the same rows twice
	// updates them instead of doubling them, which is the difference between an
	// import you can re-run and one you get a single attempt at. Two fill-ups
	// on one day at one reading are the same fill-up.
	ID string `json:"id"`
	// Vehicle is the slug of the car this belongs to. Fill-ups are datastore
	// children of their vehicle, so this is a copy for the sake of code holding
	// one on its own.
	Vehicle string `json:"vehicle"`

	// On is the day, "2006-01-02".
	On string `json:"on"`
	// At is the time of day, "15:04", or empty. The form's timestamp carried
	// one and it is worth keeping - it is how you tell a morning commute fill
	// from a Friday-night-before-a-drive fill - but nothing is calculated from
	// it.
	At string `json:"at"`

	// Odometer is the reading at the pump, in whole miles.
	Odometer int `json:"odometer"`
	// Gallons is what went in.
	Gallons float64 `json:"gallons"`
	// PricePerGallonCents is what was actually paid a gallon, after any
	// discount. It is the after price rather than the pump price because that
	// is the number on the receipt and the number every cost on this site is
	// built from; the pump price is worked out by adding the discount back.
	PricePerGallonCents int64 `json:"price_per_gallon_cents"`
	// FuelSaverCents is the discount a gallon, from a grocery-store fuel
	// programme or anything else that takes money off at the pump. It is its
	// own field rather than a note because what it adds up to over a year is
	// one of the more satisfying numbers on the site.
	FuelSaverCents int64 `json:"fuel_saver_cents"`
	// CostCents overrides the gallons-times-price arithmetic for the one
	// receipt where it does not come out even. Left at zero the cost is worked
	// out, which is what happens for almost every fill.
	CostCents int64 `json:"cost_cents"`

	// Station is where, "Hy-Vee New Hope". It is grouped on for the station
	// breakdown, so it is folded to a canonical form before being stored.
	Station string `json:"station"`
	// Note is anything else about this fill - "gas spilled during fill-up,
	// maybe half a gallon" is a real one, and it is exactly the sort of thing
	// that explains an odd mpg two years later.
	Note string `json:"note"`

	// Partial marks a fill that did not fill the tank. Miles per gallon between
	// two fills only means anything if both of them ran to full, so a partial
	// contributes its gallons and its cost to every total and contributes no
	// mpg at all - and neither does the fill after it.
	Partial bool `json:"partial"`
	// Missed marks a fill-up with a known gap before it: a tank that was bought
	// and never logged. The miles are real, the gallons are real, and the mpg
	// between them is not, so it is left out the same way a partial is.
	Missed bool `json:"missed"`

	// By is the address of whoever logged it, for a car with two drivers.
	By      string `json:"by"`
	Created int64  `json:"created"`
	Updated int64  `json:"updated"`
}

// FillupID is the id a fill-up is stored under. Deriving it from the day and
// the reading is what makes an import safe to run twice.
func FillupID(on string, odometer int) string {
	return fmt.Sprintf("%s-%d", on, odometer)
}

// Date is the day of the fill, or the zero time if it cannot be read.
func (f Fillup) Date() time.Time {
	return ParseDate(f.On)
}

// Cost is what this tank cost: the stored total when there is one, otherwise
// gallons times the price a gallon.
//
// Rounding happens once, here, at the point where a real receipt rounds. Every
// total on the site is a sum of these rather than a sum of the products, so
// what the page says you have spent is the sum of what you actually paid.
func (f Fillup) Cost() int64 {
	if f.CostCents > 0 {
		return f.CostCents
	}

	return Round(f.Gallons * float64(f.PricePerGallonCents))
}

// PumpPriceCents is what the sign by the road said: what was paid plus whatever
// came off it.
func (f Fillup) PumpPriceCents() int64 {
	return f.PricePerGallonCents + f.FuelSaverCents
}

// Saved is what the discount was worth on this tank.
func (f Fillup) Saved() int64 {
	if f.FuelSaverCents <= 0 {
		return 0
	}

	return Round(f.Gallons * float64(f.FuelSaverCents))
}

// Discounted reports whether a discount was used, so a page can leave the
// column empty rather than writing $0.00 sixty times.
func (f Fillup) Discounted() bool {
	return f.FuelSaverCents > 0
}

// FullTank reports whether this fill can carry a miles-per-gallon figure of its
// own. A partial tank and a tank with a missed one behind it both can not.
func (f Fillup) FullTank() bool {
	return !f.Partial && !f.Missed
}

// -------------------------------------------------------------- services ----

// Service is one visit to a shop: what was done, what it cost, and what the
// odometer read when it happened.
//
// The reading is what makes a service more than a receipt. It is how "when is
// the next oil change" gets answered, and how the interval between two of them
// is measured in the unit that actually wears a car out.
type Service struct {
	// ID is "2026-08-07-95077", the same date-and-reading shape a fill-up uses
	// and for the same reason.
	ID      string `json:"id"`
	Vehicle string `json:"vehicle"`

	On       string `json:"on"`
	Odometer int    `json:"odometer"`

	// Shop is who did it - "West Side VW", "my driveway".
	Shop string `json:"shop"`
	// Work is what was done, one item a line. It is free text because that is
	// what an invoice is, and because a fixed list of jobs would be wrong by
	// the second visit.
	Work string `json:"work"`
	// CostCents is what it cost, all in.
	CostCents int64 `json:"cost_cents"`
	// Note is anything worth remembering that is not the work itself: a
	// warranty, a part number, what they said about the tyres.
	Note string `json:"note"`

	By      string `json:"by"`
	Created int64  `json:"created"`
	Updated int64  `json:"updated"`
}

// ServiceID is the id a service visit is stored under.
func ServiceID(on string, odometer int) string {
	return fmt.Sprintf("%s-%d", on, odometer)
}

// Date is the day of the visit, or the zero time if it cannot be read.
func (s Service) Date() time.Time {
	return ParseDate(s.On)
}

// Items is the work split into lines, for a page that wants to list it rather
// than print a paragraph.
func (s Service) Items() []string {
	items := []string{}

	for _, line := range strings.Split(s.Work, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			items = append(items, trimmed)
		}
	}

	return items
}

// Summary is the work on one line, for a table. The full text is on the
// vehicle's own page.
func (s Service) Summary() string {
	items := s.Items()

	switch len(items) {
	case 0:
		return "Service"

	case 1:
		return items[0]
	}

	return fmt.Sprintf("%s, and %d more", items[0], len(items)-1)
}

// Mentions reports whether this visit's work mentions a job, case-insensitively.
// It is how the oil-change interval is found without asking anybody to tick a
// box that did not exist on the invoice.
func (s Service) Mentions(job string) bool {
	return strings.Contains(strings.ToLower(s.Work), strings.ToLower(job))
}

// ---------------------------------------------------------------- dates ----

// ParseDate reads a stored date. Anything unreadable comes back as the zero
// time, which every caller here already has to handle for a field that was
// never filled in.
func ParseDate(stored string) time.Time {
	if stored == "" {
		return time.Time{}
	}

	parsed, err := time.ParseInLocation(DateLayout, stored, Local)
	if err != nil {
		return time.Time{}
	}

	return parsed
}

// Today is the current day in the site's clock, with the time of day stripped -
// which is what makes "days owned" a whole number that only changes at
// midnight rather than one that depends on when the page was loaded.
func Today(now time.Time) time.Time {
	local := now.In(Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, Local)
}

// DaysBetween counts whole days from one day to another. Both are floored to a
// day first, so two timestamps a few hours apart on the same date are nought
// days apart rather than one.
//
// The subtraction is rounded rather than truncated because a span crossing a
// daylight saving boundary is 23 or 25 hours long, and an integer division
// would quietly lose a day twice a year.
func DaysBetween(from time.Time, to time.Time) int {
	if from.IsZero() || to.IsZero() {
		return 0
	}

	return int(math.Round(Today(to).Sub(Today(from)).Hours() / 24))
}

// ByDate puts fill-ups in the order they happened, oldest first, and breaks a
// tie on the odometer.
//
// Everything downstream reads each fill against the one before it, so this
// order is not a presentation choice - it is what makes the arithmetic mean
// anything. The two rows in the sheet that share a date are exactly why the
// tie-break is here.
func ByDate(fills []Fillup) {
	sort.SliceStable(fills, func(a int, b int) bool {
		if fills[a].On != fills[b].On {
			return fills[a].On < fills[b].On
		}

		return fills[a].Odometer < fills[b].Odometer
	})
}

// ServicesByDate puts shop visits in the order they happened, oldest first.
func ServicesByDate(services []Service) {
	sort.SliceStable(services, func(a int, b int) bool {
		if services[a].On != services[b].On {
			return services[a].On < services[b].On
		}

		return services[a].Odometer < services[b].Odometer
	})
}

// Round turns a float into whole cents. Money leaves the float world here and
// nowhere else.
func Round(value float64) int64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}

	return int64(math.Round(value))
}
