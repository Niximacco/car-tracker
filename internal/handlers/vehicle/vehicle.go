// Package vehicle_handler is a car: adding one, looking at one, changing one,
// deciding who else can see it, and getting rid of it.
package vehicle_handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Niximacco/car-tracker/internal/access"
	"github.com/Niximacco/car-tracker/internal/auth"
	data "github.com/Niximacco/car-tracker/internal/cloud"
	"github.com/Niximacco/car-tracker/internal/money"
	"github.com/Niximacco/car-tracker/internal/slug"
	"github.com/Niximacco/car-tracker/internal/types"
	"github.com/Niximacco/car-tracker/internal/web"
	"github.com/gin-gonic/gin"
)

func AddVehicleV1(router *gin.Engine) {
	router.GET("/vehicles/new", auth.RequiredPage(), NewForm)
	router.POST("/vehicles/new", auth.RequiredPage(), access.CanEdit(), Create)

	router.GET("/vehicle/:slug", auth.RequiredPage(), Overview)
	router.GET("/vehicle/:slug/insights", auth.RequiredPage(), Insights)

	router.GET("/vehicle/:slug/settings", auth.RequiredPage(), Settings)
	router.POST("/vehicle/:slug/settings", auth.RequiredPage(), access.CanEdit(), SaveSettings)

	router.POST("/vehicle/:slug/share", auth.RequiredPage(), access.CanEdit(), Share)
	router.POST("/vehicle/:slug/share/remove", auth.RequiredPage(), access.CanEdit(), Unshare)

	router.POST("/vehicle/:slug/delete", auth.RequiredPage(), access.CanEdit(), Delete)
}

// Overview is a car's front page: the block from the bottom of the spreadsheet,
// the three charts, and the last few entries of each kind.
func Overview(c *gin.Context) {
	page, report, ok := access.Car(c, "", "dash")
	if !ok {
		return
	}

	page.Title = page.Vehicle.Called()
	page.MPGChart = web.MPGChart(report)
	page.PriceChart = web.PriceChart(report)
	page.CostChart = web.CostChart(report)
	page.PerMileChart = web.PerMileChart(report)

	web.Render(c, http.StatusOK, web.VehiclePage, page)
}

// Insights is everything the log knows that the log does not say out loud: the
// years, the months, the weather, the stations, the spread, the trend, and the
// extremes.
func Insights(c *gin.Context) {
	page, report, ok := access.Car(c, "", "insights")
	if !ok {
		return
	}

	page.Title = page.Vehicle.Called() + " insights"

	page.SpendChart = web.SpendChart(report)
	page.MilesChart = web.MilesChart(report)
	page.TotalChart = web.TotalChart(report)

	// The scale each table's bars are drawn against. It is worked out here
	// because a template cannot take a maximum, and once per table because a
	// year's spending and a station's visit count share no scale at all.
	for _, year := range report.Years {
		if value := float64(year.TotalCents); value > page.WidestYear {
			page.WidestYear = value
		}
	}

	for _, station := range report.Stations {
		if value := float64(station.Fills); value > page.WidestStation {
			page.WidestStation = value
		}
	}

	for _, month := range report.Seasons {
		if month.MPG > page.WidestMonth {
			page.WidestMonth = month.MPG
		}
	}

	for _, month := range report.Timeline {
		if value := float64(month.TotalCents); value > page.WidestPeriod {
			page.WidestPeriod = value
		}
	}

	for _, day := range report.Habits.Days {
		if value := float64(day.Fills); value > page.WidestDay {
			page.WidestDay = value
		}
	}

	web.Render(c, http.StatusOK, web.InsightsPage, page)
}

// NewForm is the add-a-vehicle page.
func NewForm(c *gin.Context) {
	page := access.Page(c, "Add a vehicle")
	page.Nav = "newcar"

	web.Render(c, http.StatusOK, web.NewCarPage, page)
}

// Create adds a car, owned by whoever added it.
func Create(c *gin.Context) {
	vehicle, problem := read(c, types.Vehicle{})

	if problem == "" {
		vehicle.Slug = slug.Make(vehicle.Name)
		if vehicle.Slug == "" {
			problem = "That name has no letters or digits in it, so there is nothing to build an address from."
		}
	}

	if problem != "" {
		page := access.Page(c, "Add a vehicle")
		page.Nav = "newcar"
		page.Vehicle = vehicle
		page.Error = problem

		web.Render(c, http.StatusBadRequest, web.NewCarPage, page)
		return
	}

	vehicle.Owner = auth.Email(c)

	created, err := data.NewVehicle(vehicle)
	switch {
	case err == nil:

	case errors.Is(err, data.AlreadyExistsErr):
		page := access.Page(c, "Add a vehicle")
		page.Nav = "newcar"
		page.Vehicle = vehicle
		page.Error = "There is already a vehicle at that name. Call this one something else - the name is what its address is built from."

		web.Render(c, http.StatusConflict, web.NewCarPage, page)
		return

	default:
		log.Printf("could not create a vehicle: %s", err.Error())
		access.Failed(c, "We couldn't add that vehicle. Please try again.")
		return
	}

	access.Back(c, "/vehicle/"+created.Slug, "added")
}

