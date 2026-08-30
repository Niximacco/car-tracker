// Package shortcode generates short, url-safe ids.
//
// Nothing on this site needs one yet: every entity here is keyed by something
// meaningful - a vehicle by its name, a fill-up by its day and its odometer
// reading - and that is deliberate, because it is what makes an import safe to
// run twice. This is carried across from link-shortener-backend by way of
// wild-games so that the first thing here that does need an opaque id has one
// that is already tested, rather than one written in a hurry.
package shortcode

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// letters leaves out 0, O, I and L. These ids get read off one screen and typed
// into another, and those are the pairs that get mistaken.
var letters = []rune("ABCDEFGHJKMNPQRSTUVWXYZ23456789")

const default_len = 6

type ShortCode struct {
	Len int
}

type Option func(f *ShortCode)

func Len(length int) Option {
	return func(f *ShortCode) {
		f.Len = length
	}
}

// New returns a random code. It draws from crypto/rand: these ids are not
// secrets - deciding a request needs an admin session whatever its id is - but
// math/rand's global source is shared process-wide, and there is no reason to
// make an id's value predictable from anything else that happens to be drawing
// from it.
func New(opts ...Option) string {
	short := &ShortCode{Len: default_len}
	for _, opt := range opts {
		opt(short)
	}

	if short.Len < 1 {
		short.Len = default_len
	}

	limit := big.NewInt(int64(len(letters)))

	b := make([]rune, short.Len)
	for i := range b {
		// rand.Int only fails if the system entropy source is broken, at which
		// point there is nothing sensible to fall back to.
		pick, err := rand.Int(rand.Reader, limit)
		if err != nil {
			panic("shortcode: no entropy available: " + err.Error())
		}
		b[i] = letters[pick.Int64()]
	}

	return string(b)
}

const max_len = 64

// Valid reports whether a code can be used. Codes arrive as a single path
// segment, so anything that would change the shape of the url - a slash, a
// query, a space - is out.
func Valid(short string) bool {
	if len(short) == 0 || len(short) > max_len {
		return false
	}

	for _, character := range short {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-' || character == '_':
		default:
			return false
		}
	}

	return true
}

// Normalize puts a code into the case it is stored in, so a code that arrives
// hand-typed still resolves.
func Normalize(short string) string {
	return strings.ToUpper(strings.TrimSpace(short))
}
