package config

import (
	"log"
	"os"
	"regexp"
	"strings"
)

var (
	// BASE_URL is the public origin the service is reached at. It is what this
	// site is registered at auth.ajn.me under, so it has to match what the
	// browser actually sees or no sign-in link will come back to it.
	BASE_URL = "https://car.ajn.me"
	// SITE_NAME is what the pages call this service.
	SITE_NAME = "Car Tracker"
	// FONTAWESOME_KIT is the id of the Font Awesome kit that draws the icons,
	// or empty for a site with no icons on it. It is an identifier rather than
	// a credential - it ends up in the page source of every request - but it is
	// configuration rather than code, because the kit belongs to an account
	// this repository knows nothing about.
	//
	// Everything still reads and works without one: the pages are laid out and
	// labelled in words, and an icon that never arrives leaves an empty inline
	// element behind rather than a gap or a broken box.
	FONTAWESOME_KIT = ""
)

// kitID is what a Font Awesome kit id looks like. The value is interpolated
// into a script src, so it is checked against this rather than trusted: a
// mis-set variable should turn the icons off, not put arbitrary text into a
// <script> tag. html/template would escape it anyway; this is the belt to that
// pair of braces.
var kitID = regexp.MustCompile(`^[a-zA-Z0-9]{6,32}$`)

func init() {
	if baseURL := os.Getenv("APP_BASE_URL"); baseURL != "" {
		BASE_URL = strings.TrimRight(baseURL, "/")
	} else {
		log.Printf("APP_BASE_URL is not set, this service will call itself %s", BASE_URL)
	}

	if siteName := os.Getenv("SITE_NAME"); siteName != "" {
		SITE_NAME = siteName
	}

	switch kit := strings.TrimSpace(os.Getenv("FONTAWESOME_KIT")); {
	case kit == "":
		log.Printf("FONTAWESOME_KIT is not set, the pages will render without icons")

	case kitID.MatchString(kit):
		FONTAWESOME_KIT = kit

	default:
		log.Printf("FONTAWESOME_KIT does not look like a kit id, the pages will render without icons")
	}
}
