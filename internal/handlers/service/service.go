// Package service_handler is the maintenance history: every visit to a shop,
// what was done, and the form that adds one.
package service_handler

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

func AddServiceV1(router *gin.Engine) {
	router.GET("/vehicle/:slug/service", auth.RequiredPage(), Log)
	router.POST("/vehicle/:slug/service", auth.RequiredPage(), access.CanEdit(), Save)
	router.POST("/vehicle/:slug/service/delete", auth.RequiredPage(), access.CanEdit(), Delete)
}

func Log(c *gin.Context) {
	page, report, ok := access.Car(c, "", "service")
	if !ok {
		return
	}

	page.Title = page.Vehicle.Called() + " service history"

	if editing := c.Query("edit"); editing != "" {
		for _, visit := range report.Services {
			if visit.ID == editing {
				page.Service = visit
				page.Editing = editing
				break
			}
		}

		if page.Editing == "" {
			page.Error = "That visit is no longer here - it may have been changed or deleted from another tab."
		}
	}

	if page.Editing == "" && page.Service.On == "" {
		page.Service.On = types.Today(time.Now()).Format(types.DateLayout)
	}

	web.Render(c, http.StatusOK, web.ServicesPage, page)
}

func Save(c *gin.Context) {
	vehicle, ok := access.Vehicle(c)
	if !ok {
		return
	}

	if !access.Editor(c, vehicle) {
		return
	}

	editing := strings.TrimSpace(c.PostForm("editing"))

	service, problem := read(c, vehicle)
	if problem != "" {
		page, _, ok := access.Car(c, "Service history", "service")
		if !ok {
			return
		}

		page.Service = service
		page.Editing = editing
		page.Error = problem

		web.Render(c, http.StatusBadRequest, web.ServicesPage, page)
		return
	}

	if _, err := data.SaveService(service, editing, auth.Email(c)); err != nil {
		log.Printf("could not save a service on %s: %s", vehicle.Slug, err.Error())
		access.Failed(c, "We couldn't save that visit. Please try again.")
		return
	}

	notice := "logged"
	if editing != "" {
		notice = "saved"
	}

	access.Back(c, "/vehicle/"+vehicle.Slug+"/service", notice)
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
		access.Back(c, "/vehicle/"+vehicle.Slug+"/service", "")
		return
	}

	if err := data.DeleteService(vehicle.Slug, id); err != nil {
		log.Printf("could not delete service %s on %s: %s", id, vehicle.Slug, err.Error())
		access.Failed(c, "We couldn't delete that visit. Please try again.")
		return
	}

	access.Back(c, "/vehicle/"+vehicle.Slug+"/service", "deleted")
}

// read pulls a shop visit out of a posted form.
//
// A visit with no cost on it is allowed: warranty work, a recall, and a
// rotation thrown in are all real entries, and the reason to record them is the
// odometer reading rather than the money. What is not allowed is a visit with
// neither work nor cost, which is not a visit.
func read(c *gin.Context, vehicle types.Vehicle) (service types.Service, problem string) {
	service.Vehicle = vehicle.Slug
	service.Shop = strings.TrimSpace(c.PostForm("shop"))
	service.Note = strings.TrimSpace(c.PostForm("note"))
	service.Work = strings.TrimSpace(strings.ReplaceAll(c.PostForm("work"), "\r\n", "\n"))

	on := strings.TrimSpace(c.PostForm("on"))
	if on == "" || types.ParseDate(on).IsZero() {
		return service, "That date is not a date."
	}
	service.On = on

	odometer, err := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(c.PostForm("odometer"), ",", "")))
	if err != nil {
		return service, "That odometer reading is not a number."
	}

	if odometer <= 0 {
		return service, "A visit needs an odometer reading. It is what makes the next one predictable."
	}
	service.Odometer = odometer

	cost, err := money.Parse(c.PostForm("cost"))
	if err != nil {
		return service, "That cost is not an amount."
	}

	if cost < 0 {
		return service, "A cost below zero is a refund, which this does not know how to record."
	}
	service.CostCents = cost

	if service.Work == "" && cost == 0 {
		return service, "There is nothing on this visit - no work and no cost."
	}

	return service, ""
}