// Settings is the page for what a car is, who can see it, and how to get rid of
// it. Reading it needs no more than being able to see the car; the buttons on
// it are what needs the owner, and each of those checks for itself.
func Settings(c *gin.Context) {
	page, _, ok := access.Car(c, "", "settings")
	if !ok {
		return
	}

	if !page.CanManage {
		access.Refused(c, "Only "+types.AccountName(page.Vehicle.Owner)+", who owns this vehicle, can change it.")
		return
	}

	page.Title = page.Vehicle.Called() + " settings"

	web.Render(c, http.StatusOK, web.SettingsPage, page)
}

// SaveSettings writes the vehicle back.
//
// The whole form is written in one transaction rather than field by field,
// because half of it is the origin every lifetime figure is measured from and a
// half-applied change to that is worse than a refused one.
func SaveSettings(c *gin.Context) {
	current, ok := access.Vehicle(c)
	if !ok {
		return
	}

	if !access.Owner(c, current) {
		return
	}

	edited, problem := read(c, current)
	if problem != "" {
		page, _, ok := access.Car(c, "Settings", "settings")
		if !ok {
			return
		}

		page.Error = problem
		web.Render(c, http.StatusBadRequest, web.SettingsPage, page)
		return
	}

	was := current.Retired

	saved, err := data.UpdateVehicle(current.Slug, func(vehicle *types.Vehicle) error {
		// Only the fields the form carries. The share list and the ownership
		// have their own routes, and a form that quietly wrote them back would
		// let a stale tab undo somebody else's change to them.
		vehicle.Name = edited.Name
		vehicle.Year = edited.Year
		vehicle.Make = edited.Make
		vehicle.Model = edited.Model
		vehicle.Trim = edited.Trim
		vehicle.Plate = edited.Plate
		vehicle.VIN = edited.VIN
		vehicle.TankGallons = edited.TankGallons
		vehicle.PurchasedOn = edited.PurchasedOn
		vehicle.PurchaseOdometer = edited.PurchaseOdometer
		vehicle.PurchasePriceCents = edited.PurchasePriceCents
		vehicle.Retired = edited.Retired
		vehicle.SoldOn = edited.SoldOn
		vehicle.SoldOdometer = edited.SoldOdometer
		vehicle.SoldPriceCents = edited.SoldPriceCents

		return nil
	})

	if err != nil {
		log.Printf("could not save vehicle %s: %s", current.Slug, err.Error())
		access.Failed(c, "We couldn't save that. Please try again.")
		return
	}

	notice := "saved"
	switch {
	case saved.Retired && !was:
		notice = "retired"

	case !saved.Retired && was:
		notice = "unretired"
	}

	access.Back(c, "/vehicle/"+saved.Slug+"/settings", notice)
}

// Share adds an address to a car's share list.
//
// It does not check that the address has an account. Sharing with somebody
// before an admin has let them in is a reasonable order to do things in, and
// the list is checked against the session on every request anyway - so an
// address on it with no account behind it grants exactly nothing until there
// is one.
func Share(c *gin.Context) {
	vehicle, ok := access.Vehicle(c)
	if !ok {
		return
	}

	if !access.Owner(c, vehicle) {
		return
	}

	address := data.NormalizeEmail(c.PostForm("email"))
	if !data.ValidAddress(address) {
		page, _, ok := access.Car(c, "Settings", "settings")
		if !ok {
			return
		}

		page.Error = "That doesn't look like an email address."
		web.Render(c, http.StatusBadRequest, web.SettingsPage, page)
		return
	}

	if _, err := data.UpdateVehicle(vehicle.Slug, func(vehicle *types.Vehicle) error {
		vehicle.SharedWith = append(vehicle.SharedWith, address)
		return nil
	}); err != nil {
		log.Printf("could not share %s: %s", vehicle.Slug, err.Error())
		access.Failed(c, "We couldn't share that. Please try again.")
		return
	}

	access.Back(c, "/vehicle/"+vehicle.Slug+"/settings", "shared")
}

