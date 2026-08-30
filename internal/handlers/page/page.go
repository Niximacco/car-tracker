// Package page_handler is the two screens that are not about one vehicle: the
// garage, and your own account.
package page_handler

import (
	"log"
	"net/http"
	"strings"

	"github.com/Niximacco/car-tracker/internal/access"
	"github.com/Niximacco/car-tracker/internal/auth"
	data "github.com/Niximacco/car-tracker/internal/cloud"
	"github.com/Niximacco/car-tracker/internal/types"
	"github.com/Niximacco/car-tracker/internal/web"
	"github.com/gin-gonic/gin"
)

func AddPagesV1(router *gin.Engine) {
	router.GET("/", auth.RequiredPage(), Garage)

	router.GET("/profile", auth.RequiredPage(), Profile)
	router.POST("/profile", auth.RequiredPage(), access.CanEdit(), SaveProfile)
}

// Garage is the front page: every car this account may see, with enough of each
// one's summary to tell them apart and to see which is costing you.
//
// It reads each car's whole history to build those figures, which is one query
// a car rather than one query. That is deliberate: a household has a handful of
// vehicles, and the alternative is storing a summary on the vehicle - which is
// a stored total, and a stored total is one forgotten fill-up away from being
// a lie on the front page.
func Garage(c *gin.Context) {
	user := auth.User(c)

	vehicles, err := data.VehiclesFor(user)
	if err != nil {
		log.Printf("could not list vehicles: %s", err.Error())
		access.Failed(c, "We couldn't read the garage. Please try again.")
		return
	}

	page := access.Page(c, "Garage")
	page.Nav = "garage"
	page.Vehicles = vehicles

	for _, vehicle := range vehicles {
		card := web.Card{
			Vehicle: vehicle,
			Shared:  !strings.EqualFold(vehicle.Owner, user.Email),
		}

		report, err := access.Report(vehicle)
		if err != nil {
			// One car whose history will not load should not take the whole
			// garage down with it. The card renders with empty figures, which
			// reads as a car with nothing logged - so the log line is the only
			// place this is visible, and that is the right trade for a page
			// whose job is to list the others.
			log.Printf("could not summarise %s for the garage: %s", vehicle.Slug, err.Error())
		} else {
			card.Summary = report.Summary
			card.LastOn = lastEntry(report.Summary.LastOn, report.Services)
		}

		page.Cards = append(page.Cards, card)
	}

	web.Render(c, http.StatusOK, web.GaragePage, page)
}

// lastEntry is the most recent thing that happened to a car, fill-up or shop
// visit, as a stored date. It is what tells you at a glance whether a car's
// history is being kept up.
func lastEntry(lastFill string, services []types.Service) string {
	latest := lastFill

	if len(services) > 0 {
		if on := services[len(services)-1].On; on > latest {
			latest = on
		}
	}

	return latest
}

func Profile(c *gin.Context) {
	page := access.Page(c, "Your account")
	page.Nav = "profile"

	web.Render(c, http.StatusOK, web.ProfilePage, page)
}

func SaveProfile(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))

	if _, err := data.UpdateUser(auth.Email(c), &name, nil, nil, nil); err != nil {
		log.Printf("could not save a profile: %s", err.Error())
		access.Failed(c, "We couldn't save that. Please try again.")
		return
	}

	access.Back(c, "/profile", "saved")
}
