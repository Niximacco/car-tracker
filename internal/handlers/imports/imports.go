// Package import_handler brings a spreadsheet in.
//
// It is two steps rather than one, always. Reading what was pasted and writing
// it are separate requests, and between them is a screen showing every row as
// it was understood. An import is the one thing on this site that touches four
// years of history at once, and an import that lands before you have looked at
// it is an import you have to undo by hand.
package import_handler

import (
	"log"
	"net/http"
	"strings"

	"github.com/Niximacco/car-tracker/internal/access"
	"github.com/Niximacco/car-tracker/internal/auth"
	data "github.com/Niximacco/car-tracker/internal/cloud"
	"github.com/Niximacco/car-tracker/internal/importer"
	"github.com/Niximacco/car-tracker/internal/seed"
	"github.com/Niximacco/car-tracker/internal/web"
	"github.com/gin-gonic/gin"
)

// seedSize is how much history the built-in copy holds, counted once at start
// rather than on every page draw. It is only there so the offer to load it can
// say what it is offering.
var seedFillups, seedServices int

func init() {
	preview, err := seed.Load(seed.Vehicle(""))
	if err != nil {
		log.Printf("WARNING: the built-in spreadsheet will not read: %s", err.Error())
		return
	}

	seedFillups, seedServices = preview.Fillups, preview.Services

	if preview.Bad > 0 {
		log.Printf("WARNING: %d rows of the built-in spreadsheet do not read", preview.Bad)
	}
}

func AddImportV1(router *gin.Engine) {
	router.GET("/vehicle/:slug/import", auth.RequiredPage(), Form)
	router.POST("/vehicle/:slug/import", auth.RequiredPage(), access.CanEdit(), Preview)
	router.POST("/vehicle/:slug/import/seed", auth.RequiredPage(), access.CanEdit(), FromSeed)
	router.POST("/vehicle/:slug/import/confirm", auth.RequiredPage(), access.CanEdit(), Confirm)
}

func Form(c *gin.Context) {
	page, ok := start(c)
	if !ok {
		return
	}

	web.Render(c, http.StatusOK, web.ImportPage, page)
}

// Preview reads a paste and shows what it would do, writing nothing.
func Preview(c *gin.Context) {
	page, ok := start(c)
	if !ok {
		return
	}

	pasted := c.PostForm("pasted")
	page.Pasted = pasted

	preview, err := importer.Parse(pasted, page.Vehicle, page.Vehicle.TankGallons)
	if err != nil {
		page.Error = capitalize(err.Error())
		web.Render(c, http.StatusBadRequest, web.ImportPage, page)
		return
	}

	page.Preview = preview

	if !preview.Any() {
		page.Error = "Nothing in that could be read as a fill-up or a shop visit."
	}

	web.Render(c, http.StatusOK, web.ImportPage, page)
}

// FromSeed shows the same screen for the copy of the spreadsheet compiled into
// this binary.
func FromSeed(c *gin.Context) {
	page, ok := start(c)
	if !ok {
		return
	}

	preview, err := seed.Load(page.Vehicle)
	if err != nil {
		log.Printf("could not read the built-in spreadsheet: %s", err.Error())
		access.Failed(c, "We couldn't read the built-in history. That is our problem, not yours.")
		return
	}

	page.Preview = preview
	page.Seeded = true

	web.Render(c, http.StatusOK, web.ImportPage, page)
}

// Confirm writes what the preview showed.
//
// It re-reads the text rather than trusting a list of entries posted back from
// the browser, so that what is written is what this service made of what was
// pasted - not whatever a form was carrying by the time it came back.
func Confirm(c *gin.Context) {
	page, ok := start(c)
	if !ok {
		return
	}

	var preview importer.Preview
	var err error

	if c.PostForm("seeded") != "" {
		preview, err = seed.Load(page.Vehicle)
	} else {
		preview, err = importer.Parse(c.PostForm("pasted"), page.Vehicle, page.Vehicle.TankGallons)
	}

	if err != nil {
		page.Error = capitalize(err.Error())
		web.Render(c, http.StatusBadRequest, web.ImportPage, page)
		return
	}

	by := auth.Email(c)

	addedFills, updatedFills, err := data.ImportFillups(preview.FillupsToImport(), by)
	if err != nil {
		log.Printf("could not import fill-ups to %s: %s", page.Vehicle.Slug, err.Error())
		// Whatever went in before the failure stays in, and is reported, because
		// telling somebody nothing happened when half of it did is how an import
		// gets run twice by hand.
		access.Failed(c, "Something went wrong partway through. Some entries may have been imported - read it again and re-run it; nothing will be doubled.")
		return
	}

	addedVisits, updatedVisits, err := data.ImportServices(preview.ServicesToImport(), by)
	if err != nil {
		log.Printf("could not import services to %s: %s", page.Vehicle.Slug, err.Error())
		access.Failed(c, "The fill-ups went in but something went wrong on the shop visits. Read it again and re-run it; nothing will be doubled.")
		return
	}

	log.Printf("imported %d new and %d existing entries to %s",
		addedFills+addedVisits, updatedFills+updatedVisits, page.Vehicle.Slug)

	page.Added = addedFills + addedVisits
	page.Updated = updatedFills + updatedVisits

	web.Render(c, http.StatusOK, web.ImportPage, page)
}

// start is the setup every route here shares: load the car, check the visitor
// owns it, and put the built-in history's size on the page.
//
// Importing needs the owner rather than an editor. It is the one action that
// can rewrite years of somebody else's history in a single click, and the
// person it belongs to is the person who set the car up.
func start(c *gin.Context) (page web.Page, ok bool) {
	page, _, ok = access.Car(c, "Import", "import")
	if !ok {
		return page, false
	}

	if !access.Owner(c, page.Vehicle) {
		return page, false
	}

	page.Title = page.Vehicle.Called() + " import"
	page.SeedFillups = seedFillups
	page.SeedServices = seedServices

	return page, true
}

// capitalize puts an error's sentence into the case a sentence goes in. The
// importer's messages are written to be read by a person and are lower case
// because that is how Go writes an error.
func capitalize(text string) string {
	if text == "" {
		return ""
	}

	return strings.ToUpper(text[:1]) + text[1:]
}
