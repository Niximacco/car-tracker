package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	// The tz database, compiled into the binary. The runtime image is a bare
	// alpine with no zoneinfo in it, and every date on this site is a local
	// wall-clock fact: without this, an evening fill-up logged at nine lands on
	// the following day and the day it belongs to has one fewer.
	_ "time/tzdata"

	data "github.com/Niximacco/car-tracker/internal/cloud"
	asset_handler "github.com/Niximacco/car-tracker/internal/handlers/asset"
	auth_handler "github.com/Niximacco/car-tracker/internal/handlers/auth"
	fillup_handler "github.com/Niximacco/car-tracker/internal/handlers/fillup"
	import_handler "github.com/Niximacco/car-tracker/internal/handlers/imports"
	page_handler "github.com/Niximacco/car-tracker/internal/handlers/page"
	service_handler "github.com/Niximacco/car-tracker/internal/handlers/service"
	user_handler "github.com/Niximacco/car-tracker/internal/handlers/user"
	vehicle_handler "github.com/Niximacco/car-tracker/internal/handlers/vehicle"
	"github.com/Niximacco/car-tracker/internal/router"
	"github.com/Niximacco/car-tracker/internal/types"
	"github.com/gin-gonic/gin"
)

var PORT = ""

func init() {
	PORT = os.Getenv("PORT")
	if PORT == "" {
		PORT = "8080"
	}

	data.Initialize()
}

// bootstrapAdmin creates the first account, if one is named and does not exist.
//
// Access is granted by having an entity in the user kind, and only an admin can
// create one - so a brand new namespace has nobody who can sign in and nobody
// who can let anybody in. This is the way out of that, without hand-writing an
// entity in the console.
//
// It is safe to leave set: an existing user is never touched, so it cannot be
// used to quietly restore admin to somebody who had it taken away.
func bootstrapAdmin() {
	address := data.NormalizeEmail(os.Getenv("BOOTSTRAP_ADMIN_EMAIL"))
	if address == "" {
		return
	}

	_, err := data.NewUser(address, os.Getenv("BOOTSTRAP_ADMIN_NAME"), true, false)

	switch {
	case err == nil:
		log.Printf("created the bootstrap admin account for %s", address)

	case errors.Is(err, data.AlreadyExistsErr):
		// The usual case on every start after the first.

	default:
		log.Printf("could not create the bootstrap admin account: %s", err.Error())
	}
}

func main() {
	if os.Getenv("mode") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	bootstrapAdmin()

	log.Printf("dates on this service are read in %s", types.LocalName)

	router := router.New()

	// Cloud Run needs something to probe that does not require a session.
	router.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Add Routes
	asset_handler.AddAssetV1(router)
	auth_handler.AddAuthV1(router)
	page_handler.AddPagesV1(router)
	vehicle_handler.AddVehicleV1(router)
	fillup_handler.AddFillupV1(router)
	service_handler.AddServiceV1(router)
	import_handler.AddImportV1(router)
	user_handler.AddUserV1(router)

	log.Printf("Running car-tracker on :%s...", PORT)
	if err := router.Run(fmt.Sprintf(":%s", PORT)); err != nil {
		log.Fatal(err.Error())
	}
}
