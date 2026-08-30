package auth

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	data "github.com/Niximacco/car-tracker/internal/cloud"
	"github.com/Niximacco/car-tracker/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var (
	JWT_SIGNING_KEY []byte
	AUDIENCE        = os.Getenv("DATASTORE_NAMESPACE")

	COOKIE_NAME   = "ct_session"
	COOKIE_DOMAIN = ""
	COOKIE_SECURE = true
)

var (
	errorNoAuthHeader     = errors.New("no authorization header content present")
	errorAuthHeaderFormat = errors.New("authorization header format incorrect, should be 'bearer <token>`")
	errorNoCredentials    = errors.New("not signed in")
	errorInvalidToken     = errors.New("invalid token")
)

const (
	// SESSION_VALID_TIME is how long a session survives without the user coming
	// back. Every visit re-issues the token, so an active user never expires.
	SESSION_VALID_TIME = 30 * 24 * time.Hour
	// REFRESH_AFTER is how old a token has to be before a visit re-issues it.
	// It keeps us from writing a Set-Cookie header on every single request.
	REFRESH_AFTER = 1 * time.Hour
	ISSUER        = "car-tracker-api"

	// contextEmailKey holds the authenticated email address on the gin context.
	contextEmailKey = "auth_email"
	// contextUserKey holds the loaded user entity on the gin context.
	contextUserKey = "auth_user"
	// contextConfirmedKey records whether that user really came out of
	// datastore, or is the placeholder authenticate falls back to.
	contextConfirmedKey = "auth_confirmed"
)

func init() {
	log.Print("Initializing Authentication")
	signingKey := os.Getenv("JWT_SIGNING_KEY")
	if signingKey == "" {
		log.Fatal("No Signing Key Present.")
	}

	JWT_SIGNING_KEY = []byte(signingKey)

	if name := os.Getenv("SESSION_COOKIE_NAME"); name != "" {
		COOKIE_NAME = name
	}

	// Left empty the cookie is host-only, which is what we want when the API and
	// the pages share a hostname. Set it to a parent domain (".example.com") only
	// if the session has to be readable from a sibling subdomain.
	COOKIE_DOMAIN = os.Getenv("COOKIE_DOMAIN")

	// Secure cookies are the default; plain http local development needs this off.
	if strings.EqualFold(os.Getenv("COOKIE_SECURE"), "false") {
		log.Print("WARNING: COOKIE_SECURE=false, session cookies will be sent over plain http")
		COOKIE_SECURE = false
	}

	log.Print("done")
}

type Claims struct {
	// Username holds the user's email address. The name is kept for tokens that
	// were issued before magic-link login existed.
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func New(username string) (tokenString string, err error) {
	expirationTime := time.Now().Add(SESSION_VALID_TIME)
	claims := Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    ISSUER,
			Subject:   username,
			Audience:  []string{AUDIENCE},
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			NotBefore: jwt.NewNumericDate(time.Now()),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)

	tokenString, err = token.SignedString(JWT_SIGNING_KEY)

	return

}

func parseHeader(header string) (token string, err error) {
	if header == "" {
		return "", errorNoAuthHeader
	}

	// Tokens will be of format "bearer <token>", split on ' ' space
	content := strings.Split(header, " ")
	if len(content) != 2 {
		return "", errorAuthHeaderFormat
	}

	token = content[1]
	return
}

// parse validates a raw token string and returns its claims. Anything that
// fails validation - bad signature, wrong algorithm, expired, or issued for
// somebody else's deployment - comes back as an error.
func parse(tokenString string) (claims *Claims, err error) {
	claims = &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return JWT_SIGNING_KEY, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS512.Alg()}))

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errorInvalidToken
	}

	if claims.Issuer != ISSUER {
		return nil, errorInvalidToken
	}

	if !hasAudience(claims.Audience, AUDIENCE) {
		return nil, errorInvalidToken
	}

	if claims.Username == "" {
		return nil, errorInvalidToken
	}

	return claims, nil
}

