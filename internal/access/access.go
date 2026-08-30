// Package access is the half of every request that is the same in every
// handler: who is asking, which car they are asking about, and whether they are
// allowed to.
//
// It exists because that answer is not one thing. Signing in is on the account
// and is settled by the middleware; seeing a particular car is on the vehicle
// and cannot be settled until the vehicle has been read. Every page under
// /vehicle/:slug needs both, and a copy of the second one in each handler is
// six places for it to be subtly different.
package access

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/Niximacco/car-tracker/internal/auth"
	data "github.com/Niximacco/car-tracker/internal/cloud"
	"github.com/Niximacco/car-tracker/internal/stats"
	"github.com/Niximacco/car-tracker/internal/types"
	"github.com/Niximacco/car-tracker/internal/web"
	"github.com/gin-gonic/gin"
)

// notices are the things a page can say after a redirect.
//
// A write redirects rather than rendering, so that a reload does not repeat it,
// and the message it wants to show has to survive that. It travels as a code in
// the query string and is looked up here: the page only ever renders text this
// map holds, so nothing a visitor puts in the address bar can end up on the
// page as though the site said it.
var notices = map[string]string{
	"saved":     "Saved.",
	"logged":    "Logged.",
	"deleted":   "Deleted.",
	"added":     "Added.",
	"shared":    "Shared.",
	"unshared":  "No longer shared.",
	"imported":  "Imported.",
	"removed":   "Removed.",
	"retired":   "Marked as retired. Every per-day figure now stops on the day it went.",
	"unretired": "Back in the garage. The per-day figures are counting again.",
	"default":   "Opening the site now goes straight to that vehicle.",
	"nodefault": "Opening the site now shows the garage.",
}

// Page starts a page with the visitor already on it.
func Page(c *gin.Context, title string) web.Page {
	user := auth.User(c)

	page := web.New(title)
	page.Email = auth.Email(c)
	page.SignedIn = auth.IsSignedIn(c)
	page.IsAdmin = user.Admin
	page.ViewOnly = user.ViewOnly
	page.You = user
	page.Notice = notices[c.Query("ok")]

	return page
}

// Vehicle loads the car named in the url and checks the visitor may see it.
//
// A car somebody may not see and a car that does not exist come back the same
// way, on purpose. Answering "that one exists, you just cannot have it" would
// let anybody with a browser enumerate what everybody else drives, and there is
// nothing this site could do with the distinction anyway.
func Vehicle(c *gin.Context) (vehicle types.Vehicle, ok bool) {
	slug := c.Param("slug")

	vehicle, err := data.GetVehicle(slug)
	switch {
	case err == nil:

	case errors.Is(err, data.VehicleNotFoundErr):
		NotFound(c)
		return types.Vehicle{}, false

	default:
		log.Printf("could not read vehicle %s: %s", slug, err.Error())
		Failed(c, "We couldn't read that vehicle. Please try again.")
		return types.Vehicle{}, false
	}

	user := auth.User(c)
	if !user.Admin && !vehicle.CanBeSeenBy(user.Email) {
		NotFound(c)
		return types.Vehicle{}, false
	}

	return vehicle, true
}

// Report loads a car's whole history and works everything out from it.
func Report(vehicle types.Vehicle) (stats.Report, error) {
	fills, err := data.ListFillups(vehicle.Slug)
	if err != nil {
		return stats.Report{}, err
	}

	services, err := data.ListServices(vehicle.Slug)
	if err != nil {
		return stats.Report{}, err
	}

	return stats.Analyze(vehicle, fills, services, time.Now()), nil
}

