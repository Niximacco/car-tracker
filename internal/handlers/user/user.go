// Package user_handler is the allow list: who may sign in, and what they may do
// once they have.
//
// It is the whole of authorization on this site. auth.ajn.me can say somebody
// proved they can read an address; it holds no user list and has no opinion
// about whether that address gets a session here. This is where that opinion
// lives.
package user_handler

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/Niximacco/car-tracker/internal/access"
	"github.com/Niximacco/car-tracker/internal/auth"
	data "github.com/Niximacco/car-tracker/internal/cloud"
	"github.com/Niximacco/car-tracker/internal/web"
	"github.com/gin-gonic/gin"
)

func AddUserV1(router *gin.Engine) {
	router.GET("/users", auth.RequiredPage(), admins(), List)
	router.POST("/users", auth.RequiredPage(), admins(), Create)
	router.POST("/users/update", auth.RequiredPage(), admins(), Update)
}

// admins is the page-shaped version of auth.Admin: the same check, rendering a
// page rather than a line of json, because everything under /users is a screen.
func admins() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.IsAdmin(c) {
			access.Refused(c, "That's an admin-only page.")
			return
		}

		c.Next()
	}
}

func List(c *gin.Context) {
	page, ok := load(c)
	if !ok {
		return
	}

	web.Render(c, http.StatusOK, web.UsersPage, page)
}

func Create(c *gin.Context) {
	address := data.NormalizeEmail(c.PostForm("email"))
	name := strings.TrimSpace(c.PostForm("name"))
	admin, viewOnly, _ := role(c.PostForm("role"))

	if !data.ValidAddress(address) {
		fail(c, "That doesn't look like an email address.", http.StatusBadRequest)
		return
	}

	_, err := data.NewUser(address, name, admin, viewOnly)
	switch {
	case err == nil:
		log.Printf("added %s to the allow list", address)

	case errors.Is(err, data.AlreadyExistsErr):
		fail(c, "That address already has an account. Change what it can do on the row above.", http.StatusConflict)
		return

	default:
		log.Printf("could not add a user: %s", err.Error())
		access.Failed(c, "We couldn't add that account. Please try again.")
		return
	}

	access.Back(c, "/users", "added")
}

// Update changes what one account may do.
//
// The four roles are one dropdown rather than four checkboxes because they are
// genuinely exclusive: an admin who is also read-only is a state nobody means
// and every combination of boxes would have to answer for it.
func Update(c *gin.Context) {
	address := data.NormalizeEmail(c.PostForm("email"))
	admin, viewOnly, disabled := role(c.PostForm("role"))

	// An admin who takes their own admin away has locked themselves out of the
	// page that would put it back, and the way back in is an environment
	// variable and a redeploy. It is refused rather than confirmed, because
	// there is no reason to do it that "sign in as the other admin" does not
	// serve better.
	if strings.EqualFold(address, auth.Email(c)) && (!admin || disabled) {
		fail(c, "You cannot take your own access away from this page. Ask another admin.", http.StatusBadRequest)
		return
	}

	if _, err := data.UpdateUser(address, nil, &admin, &viewOnly, &disabled); err != nil {
		log.Printf("could not update %s: %s", address, err.Error())
		access.Failed(c, "We couldn't change that account. Please try again.")
		return
	}

	log.Printf("changed what %s can do", address)
	access.Back(c, "/users", "saved")
}

// role turns the dropdown into the three flags stored on the account.
func role(chosen string) (admin bool, viewOnly bool, disabled bool) {
	switch chosen {
	case "admin":
		return true, false, false

	case "viewonly":
		return false, true, false

	case "disabled":
		return false, false, true
	}

	return false, false, false
}

// load builds the page, which is the list.
func load(c *gin.Context) (page web.Page, ok bool) {
	users, err := data.ListUsers(data.USER_LIST_LIMIT)
	if err != nil {
		log.Printf("could not list users: %s", err.Error())
		access.Failed(c, "We couldn't read the account list. Please try again.")
		return page, false
	}

	page = access.Page(c, "Who can sign in")
	page.Nav = "users"
	page.Users = users

	return page, true
}

// fail re-renders the list with a message on it, for the mistakes somebody can
// see and fix from the page they are on.
func fail(c *gin.Context, why string, status int) {
	page, ok := load(c)
	if !ok {
		return
	}

	page.Error = why
	web.Render(c, status, web.UsersPage, page)
}