func hasAudience(audiences jwt.ClaimStrings, want string) bool {
	for _, audience := range audiences {
		if audience == want {
			return true
		}
	}

	return false
}

// ParseToken validates an "Authorization: bearer <token>" header and returns the
// email address it was issued to.
func ParseToken(header string) (username string, err error) {
	tokenString, err := parseHeader(header)
	if err != nil {
		return "", err
	}

	claims, err := parse(tokenString)
	if err != nil {
		return "", err
	}

	return claims.Username, nil
}

// SetSessionCookie writes the session token as an http-only cookie.
func SetSessionCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(COOKIE_NAME, token, int(SESSION_VALID_TIME.Seconds()), "/", COOKIE_DOMAIN, COOKIE_SECURE, true)
}

// ClearSessionCookie expires the session cookie in the browser.
func ClearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(COOKIE_NAME, "", -1, "/", COOKIE_DOMAIN, COOKIE_SECURE, true)
}

// StartSession issues a fresh token for an email address and sets it as a cookie.
func StartSession(c *gin.Context, email string) error {
	token, err := New(email)
	if err != nil {
		return err
	}

	SetSessionCookie(c, token)
	return nil
}

// resolve pulls a session out of the request: the cookie first, then a bearer
// header so existing API clients keep working. A cookie-backed session that is
// older than REFRESH_AFTER is re-issued, which is what keeps a regular visitor
// signed in indefinitely.
func resolve(c *gin.Context) (email string, err error) {
	if cookie, cookieErr := c.Cookie(COOKIE_NAME); cookieErr == nil && cookie != "" {
		claims, parseErr := parse(cookie)
		if parseErr != nil {
			// A cookie we can't validate is worse than no cookie: drop it so the
			// browser stops sending it and the user gets a clean login.
			ClearSessionCookie(c)
			return "", parseErr
		}

		if claims.IssuedAt != nil && time.Since(claims.IssuedAt.Time) > REFRESH_AFTER {
			if refreshed, refreshErr := New(claims.Username); refreshErr == nil {
				SetSessionCookie(c, refreshed)
			}
		}

		return claims.Username, nil
	}

	if header := c.GetHeader("Authorization"); header != "" {
		return ParseToken(header)
	}

	return "", errorNoCredentials
}

// authenticate resolves the session and confirms the account behind it is still
// allowed in. Access revoked in datastore therefore takes effect on the next
// request, rather than whenever a 30 day cookie happens to expire.
//
// The user is put on the context so the rest of the request can ask about
// admin rights without paying for a second lookup.
func authenticate(c *gin.Context) (email string, err error) {
	email, err = resolve(c)
	if err != nil {
		return "", err
	}

	confirmed := true

	user, err := data.GetUser(email)
	switch {
	case err == nil:

	case errors.Is(err, data.UserNotFoundErr), errors.Is(err, data.UserDisabledErr):
		// The account was removed or disabled while this session was alive.
		ClearSessionCookie(c)
		return "", err

	default:
		// Datastore having a bad moment must not sign everybody out. Only an
		// explicit revocation closes the door; anything else fails open.
		//
		// Staying signed in is what fails open, though, and not what the
		// account is allowed to do. This placeholder says nothing about
		// whether it may change things, so it is marked as unconfirmed and
		// Editor turns writes away until we can read the real answer.
		log.Printf("could not confirm the account behind a session: %s", err.Error())
		user = types.User{Email: email}
		confirmed = false
	}

	c.Set(contextEmailKey, email)
	c.Set(contextUserKey, user)
	c.Set(contextConfirmedKey, confirmed)

	return email, nil
}

// Confirmed reports whether the account behind this request was actually read
// out of datastore rather than assumed.
func Confirmed(c *gin.Context) bool {
	confirmed, ok := c.Get(contextConfirmedKey)
	if !ok {
		return false
	}

	if asBool, ok := confirmed.(bool); ok {
		return asBool
	}

	return false
}