// Car is the whole of the setup every page under /vehicle/:slug does: load the
// car, check the visitor, read the history, work it out, and start a page with
// all of it on.
//
// It renders the failure itself and reports false, so a handler that gets false
// has nothing left to do but return.
func Car(c *gin.Context, title string, nav string) (page web.Page, report stats.Report, ok bool) {
	vehicle, ok := Vehicle(c)
	if !ok {
		return page, report, false
	}

	report, err := Report(vehicle)
	if err != nil {
		log.Printf("could not read the history for %s: %s", vehicle.Slug, err.Error())
		Failed(c, "We couldn't read this vehicle's history. Please try again.")
		return page, report, false
	}

	page = Page(c, title)
	page.Nav = nav
	page.Vehicle = vehicle
	page.Report = report
	page.CanManage = vehicle.CanBeManagedBy(auth.User(c))

	return page, report, true
}

// CanEdit is the page-shaped version of auth.Editor: the same check, rendering
// a page rather than a line of json.
//
// Every write on this site is a form post from a browser, so a refusal has to
// be something a person can read. auth.Editor answers in json, which is right
// for an api and is a wall of braces in a browser window.
//
// It stands in front of the routes that have no vehicle to check - creating one,
// and editing your own name. Everything under /vehicle/:slug goes through Editor
// or Owner below instead, because those can also answer the question this one
// cannot: whether this is your car.
func CanEdit() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := auth.User(c)

		if !user.CanEdit() {
			Refused(c, "This account can read the garage and nothing else.")
			return
		}

		if !auth.Confirmed(c) {
			page := Page(c, "Try again in a moment")
			page.Error = "We couldn't check what this account is allowed to do. Try again in a moment."
			web.Render(c, http.StatusServiceUnavailable, web.MessagePage, page)
			c.Abort()
			return
		}

		c.Next()
	}
}

// Editor reports whether this visitor may change anything on this car, and
// renders the refusal if not.
//
// It is two questions at once because they fail the same way. A view-only
// account and somebody who was never shared on this car both get the same page:
// there is nothing here for you.
func Editor(c *gin.Context, vehicle types.Vehicle) bool {
	user := auth.User(c)

	if !user.CanEdit() {
		Refused(c, "This account can read the garage and nothing else.")
		return false
	}

	if !auth.Confirmed(c) {
		// Datastore could not be asked what this account is allowed to do. A
		// write would almost certainly fail a moment later anyway, so refusing
		// it here closes the window where a view-only account looks ordinary.
		page := Page(c, "Try again in a moment")
		page.Error = "We couldn't check what this account is allowed to do. Try again in a moment."
		web.Render(c, http.StatusServiceUnavailable, web.MessagePage, page)
		c.Abort()
		return false
	}

	if !user.Admin && !vehicle.CanBeSeenBy(user.Email) {
		NotFound(c)
		return false
	}

	return true
}

// Owner reports whether this visitor may change the vehicle itself rather than
// only log against it, and renders the refusal if not.
func Owner(c *gin.Context, vehicle types.Vehicle) bool {
	if !Editor(c, vehicle) {
		return false
	}

	if !vehicle.CanBeManagedBy(auth.User(c)) {
		Refused(c, "Only "+types.AccountName(vehicle.Owner)+", who owns this vehicle, can change it.")
		return false
	}

	return true
}

// NotFound is the page for a car that is not there, or that is not yours.
func NotFound(c *gin.Context) {
	page := Page(c, "Not found")
	page.Error = "There is no vehicle here."
	web.Render(c, http.StatusNotFound, web.MessagePage, page)
	c.Abort()
}

// Refused is the page for something this account may not do.
func Refused(c *gin.Context, why string) {
	page := Page(c, "Not allowed")
	page.Error = why
	web.Render(c, http.StatusForbidden, web.MessagePage, page)
	c.Abort()
}

// Failed is the page for something that went wrong at our end.
func Failed(c *gin.Context, why string) {
	page := Page(c, "Something went wrong")
	page.Error = why
	web.Render(c, http.StatusInternalServerError, web.MessagePage, page)
	c.Abort()
}

// Back sends a browser to a path with a notice on it. The notice is a code
// rather than the sentence, because the sentence lives in one place and this
// travels through an address bar.
func Back(c *gin.Context, path string, notice string) {
	if notice != "" {
		path += "?ok=" + notice
	}

	c.Redirect(http.StatusSeeOther, path)
}
