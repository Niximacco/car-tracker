package web

import (
	"embed"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Niximacco/car-tracker/internal/chart"
	"github.com/Niximacco/car-tracker/internal/config"
	"github.com/Niximacco/car-tracker/internal/importer"
	"github.com/Niximacco/car-tracker/internal/money"
	"github.com/Niximacco/car-tracker/internal/stats"
	"github.com/Niximacco/car-tracker/internal/types"
	"github.com/gin-gonic/gin"
)

// Templates ship inside the binary so there is nothing to mount or copy at
// deploy time, and a cold start does not touch the filesystem.
//
//go:embed templates/*.html
var templateFS embed.FS

const (
	LoginPage    = "login.html"
	SentPage     = "sent.html"
	MessagePage  = "message.html"
	GaragePage   = "garage.html"
	VehiclePage  = "vehicle.html"
	FillupsPage  = "fillups.html"
	ServicesPage = "services.html"
	InsightsPage = "insights.html"
	SettingsPage = "settings.html"
	NewCarPage   = "newcar.html"
	ImportPage   = "import.html"
	UsersPage    = "users.html"
	ProfilePage  = "profile.html"
)

var pages = map[string]*template.Template{}

// funcs are the helpers the templates can call.
//
// Every one of them is a formatter. There is no arithmetic in this map and none
// in the templates: a number that appears on a page was worked out in the stats
// package, where a test can look at it. What is here decides how many decimal
// places it wears.
var funcs = template.FuncMap{
	// ------------------------------------------------------------ money ----

	// money writes whole cents as "$129.57", and moneyBlank writes nothing for
	// nothing - for the columns where "$0.00" would read as a decision rather
	// than as an empty cell.
	"money":      money.Format,
	"moneyBlank": money.Blank,
	"moneyInput": money.Input,

	// dollars writes a fractional number of cents as money, for the averages -
	// a price a gallon worked out over four years is not a whole number of
	// cents and pretending otherwise loses the difference between $3.61 and
	// $3.6149.
	"dollars": func(cents float64) string {
		return money.Format(types.Round(cents))
	},

	// cents writes a small per-something amount to three places: "$0.105" a
	// mile. Two places would round every one of these to a dime and make the
	// column useless, which is exactly why the sheet carried three.
	"cents": func(value float64) string {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return "-"
		}

		return fmt.Sprintf("$%.3f", value/100)
	},

	// ----------------------------------------------------------- number ----

	// miles writes a whole number with thousands separators. Odometer readings
	// are six digits and unreadable without them.
	"miles": commas,
	"count": commas,

	// gallons writes a volume the way a pump does, to three places.
	"gallons": func(value float64) string {
		if value <= 0 {
			return "-"
		}

		return trimZeros(fmt.Sprintf("%.3f", value))
	},

	// mpg writes a fuel figure to one place. A second decimal on a number this
	// noisy is precision that is not there.
	"mpg": func(value float64) string {
		if value <= 0 {
			return "-"
		}

		return fmt.Sprintf("%.1f", value)
	},

	// one is the general-purpose decimal, for the averages that are neither
	// money nor fuel: days between fill-ups, miles a day.
	"one": func(value float64) string {
		if value == 0 {
			return "-"
		}

		return fmt.Sprintf("%.1f", value)
	},

	// percent writes a proportion. The sign is kept on it, because the one
	// place a negative turns up - summer against winter on a car that somehow
	// does better in the cold - is a fact worth seeing rather than hiding.
	"percent": func(value float64) string {
		if value == 0 {
			return "-"
		}

		return fmt.Sprintf("%+.1f%%", value)
	},

	// share writes a proportion that cannot be negative, so it goes without the
	// sign: what fraction of your fill-ups happen at one station.
	"share": func(value float64) string {
		if value <= 0 {
			return "-"
		}

		return fmt.Sprintf("%.0f%%", value)
	},

	// ------------------------------------------------------------ dates ----

	// day writes a stored date as "Aug 11, 2026". Every date on this site
	// carries its year: the whole point of the history is that it is four years
	// long, and "Aug 11" in a column that spans 2021 to 2026 is not a date.
	"day": func(stored string) string {
		parsed := types.ParseDate(stored)
		if parsed.IsZero() {
			return "-"
		}

		return parsed.Format("Jan 2, 2006")
	},

	// clock writes a stored time of day as "4:43 PM", or nothing.
	"clock": func(stored string) string {
		parsed, err := time.Parse("15:04", stored)
		if err != nil {
			return ""
		}

		return parsed.Format("3:04 PM")
	},

	// when writes a unix timestamp, for the account pages.
	"when": func(seconds int64) string {
		if seconds <= 0 {
			return "-"
		}

		return time.Unix(seconds, 0).In(types.Local).Format("Jan 2, 2006")
	},

	// days writes a number of days the way somebody says it out loud: a
	// fortnight is "14 days", a year and a half is "1y 6m".
	"days": humanDays,

	// ago is days rendered as a distance backwards from today.
	"ago": func(stored string) string {
		parsed := types.ParseDate(stored)
		if parsed.IsZero() {
			return ""
		}

		switch days := types.DaysBetween(parsed, types.Today(time.Now())); {
		case days < 0:
			return "in " + humanDays(-days)

		case days == 0:
			return "today"

		case days == 1:
			return "yesterday"
		}

		return humanDays(types.DaysBetween(parsed, types.Today(time.Now()))) + " ago"
	},

	// ------------------------------------------------------------- text ----

	"accountName": types.AccountName,

	// station folds a typed location into what the breakdown calls it, so a
	// blank one reads as "Not recorded" rather than as a gap.
	"station": stats.StationName,

	"monthName": func(month time.Month) string { return month.String() },

	// lines splits a service record's work into its items.
	"lines": func(text string) []string {
		out := []string{}

		for _, line := range strings.Split(text, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				out = append(out, trimmed)
			}
		}

		return out
	},

	// ------------------------------------------------------------ shape ----

	// reverse is the history newest first, which is how every log on the site
	// is read. The arithmetic needs it oldest first, so rather than keeping two
	// orderings of the same data the pages turn it round on the way out.
	"reverseFills": func(fills []stats.Fill) []stats.Fill {
		out := make([]stats.Fill, 0, len(fills))
		for at := len(fills) - 1; at >= 0; at-- {
			out = append(out, fills[at])
		}

		return out
	},

	"reverseServices": func(services []types.Service) []types.Service {
		out := make([]types.Service, 0, len(services))
		for at := len(services) - 1; at >= 0; at-- {
			out = append(out, services[at])
		}

		return out
	},

	// bar is how wide to draw a proportional bar, as a percentage of the widest
	// thing beside it. It is a formatter rather than arithmetic: the comparison
	// is the caller's, this only decides that nothing is drawn narrower than a
	// sliver, so a real value never renders as nothing at all.
	"bar": func(value float64, highest float64) string {
		if highest <= 0 || value <= 0 {
			return "0"
		}

		width := value / highest * 100
		if width < 1.5 {
			width = 1.5
		}

		return fmt.Sprintf("%.1f", width)
	},
}

