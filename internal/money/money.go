// Package money is the one place that decides what a dollar amount is.
//
// Amounts are held as whole cents in an int64 and never as a float. The sheet
// this replaces adds up four years of fuel and a decade of invoices, and a
// float would make those sums disagree with each other by a penny at
// unpredictable moments - so the running total on the fuel log and the total at
// the bottom of it would differ, which is the sort of thing that makes somebody
// re-check the whole history by hand.
//
// The one number that is deliberately not held this way is a price a gallon
// worked out over a long span. That is an average rather than an amount, it is
// carried as a float, and it only ever reaches a page through a formatter.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse reads an amount the way a person types one: "129.57", "$129.57",
// "1,299", "$1,299.00", "" (nothing). Anything that is not a number comes back
// as an error rather than as zero, because a price silently read as free is
// worse than a form that says it could not read what was typed.
func Parse(typed string) (cents int64, err error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return 0, nil
	}

	// The symbol and the thousands separators are decoration a person types
	// without thinking about it, so they come off before anything is read.
	stripped := strings.NewReplacer("$", "", ",", "", " ", "").Replace(typed)

	negative := false
	if strings.HasPrefix(stripped, "-") {
		negative, stripped = true, stripped[1:]
	}

	whole, fraction, hasFraction := strings.Cut(stripped, ".")

	// Nothing left but decoration - a lone "$", a stray comma - is not the same
	// as an empty field. The field said something; it just did not say a number.
	if whole == "" && fraction == "" {
		return 0, fmt.Errorf("%q is not an amount", typed)
	}

	if !digitsOnly(whole) || !digitsOnly(fraction) {
		return 0, fmt.Errorf("%q is not an amount", typed)
	}

	if whole == "" {
		whole = "0"
	}

	dollars, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an amount", typed)
	}

	cents = dollars * 100

	if hasFraction && fraction != "" {
		// Two digits is the whole of a cent. More than that is a price copied
		// out of somewhere that carries more precision than money does, and the
		// extra digits are dropped rather than refused.
		switch {
		case len(fraction) == 1:
			fraction += "0"
		case len(fraction) > 2:
			fraction = fraction[:2]
		}

		part, parseErr := strconv.ParseInt(fraction, 10, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("%q is not an amount", typed)
		}

		cents += part
	}

	if negative {
		cents = -cents
	}

	return cents, nil
}

// digitsOnly reports whether every character is a digit. An empty string
// passes: "12." and ".75" are both things a person types, and the caller has
// already refused the case where both halves are empty.
func digitsOnly(text string) bool {
	for _, character := range text {
		if character < '0' || character > '9' {
			return false
		}
	}

	return true
}

// Format writes an amount as "$129.57". Zero is written out rather than left
// blank: on a page of prices, "$0.00" and an empty cell mean different things.
func Format(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}

	dollars, remainder := cents/100, cents%100

	return fmt.Sprintf("%s$%s.%02d", sign, withCommas(dollars), remainder)
}

// Blank is Format, except that nothing is written for nothing. It is for the
// cells that are genuinely empty - a price nobody has set yet - where "$0.00"
// would read as a decision that has been made.
func Blank(cents int64) string {
	if cents == 0 {
		return ""
	}

	return Format(cents)
}

// Input writes an amount the way a text field should be pre-filled: the digits
// with no currency symbol, and empty for nothing, so a form that is saved
// untouched saves back exactly what it was given.
func Input(cents int64) string {
	if cents == 0 {
		return ""
	}

	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}

	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// withCommas groups a whole number of dollars in threes.
func withCommas(dollars int64) string {
	digits := strconv.FormatInt(dollars, 10)
	if len(digits) <= 3 {
		return digits
	}

	var out strings.Builder
	lead := len(digits) % 3
	if lead > 0 {
		out.WriteString(digits[:lead])
	}

	for at := lead; at < len(digits); at += 3 {
		if out.Len() > 0 {
			out.WriteByte(',')
		}
		out.WriteString(digits[at : at+3])
	}

	return out.String()
}
