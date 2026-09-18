package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// The routes are registered on one gin tree in the order main registers them.
// Gin builds that tree at registration time and panics on a conflict, which
// would be a crash at startup rather than a failing request - so it is worth
// pinning down here rather than finding out during a deploy.
//
// The route set is written out rather than imported, because importing the
// handlers would pull in the auth package, whose init refuses to load without a
// signing key in the environment.
//
// The cases this really exists for are "/vehicles/new" beside "/vehicle/:slug",
// which are two static segments that differ by one letter with a parameter
// behind one of them, and the four static segments hanging off that parameter.
// "/vehicles/default" is a second static child of the first of those, and
// "/garage" is a page beside a "/" that is now a redirect.
func TestTheWholeRouteSetRegistersAndResolves(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hit := ""
	mark := func(name string) gin.HandlerFunc {
		return func(c *gin.Context) { hit = name; c.Status(http.StatusOK) }
	}

	r := New()

	r.GET("/healthz", mark("health"))

	// asset_handler.AddAssetV1
	r.GET("/static/:file", mark("asset"))

	// auth_handler.AddAuthV1
	r.GET("/login", mark("login"))
	r.POST("/login", mark("request"))
	r.GET("/auth/callback", mark("complete"))
	r.POST("/login/code", mark("code"))
	r.POST("/logout", mark("logout"))
	r.GET("/api/auth/session", mark("session"))

	// page_handler.AddPagesV1
	r.GET("/", mark("home"))
	r.GET("/garage", mark("garage"))
	r.POST("/vehicles/default", mark("setdefault"))
	r.GET("/profile", mark("profile"))
	r.POST("/profile", mark("saveprofile"))

	// vehicle_handler.AddVehicleV1
	r.GET("/vehicles/new", mark("newcar"))
	r.POST("/vehicles/new", mark("createcar"))
	r.GET("/vehicle/:slug", mark("overview"))
	r.GET("/vehicle/:slug/insights", mark("insights"))
	r.GET("/vehicle/:slug/settings", mark("settings"))
	r.POST("/vehicle/:slug/settings", mark("savesettings"))
	r.POST("/vehicle/:slug/share", mark("share"))
	r.POST("/vehicle/:slug/share/remove", mark("unshare"))
	r.POST("/vehicle/:slug/delete", mark("deletecar"))

	// fillup_handler.AddFillupV1
	r.GET("/vehicle/:slug/fuel", mark("fuel"))
	r.POST("/vehicle/:slug/fuel", mark("savefill"))
	r.POST("/vehicle/:slug/fuel/delete", mark("deletefill"))

	// service_handler.AddServiceV1
	r.GET("/vehicle/:slug/service", mark("service"))
	r.POST("/vehicle/:slug/service", mark("saveservice"))
	r.POST("/vehicle/:slug/service/delete", mark("deleteservice"))

	// import_handler.AddImportV1
	r.GET("/vehicle/:slug/import", mark("import"))
	r.POST("/vehicle/:slug/import", mark("preview"))
	r.POST("/vehicle/:slug/import/seed", mark("seed"))
	r.POST("/vehicle/:slug/import/confirm", mark("confirm"))

	// user_handler.AddUserV1
	r.GET("/users", mark("users"))
	r.POST("/users", mark("adduser"))
	r.POST("/users/update", mark("updateuser"))

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{http.MethodGet, "/", "home"},
		{http.MethodGet, "/garage", "garage"},
		{http.MethodGet, "/healthz", "health"},
		{http.MethodGet, "/static/app.a1b2c3d4e5.css", "asset"},
		{http.MethodGet, "/login", "login"},
		{http.MethodGet, "/auth/callback", "complete"},
		{http.MethodPost, "/login/code", "code"},

		// The pair this test exists for. "/vehicles/new" must not be read as a
		// vehicle called "new" under a slug parameter, and a vehicle whose slug
		// happens to be "vehicles" must not shadow anything.
		{http.MethodGet, "/vehicles/new", "newcar"},
		{http.MethodPost, "/vehicles/new", "createcar"},
		{http.MethodPost, "/vehicles/default", "setdefault"},
		{http.MethodGet, "/vehicle/the-wagon", "overview"},
		{http.MethodGet, "/vehicle/the-wagon/fuel", "fuel"},
		{http.MethodPost, "/vehicle/the-wagon/fuel/delete", "deletefill"},
		{http.MethodGet, "/vehicle/the-wagon/service", "service"},
		{http.MethodPost, "/vehicle/the-wagon/service/delete", "deleteservice"},
		{http.MethodGet, "/vehicle/the-wagon/insights", "insights"},
		{http.MethodGet, "/vehicle/the-wagon/import", "import"},
		{http.MethodPost, "/vehicle/the-wagon/import/seed", "seed"},
		{http.MethodPost, "/vehicle/the-wagon/import/confirm", "confirm"},
		{http.MethodGet, "/vehicle/the-wagon/settings", "settings"},
		{http.MethodPost, "/vehicle/the-wagon/share/remove", "unshare"},

		// "/users" beside "/users/update", the same shape one level up.
		{http.MethodGet, "/users", "users"},
		{http.MethodPost, "/users/update", "updateuser"},
	}

	for _, one := range cases {
		hit = ""

		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, httptest.NewRequest(one.method, one.path, nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("%s %s answered %d, want 200", one.method, one.path, recorder.Code)
			continue
		}

		if hit != one.want {
			t.Errorf("%s %s reached %q, want %q", one.method, one.path, hit, one.want)
		}
	}
}

// Gin's own ClientIP must not be treated as an identity here. Behind Cloud Run
// it is Google's front end, the same address for every visitor on earth, so
// anything that rate limited on it would rate limit everybody together.
func TestNoProxyIsTrustedForGinsOwnClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := New()

	var seen string
	r.GET("/", func(c *gin.Context) {
		seen = c.ClientIP()
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.1:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")

	r.ServeHTTP(httptest.NewRecorder(), request)

	if seen != "10.0.0.1" {
		t.Errorf("c.ClientIP() = %q, want the address we are actually connected to", seen)
	}
}