func init() {
	for _, page := range []string{
		LoginPage, SentPage, MessagePage, GaragePage, VehiclePage, FillupsPage,
		ServicesPage, InsightsPage, SettingsPage, NewCarPage, ImportPage,
		UsersPage, ProfilePage,
	} {
		tmpl := template.New(page).Funcs(funcs)
		pages[page] = template.Must(tmpl.ParseFS(templateFS, "templates/base.html", "templates/"+page))
	}
}

// Page is everything the templates can render. Fields that do not apply to a
// given page are simply left empty.
type Page struct {
	Title    string
	SiteName string
	BaseURL  string
	Email    string
	SignedIn bool
	IsAdmin  bool
	// ViewOnly is set for an account that may read the garage and change
	// nothing. The routes are what actually enforce that; this is so the pages
	// do not offer buttons that would only ever come back refused.
	ViewOnly bool
	// Stylesheet is the versioned path of the site's css. It carries a hash of
	// the file in its name, so a deploy that changes the styling changes this
	// too and no browser is left holding the old one.
	Stylesheet string
	// FontAwesomeKit is the kit that draws the icons, or empty for a site
	// without them. The pages are written to read either way.
	FontAwesomeKit string
	// Nav is which navigation link to mark as current.
	Nav string

	Next           string
	Error          string
	Message        string
	Notice         string
	ExpiresMinutes int

	// Garage state.
	Vehicles []types.Vehicle
	// Cards is the garage's summary of each car, worked out once on the server
	// so the page and the car's own page cannot disagree about its mileage.
	Cards []Card

	// One vehicle, and everything worked out about it.
	Vehicle types.Vehicle
	Report  stats.Report
	// CanManage is whether this visitor may change the vehicle itself, as
	// opposed to logging against it. It is on the page rather than worked out in
	// a template because the answer involves the account and the vehicle, and a
	// template that gets it wrong offers a button that 403s.
	CanManage bool

	// The charts, drawn from the report. They are built on the server for the
	// same reason the templates are compiled in: there is nothing to fetch, and
	// the arithmetic behind a line on a page is somewhere a test can reach it.
	MPGChart   chart.Chart
	PriceChart chart.Chart
	CostChart  chart.Chart

	// One entry being edited, for the forms.
	Fillup  types.Fillup
	Service types.Service
	// Editing is the id of the entry being changed, or empty for a new one. It
	// is what the form posts back so a corrected date can move the entry rather
	// than leaving the old one behind.
	Editing string

	// Import state.
	Preview importer.Preview
	Pasted  string
	Added   int
	Updated int
	// Seeded says the preview on screen came from the built-in copy of the
	// spreadsheet rather than from something somebody pasted.
	Seeded bool
	// How much history the built-in copy holds, so the offer to load it can say
	// what it is offering rather than being a button marked "trust me".
	SeedFillups  int
	SeedServices int

	// Admin state.
	Users []types.User
	// You is the signed-in account itself, for the one page that is about it.
	You types.User

	// The largest value in each table that is drawn with bars behind its
	// numbers, so a bar has something to be a proportion of. They are worked
	// out on the server because a template cannot take a maximum, and one per
	// table because a year's spending and a station's visit count share no
	// scale at all.
	WidestYear    float64
	WidestStation float64
	WidestMonth   float64
}