// Unshare takes an address off the list. Whatever they logged stays: an entry
// is a record of a tank of fuel, not a possession of whoever typed it in.
func Unshare(c *gin.Context) {
	vehicle, ok := access.Vehicle(c)
	if !ok {
		return
	}

	if !access.Owner(c, vehicle) {
		return
	}

	address := data.NormalizeEmail(c.PostForm("email"))

	if _, err := data.UpdateVehicle(vehicle.Slug, func(vehicle *types.Vehicle) error {
		kept := []string{}
		for _, shared := range vehicle.SharedWith {
			if !strings.EqualFold(shared, address) {
				kept = append(kept, shared)
			}
		}

		vehicle.SharedWith = kept
		return nil
	}); err != nil {
		log.Printf("could not unshare %s: %s", vehicle.Slug, err.Error())
		access.Failed(c, "We couldn't change that. Please try again.")
		return
	}

	access.Back(c, "/vehicle/"+vehicle.Slug+"/settings", "unshared")
}

// Delete removes a car and everything logged against it.
func Delete(c *gin.Context) {
	vehicle, ok := access.Vehicle(c)
	if !ok {
		return
	}

	if !access.Owner(c, vehicle) {
		return
	}

	if err := data.DeleteVehicle(vehicle.Slug); err != nil {
		log.Printf("could not delete %s: %s", vehicle.Slug, err.Error())
		access.Failed(c, "We couldn't delete that vehicle. Please try again.")
		return
	}

	log.Printf("deleted vehicle %s", vehicle.Slug)
	access.Back(c, "/", "deleted")
}

// read pulls a vehicle out of a posted form, keeping whatever the form does not
// carry.
//
// It returns the sentence to put on the page rather than an error, because
// every one of these is something the person typing can fix and none of them is
// something to log.
func read(c *gin.Context, current types.Vehicle) (vehicle types.Vehicle, problem string) {
	vehicle = current

	vehicle.Name = strings.TrimSpace(c.PostForm("name"))
	if vehicle.Name == "" {
		return vehicle, "A vehicle needs a name."
	}

	vehicle.Make = strings.TrimSpace(c.PostForm("make"))
	vehicle.Model = strings.TrimSpace(c.PostForm("model"))
	vehicle.Trim = strings.TrimSpace(c.PostForm("trim"))
	vehicle.Plate = strings.TrimSpace(c.PostForm("plate"))
	vehicle.VIN = strings.TrimSpace(c.PostForm("vin"))

	year, err := whole(c.PostForm("year"))
	if err != nil {
		return vehicle, "That year is not a number."
	}
	vehicle.Year = year

	tank, err := decimal(c.PostForm("tank"))
	if err != nil {
		return vehicle, "That tank size is not a number."
	}
	vehicle.TankGallons = tank

	vehicle.PurchasedOn, err = date(c.PostForm("purchased"))
	if err != nil {
		return vehicle, "That purchase date is not a date."
	}

	vehicle.PurchaseOdometer, err = whole(c.PostForm("odometer"))
	if err != nil {
		return vehicle, "That purchase odometer is not a number."
	}

	vehicle.PurchasePriceCents, err = money.Parse(c.PostForm("price"))
	if err != nil {
		return vehicle, "That purchase price is not an amount."
	}

	vehicle.Retired = c.PostForm("retired") != ""

	vehicle.SoldOn, err = date(c.PostForm("soldon"))
	if err != nil {
		return vehicle, "That sale date is not a date."
	}

	vehicle.SoldOdometer, err = whole(c.PostForm("soldodometer"))
	if err != nil {
		return vehicle, "That sale odometer is not a number."
	}

	vehicle.SoldPriceCents, err = money.Parse(c.PostForm("soldprice"))
	if err != nil {
		return vehicle, "That sale price is not an amount."
	}

	if vehicle.PurchaseOdometer < 0 || vehicle.SoldOdometer < 0 {
		return vehicle, "An odometer does not go below zero."
	}

	if vehicle.SoldOdometer > 0 && vehicle.SoldOdometer < vehicle.PurchaseOdometer {
		return vehicle, "The sale reading is lower than the purchase reading, which would mean the car went backwards."
	}

	return vehicle, ""
}

// whole reads an optional whole number from a form. Empty is nought, which is
// what an unset year and an unset odometer both are.
func whole(typed string) (int, error) {
	typed = strings.TrimSpace(strings.ReplaceAll(typed, ",", ""))
	if typed == "" {
		return 0, nil
	}

	return strconv.Atoi(typed)
}

// decimal reads an optional fractional number from a form.
func decimal(typed string) (float64, error) {
	typed = strings.TrimSpace(strings.ReplaceAll(typed, ",", ""))
	if typed == "" {
		return 0, nil
	}

	return strconv.ParseFloat(typed, 64)
}

// date reads a date field. A browser's date input hands back exactly the layout
// everything is stored in, so this is a check rather than a conversion: a value
// that is not that layout came from somewhere other than the form and is not
// going into datastore.
func date(typed string) (string, error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return "", nil
	}

	if types.ParseDate(typed).IsZero() {
		return "", errors.New("not a date")
	}

	return typed, nil
}