// Optional attaches the signed-in user to the context when there is one, and
// lets the request through either way.
func Optional() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, _ = authenticate(c)
		c.Next()
	}
}

// Required rejects unauthenticated requests with a JSON 401. Use it on the API.
func Required() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := authenticate(c); err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		c.Next()
	}
}

// RequiredPage sends unauthenticated browsers to the login page, remembering
// where they were headed. Use it on html routes.
func RequiredPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := authenticate(c); err != nil {
			c.Redirect(http.StatusFound, "/login?next="+url.QueryEscape(c.Request.URL.RequestURI()))
			c.Abort()
			return
		}

		c.Next()
	}
}

// Editor rejects a view-only account with a JSON 403. It runs after Required,
// so by this point there is a valid session; this decides what that session is
// allowed to do.
//
// There is no page-level equivalent, because there is no page here a view-only
// account may not look at. Every screen on this site is a car's history in one
// form or another, and the difference between an account that may edit and one
// that may not is entirely in what the buttons on it do. So the routes that
// render html are open to everybody signed in, and every route that writes goes
// through this.
//
// Whether the account may see the particular car being written to is a separate
// question, and one this cannot answer: it is on the vehicle rather than on the
// user. The handlers do that, because they are the only thing that has loaded
// the vehicle by then.
//
// Like admin, the flag is read from datastore on every request rather than
// carried in the token, so taking somebody's editing away takes effect on their
// next click instead of whenever a thirty day cookie happens to expire.
func Editor() gin.HandlerFunc {
	return func(c *gin.Context) {
		if IsViewOnly(c) {
			log.Printf("refused a change from a view-only account")
			c.AbortWithStatusJSON(http.StatusForbidden,
				gin.H{"error": "this account can read the schedule and nothing else"})
			return
		}

		// The account could not be read, so what it may do is unknown. A write
		// would almost certainly fail at datastore a moment later anyway, so
		// refusing it here costs nothing and closes the window where a
		// view-only account looks like an ordinary one.
		if !Confirmed(c) {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable,
				gin.H{"error": "couldn't check what this account is allowed to do - try again in a moment"})
			return
		}

		c.Next()
	}
}

// Admin rejects anyone who is not an admin with a JSON 403. Managing who else
// is allowed to sign in goes through it, and so does reaching a car nobody
// shared with you.
//
// Admin status is read from datastore on every request rather than carried in
// the token, so revoking it takes effect immediately instead of waiting out a
// thirty day cookie.
func Admin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsAdmin(c) {
			log.Printf("refused an admin-only request from a non-admin")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "that's an admin-only action"})
			return
		}

		c.Next()
	}
}

// IsViewOnly reports whether the request came from an account that may only
// read. It reads the user that authenticate already loaded, so asking is free.
func IsViewOnly(c *gin.Context) bool {
	return !User(c).CanEdit()
}

// Email returns the authenticated email address for the request, or "" when the
// request is anonymous.
func Email(c *gin.Context) string {
	email, ok := c.Get(contextEmailKey)
	if !ok {
		return ""
	}

	if asString, ok := email.(string); ok {
		return asString
	}

	return ""
}

// User returns the account behind the request, or a zero user when the request
// is anonymous.
func User(c *gin.Context) types.User {
	user, ok := c.Get(contextUserKey)
	if !ok {
		return types.User{}
	}

	if asUser, ok := user.(types.User); ok {
		return asUser
	}

	return types.User{}
}

// IsAdmin reports whether the request came from an admin. It reads the user
// that authenticate already loaded, so asking is free.
func IsAdmin(c *gin.Context) bool {
	return User(c).Admin
}

// IsSignedIn reports whether the request carried a valid session.
func IsSignedIn(c *gin.Context) bool {
	return Email(c) != ""
}