// Card is one vehicle as the garage lists it: enough to tell them apart and to
// see which one is costing you.
type Card struct {
	Vehicle types.Vehicle
	Summary stats.Summary
	// Shared is set for a car somebody else owns and let you see, so the garage
	// can say whose it is.
	Shared bool
	// LastOn is the most recent entry of any kind, which is the one thing that
	// says whether this car's history is being kept up.
	LastOn string
}

// New starts a Page with the site-wide values already filled in.
func New(title string) Page {
	return Page{
		Title:          title,
		SiteName:       config.SITE_NAME,
		BaseURL:        config.BASE_URL,
		Stylesheet:     Stylesheet,
		FontAwesomeKit: config.FONTAWESOME_KIT,
	}
}

// Render writes an html page. Everything interpolated goes through
// html/template, so user-supplied values are escaped for their context.
func Render(c *gin.Context, status int, name string, page Page) {
	tmpl, ok := pages[name]
	if !ok {
		log.Printf("no such template: %s", name)
		c.String(http.StatusInternalServerError, "template error")
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)

	if err := tmpl.ExecuteTemplate(c.Writer, "base", page); err != nil {
		log.Printf("could not render %s: %s", name, err.Error())
	}
}

// SafeNext sanitizes a "?next=" value so it can only ever send a browser to a
// path on this site. Anything that could resolve to another origin - an
// absolute url, a protocol-relative "//host", a backslash trick - collapses to
// the front page.
func SafeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") {
		return "/"
	}

	if strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}

	if strings.ContainsAny(next, "\r\n") {
		return "/"
	}

	return next
}

// TooManyRequests renders the page a caller gets when they have been turned
// away by a rate limit, with a Retry-After for anything that reads one.
func TooManyRequests(retryAfter time.Duration) gin.HandlerFunc {
	seconds := strconv.Itoa(int(retryAfter.Seconds()))

	return func(c *gin.Context) {
		c.Header("Retry-After", seconds)

		page := New("Too many attempts")
		page.Error = "Too many attempts from your connection. Wait a minute and try again."
		Render(c, http.StatusTooManyRequests, MessagePage, page)
	}
}

// commas groups a whole number in threes.
func commas(value int) string {
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	}

	digits := strconv.Itoa(value)
	if len(digits) <= 3 {
		return sign + digits
	}

	var out strings.Builder
	lead := len(digits) % 3

	if lead > 0 {
		out.WriteString(digits[:lead])
	}

	for at := lead; at < len(digits); at += 3 {
		if out.Len() > 0 {
			out.WriteByte(',')
		}

		out.WriteString(digits[at : at+3])
	}

	return sign + out.String()
}

// humanDays writes a span the way somebody says it. Anything under a season
// stays in days, because that is the unit a fill-up interval is thought in;
// past that it becomes years and months, because "1,705 days" is a number
// nobody can picture.
func humanDays(days int) string {
	switch {
	case days <= 0:
		return "-"

	case days == 1:
		return "1 day"

	case days < 100:
		return fmt.Sprintf("%d days", days)
	}

	years := days / 365
	months := (days % 365) / 30

	switch {
	case years == 0:
		return fmt.Sprintf("%d months", months)

	case months == 0:
		return fmt.Sprintf("%dy", years)
	}

	return fmt.Sprintf("%dy %dm", years, months)
}

// trimZeros takes the trailing noughts off a decimal, and the point with them,
// so a round twelve gallons reads as "12" rather than "12.000".
func trimZeros(text string) string {
	if !strings.Contains(text, ".") {
		return text
	}

	return strings.TrimSuffix(strings.TrimRight(text, "0"), ".")
}
