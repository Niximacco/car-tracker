// Package slug turns human text into the keys we store things under.
//
// Here that is one thing: a vehicle's name into the id it lives at and the
// address it is read at. "The wagon" becomes "the-wagon", once, when the car is
// created - and never again, because links to it are worth keeping and a
// renamed car is the same car.
//
// Carried over from niximacco-recipes by way of wild-games, trimmed to the
// url-safe fold, which is all this site needs.
package slug

import (
	"strings"
	"unicode"
)

// MaxLength caps a slug so it stays a comfortable url and a legal datastore key
// name. Datastore allows far more; this is about readability.
const MaxLength = 80

// Make turns text into a url-safe slug: "Dallas Stars" becomes "dallas-stars".
// Accented letters are folded to their base letter, everything else that is not
// a letter or digit becomes a separator, and runs of separators collapse.
//
// Make can return "", for text with no letters or digits in it at all. The
// caller has to decide what to do about that rather than storing an empty key.
func Make(text string) string {
	var builder strings.Builder
	lastWasDash := true // leading dashes are never wanted

	for _, character := range strings.ToLower(strings.TrimSpace(text)) {
		switch {
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			builder.WriteRune(fold(character))
			lastWasDash = false

		default:
			if !lastWasDash {
				builder.WriteRune('-')
				lastWasDash = true
			}
		}

		if builder.Len() >= MaxLength {
			break
		}
	}

	return strings.Trim(builder.String(), "-")
}

// fold maps the accented latin letters onto plain ascii. Anything not in the
// table is left alone: a slug is allowed to hold non-ascii letters, it just
// must not hold punctuation or spaces.
func fold(character rune) rune {
	switch character {
	case 'á', 'à', 'â', 'ä', 'ã', 'å':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'ö', 'õ':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ñ':
		return 'n'
	case 'ç':
		return 'c'
	}

	return character
}
