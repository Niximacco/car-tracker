// Package fillup_handler is the fuel log: the whole history as a table, and the
// one form that adds to it or corrects it.
package fillup_handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Niximacco/car-tracker/internal/access"
	"github.com/Niximacco/car-tracker/internal/auth"
	data "github.com/Niximacco/car-tracker/internal/cloud"
	"github.com/Niximacco/car-tracker/internal/money"
	"github.com/Niximacco/car-tracker/internal/types"
	"github.com/Niximacco/car-tracker/internal/web"
	"github.com/gin-gonic/gin"
)

func AddFillupV1(router *gin.Engine) {
	router.GET("/vehicle/:slug/fuel", auth.RequiredPage(), Log)
	router.POST("/vehicle/:slug/fuel", auth.RequiredPage(), access.CanEdit(), Save)
	router.POST("/vehicle/:slug/fuel/delete", auth.RequiredPage(), access.CanEdit(), Delete)
}

// Log is the fuel history and the form under it.
//
// An "?edit=" on the address fills the form in with an existing entry rather
// than starting an empty one. It is in the address rather than in a script so
// that the edit form is a page you can land on, reload and come back to.
func Log(c *gin.Context) {
	page, report, ok := access.Car(c, "", "fuel")
	if !ok {
		return
	}

	page.Title = page.Vehicle.Called() + " fuel log"

	if editing := c.Query("edit"); editing != "" {
		for _, fill := range report.Fills {
			if fill.ID == editing {
				page.Fillup = fill.Fillup
				page.Editing = editing
				break
			}
		}

		if page.Editing == "" {
			page.Error = "That fill-up is no longer here - it may have been changed or deleted from another tab."
		}
	}

	// A new entry starts on today, because the overwhelmingly common case is
	// logging a fill-up at the pump or shortly after.
	if page.Editing == "" && page.Fillup.On == "" {
		page.Fillup.On = types.Today(time.Now()).Format(types.DateLayout)
	}

	web.Render(c, http.StatusOK, web.FillupsPage, page)
}

// Save writes a fill-up, new or corrected.
func Save(c *gin.Context) {
	vehicle, ok := access.Vehicle(c)
	if !ok {
		return
	}

	if !access.Editor(c, vehicle) {
		return
	}

	editing := strings.TrimSpace(c.PostForm("editing"))

	fillup, problem := read(c, vehicle)
	if problem != "" {
		page, _, ok := access.Car(c, "Fuel log", "fuel")
		if !ok {
			return
		}

		page.Fillup = fillup
		page.Editing = editing
		page.Error = problem

		web.Render(c, http.StatusBadRequest, web.FillupsPage, page)
		return
	}

	// The id is the day and the reading, so correcting either of those means a
	// new entity. Handing the old id in is what lets the write and the tidy-up
	// happen in one transaction rather than leaving a ghost row behind when the
	// second half fails.
	if _, err := data.SaveFillup(fillup, editing, auth.Email(c)); err != nil {
		log.Printf("could not save a fill-up on %s: %s", vehicle.Slug, err.Error())
		access.Failed(c, "We couldn't save that fill-up. Please try again.")
		return
	}

	notice := "logged"
	if editing != "" {
		notice = "saved"
	}

	access.Back(c, "/vehicle/"+vehicle.Slug+"/fuel", notice)
}

func Delete(c *gin.Context) {
	vehicle, ok := access.Vehicle(c)
	if !ok {
		return
	}

	if !access.Editor(c, vehicle) {
		return
	}

	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		access.Back(c, "/vehicle/"+vehicle.Slug+"/fuel", "")
		return
	}

	if err := data.DeleteFillup(vehicle.Slug, id); err != nil {
		log.Printf("could not delete fill-up %s on %s: %s", id, vehicle.Slug, err.Error())
		access.Failed(c, "We couldn't delete that fill-up. Please try again.")
		return
	}

	access.Back(c, "/vehicle/"+vehicle.Slug+"/fuel", "deleted")
}

// read pulls a fill-up out of a posted form.
//
// Everything it refuses is something the person typing can fix, so it hands
// back the sentence to put on the page rather than an error to log. The one
// thing it deliberately does not refuse is a figure that is merely surprising -
// a tank bigger than the tank, an odometer that went backwards. Those are
// flagged on the row afterwards, because every one of them is occasionally
// true and a log that cannot record what happened is not a log.
func read(c *gin.Context, vehicle types.Vehicle) (fillup types.Fillup, problem string) {
	fillup.Vehicle = vehicle.Slug
	fillup.Station = strings.TrimSpace(c.PostForm("station"))
	fillup.Note = strings.TrimSpace(c.PostForm("note"))
	fillup.Partial = c.PostForm("partial") != ""
	fillup.Missed = c.PostForm("missed") != ""

	on := strings.TrimSpace(c.PostForm("on"))
	if on == "" || types.ParseDate(on).IsZero() {
		return fillup, "That date is not a date."
	}
	fillup.On = on

	if at := strings.TrimSpace(c.PostForm("at")); at != "" {
		parsed, err := time.Parse("15:04", at)
		if err != nil {
			return fillup, "That time is not a time."
		}

		fillup.At = parsed.Format("15:04")
	}

	odometer, err := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(c.PostForm("odometer"), ",", "")))
	if err != nil {
		return fillup, "That odometer reading is not a number."
	}

	if odometer <= 0 {
		return fillup, "A fill-up needs an odometer reading. Without one it cannot tell you anything."
	}
	fillup.Odometer = odometer

	gallons, err := strconv.ParseFloat(strings.TrimSpace(strings.ReplaceAll(c.PostForm("gallons"), ",", "")), 64)
	if err != nil {
		return fillup, "That number of gallons is not a number."
	}

	if gallons <= 0 {
		return fillup, "A fill-up needs some fuel in it."
	}
	fillup.Gallons = gallons

	price, err := money.Parse(c.PostForm("price"))
	if err != nil {
		return fillup, "That price a gallon is not an amount."
	}
	fillup.PricePerGallonCents = price

	saver, err := money.Parse(c.PostForm("saver"))
	if err != nil {
		return fillup, "That fuel saver is not an amount."
	}

	if saver < 0 {
		return fillup, "A discount below zero is a surcharge. Put it on the price instead."
	}
	fillup.FuelSaverCents = saver

	cost, err := money.Parse(c.PostForm("cost"))
	if err != nil {
		return fillup, "That total is not an amount."
	}
	fillup.CostCents = cost

	if price <= 0 && cost <= 0 {
		return fillup, "A fill-up needs either a price a gallon or a total. Otherwise it was free."
	}

	// A total with no unit price still has one; it is on the other side of the
	// division. Working it out here rather than leaving it blank is what keeps
	// the price chart and the station averages from having a hole in them.
	if fillup.PricePerGallonCents <= 0 {
		fillup.PricePerGallonCents = types.Round(float64(cost) / gallons)
	}

	// A total that agrees with the arithmetic is not worth storing, and storing
	// it would mean a later correction to the price silently disagreeing with
	// the cost.
	if cost > 0 && absDiff(cost, types.Round(gallons*float64(fillup.PricePerGallonCents))) <= 2 {
		fillup.CostCents = 0
	}

	return fillup, ""
}

func absDiff(a int64, b int64) int64 {
	if a > b {
		return a - b
	}

	return b - a
}
