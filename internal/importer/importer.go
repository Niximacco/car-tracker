// Package importer reads the spreadsheet this site replaces.
//
// It takes whatever comes out of "File, Download, Comma separated values" -
// or a straight copy and paste out of the browser, which arrives tab separated
// - and turns it into fill-ups and shop visits. Both sheets go through the same
// function, because the form behind them wrote both kinds of row and the two
// exports differ only in which columns are filled in.
//
// Nothing here refuses a batch. A line it cannot read comes back as a line it
// could not read, with the reason and the original text, and everything else is
// still offered. Four years of history with three odd rows in it should import
// four years of history and show you the three rows - not stop at the first one
// and make you find it.
package importer

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Niximacco/car-tracker/internal/money"
	"github.com/Niximacco/car-tracker/internal/types"
)

// The two things a row can turn out to be.
const (
	KindFillup  = "fillup"
	KindService = "service"
)

// Row is one line of the paste, read as far as it could be read.
type Row struct {
	// Line is the line number in what was pasted, so a problem can be pointed
	// at rather than described.
	Line int
	// Raw is the original text, shown beside a problem so it is obvious which
	// line is meant without counting.
	Raw string

	Kind    string
	Fillup  types.Fillup
	Service types.Service

	// Problem is why this row could not be read, or empty. A row with a problem
	// is never imported.
	Problem string
	// Odd is a row that read perfectly and is still worth a second look before
	// it is believed - a reading that goes backwards, a tank that is too big to
	// be a tank. It is imported; it is just flagged first.
	Odd string
}

// Readable reports whether this row would be imported.
func (r Row) Readable() bool {
	return r.Problem == ""
}

// Preview is a whole paste, read but not written. The import screen shows one
// of these and asks before anything is saved, because an import that lands
// before you have looked at it is an import you have to undo by hand.
type Preview struct {
	Rows []Row

	Fillups  int
	Services int
	Bad      int
	Odd      int
}

// Any reports whether there is anything worth importing.
func (p Preview) Any() bool {
	return p.Fillups+p.Services > 0
}

// The columns this understands, and everything the sheet ever called them.
// Anything not on the list is ignored rather than refused: the sheet carries
// six columns of arithmetic - the difference, the mpg, the running cost - that
// this site works out for itself and must not import, and listing them as
// errors would make every single row look broken.
var columns = map[string]string{
	"timestamp": "date",
	"date":      "date",
	"day":       "date",

	"logtype": "kind",
	"type":    "kind",

	// The sheet's one column is both at once, so it becomes the place - which
	// is what it almost always holds, and what the station breakdown groups on.
	"notelocation": "where",
	"location":     "where",
	"station":      "where",
	"shop":         "where",
	"place":        "where",
	// A sheet that keeps them apart gets to keep them apart.
	"note":     "note",
	"comment":  "note",
	"comments": "note",

	"maintenancedone": "work",
	"work":            "work",
	"service":         "work",
	"maintenance":     "work",

	"cost":   "cost",
	"total":  "cost",
	"amount": "cost",
	"paid":   "cost",

	"odometer": "odometer",
	"odo":      "odometer",
	"mileage":  "odometer",
	"miles":    "odometer",

	"gallonspurchased": "gallons",
	"gallons":          "gallons",
	"gal":              "gallons",
	"volume":           "gallons",

	"pricepergallon": "price",
	"pricegallon":    "price",
	"price":          "price",
	"ppg":            "price",
	"unitprice":      "price",

	"fuelsaver": "saver",
	"saver":     "saver",
	"discount":  "discount",
	"rewards":   "saver",
}

// Parse reads a pasted export into rows, for one vehicle.
//
// The first line has to be the header. Every column is found by name rather
// than by position, because the two sheets have different columns in different
// orders and because a positional reader is a reader that silently imports the
// odometer into the gallons the day somebody adds a column.
func Parse(text string, vehicle types.Vehicle, tank float64) (Preview, error) {
	preview := Preview{Rows: []Row{}}

	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return preview, fmt.Errorf("there is nothing to import")
	}

	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = separator(text)
	reader.FieldsPerRecord = -1
	// The work column has line breaks in it and the odd unbalanced inch mark.
	// Neither is a reason to refuse an invoice.
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		return preview, fmt.Errorf("could not read the first line: %s", err.Error())
	}

	positions := headerPositions(header)
	if len(positions) == 0 {
		return preview, fmt.Errorf("the first line does not look like column headings - it should have Date, Odometer and either Gallons or Maintenance Done on it")
	}

	if _, dated := positions["date"]; !dated {
		return preview, fmt.Errorf("there is no date column - the first line needs one headed Date or Timestamp")
	}

	if _, read := positions["odometer"]; !read {
		return preview, fmt.Errorf("there is no odometer column - every entry needs a reading to be worth anything")
	}

	// The line number the reader is on. csv counts records rather than lines
	// and a quoted cell can span several, so this is tracked rather than asked
	// for: a row pointed at the wrong line is worse than a row pointed at no
	// line at all.
	line := 1

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}

		line++

		if err != nil {
			preview.Rows = append(preview.Rows, Row{
				Line:    line,
				Raw:     strings.Join(record, " "),
				Problem: err.Error(),
			})
			preview.Bad++
			continue
		}

		row := read(record, positions, vehicle, tank)
		if row.Empty() {
			continue
		}

		row.Line = line
		row.Raw = strings.TrimSpace(strings.Join(record, "  "))

		switch {
		case !row.Readable():
			preview.Bad++

		case row.Kind == KindFillup:
			preview.Fillups++

		case row.Kind == KindService:
			preview.Services++
		}

		if row.Odd != "" {
			preview.Odd++
		}

		preview.Rows = append(preview.Rows, row)
	}

	flagBackwards(&preview)

	return preview, nil
}

// Empty reports whether a row had nothing on it at all. The sheet ends in a
// hundred blank rows and every one of them would otherwise be an error.
func (r Row) Empty() bool {
	return r.Kind == "" && r.Problem == ""
}

// Fillups is everything readable that was a fill-up.
func (p Preview) FillupsToImport() []types.Fillup {
	out := []types.Fillup{}

	for _, row := range p.Rows {
		if row.Readable() && row.Kind == KindFillup {
			out = append(out, row.Fillup)
		}
	}

	return out
}

// ServicesToImport is everything readable that was a shop visit.
func (p Preview) ServicesToImport() []types.Service {
	out := []types.Service{}

	for _, row := range p.Rows {
		if row.Readable() && row.Kind == KindService {
			out = append(out, row.Service)
		}
	}

	return out
}

// read turns one record into a row.
func read(record []string, positions map[string]int, vehicle types.Vehicle, tank float64) Row {
	row := Row{}

	cell := func(name string) string {
		at, found := positions[name]
		if !found || at >= len(record) {
			return ""
		}

		return strings.TrimSpace(record[at])
	}

	// A line with nothing on it is not a problem, it is the end of the sheet.
	blank := true
	for _, field := range record {
		if strings.TrimSpace(field) != "" {
			blank = false
			break
		}
	}

	if blank {
		return row
	}

	date, clock, dateErr := ParseWhen(cell("date"))
	gallons, gallonsErr := ParseNumber(cell("gallons"))
	work := strings.TrimSpace(cell("work"))
	cost, costErr := money.Parse(cell("cost"))

	// What kind of row this is, decided from what is on it rather than from the
	// log type column - which the form stopped filling in three years ago and
	// which the maintenance sheet does not have at all. Gallons is the tell: a
	// row with fuel on it is a fill-up and a row with work on it is a visit.
	switch {
	case gallonsErr == nil && gallons > 0:
		row.Kind = KindFillup

	case work != "" || cost > 0:
		row.Kind = KindService

	case strings.Contains(strings.ToLower(cell("kind")), "fill"):
		row.Kind = KindFillup

	default:
		row.Problem = "there is no fuel and no work on this line, so there is nothing to record"
		return row
	}

	if dateErr != nil {
		row.Problem = dateErr.Error()
		return row
	}

	odometer, odometerErr := ParseInt(cell("odometer"))
	if odometerErr != nil {
		row.Problem = fmt.Sprintf("could not read the odometer: %s", odometerErr.Error())
		return row
	}

	if odometer <= 0 {
		row.Problem = "there is no odometer reading on this line"
		return row
	}

	if costErr != nil {
		row.Problem = fmt.Sprintf("could not read the cost: %s", costErr.Error())
		return row
	}

	where := strings.TrimSpace(cell("where"))
	note := strings.TrimSpace(cell("note"))
	on := date.Format(types.DateLayout)

	if row.Kind == KindService {
		row.Service = types.Service{
			ID:        types.ServiceID(on, odometer),
			Vehicle:   vehicle.Slug,
			On:        on,
			Odometer:  odometer,
			Shop:      where,
			Work:      work,
			CostCents: cost,
			Note:      note,
		}

		if work == "" {
			row.Odd = "there is a cost but nothing saying what was done"
		}

		return row
	}

	if gallonsErr != nil {
		row.Problem = fmt.Sprintf("could not read the gallons: %s", gallonsErr.Error())
		return row
	}

	price, priceErr := money.Parse(cell("price"))
	if priceErr != nil {
		row.Problem = fmt.Sprintf("could not read the price a gallon: %s", priceErr.Error())
		return row
	}

	saver, saverErr := money.Parse(cell("saver"))
	if saverErr != nil {
		row.Problem = fmt.Sprintf("could not read the fuel saver: %s", saverErr.Error())
		return row
	}

	if price <= 0 && cost <= 0 {
		row.Problem = "there is no price a gallon and no total, so this fill-up cost nothing"
		return row
	}

	// A row with a total and no unit price still has a unit price; it is just
	// on the other side of the division.
	if price <= 0 && gallons > 0 {
		price = types.Round(float64(cost) / gallons)
	}

	row.Fillup = types.Fillup{
		ID:                  types.FillupID(on, odometer),
		Vehicle:             vehicle.Slug,
		On:                  on,
		At:                  clock,
		Odometer:            odometer,
		Gallons:             gallons,
		PricePerGallonCents: price,
		FuelSaverCents:      saver,
		Station:             where,
		Note:                note,
	}

	// The one row in four years that needs it: a total that is not the gallons
	// times the price is a real receipt, and it is kept rather than corrected.
	if cost > 0 && abs(cost-row.Fillup.Cost()) > 2 {
		row.Fillup.CostCents = cost
	}

	// A tank bigger than the tank is the classic import mistake - a decimal
	// point in the wrong place, or the price and the gallons columns swapped,
	// which is a thing that has happened in this very sheet. It is flagged
	// rather than refused, because occasionally somebody really did fill a jerry
	// can at the same time.
	if tank > 0 && gallons > tank*1.15 {
		row.Odd = fmt.Sprintf("%.3f gallons is more than this vehicle's tank holds", gallons)
	}

	if saver > 0 && saver > price*3 {
		row.Odd = "the fuel saver is far larger than the price paid - are those two columns the right way round?"
	}

	return row
}

// flagBackwards marks any fill-up whose odometer is lower than the one before
// it. It is done over the whole preview rather than row by row because a row
// on its own has no idea what came before it.
func flagBackwards(preview *Preview) {
	previous := 0

	for at := range preview.Rows {
		row := &preview.Rows[at]
		if !row.Readable() || row.Kind != KindFillup {
			continue
		}

		if previous > 0 && row.Fillup.Odometer < previous {
			if row.Odd == "" {
				row.Odd = fmt.Sprintf("the odometer goes backwards here, from %d to %d", previous, row.Fillup.Odometer)
				preview.Odd++
			}
		}

		previous = row.Fillup.Odometer
	}
}

// separator decides whether this is a csv or a paste out of a browser, which is
// tabs. It reads the first line only, and prefers tabs when there are any: a
// pasted cell can easily contain a comma, and a downloaded csv never contains a
// raw tab.
func separator(text string) rune {
	line, _, _ := strings.Cut(text, "\n")

	if strings.Contains(line, "\t") {
		return '\t'
	}

	return ','
}

// headerPositions maps each column this understands to where it is.
func headerPositions(header []string) map[string]int {
	positions := map[string]int{}

	for at, name := range header {
		column, known := columns[fold(name)]
		if !known {
			continue
		}

		// First one wins. The sheet has both a "Cost" and a "Running Cost", and
		// the second is arithmetic this site does itself.
		if _, taken := positions[column]; !taken {
			positions[column] = at
		}
	}

	// "Discount" is only ever the fuel saver by another name, and is mapped
	// separately so that a sheet using both words does not have them fight.
	if at, found := positions["discount"]; found {
		if _, taken := positions["saver"]; !taken {
			positions["saver"] = at
		}

		delete(positions, "discount")
	}

	return positions
}

// fold reduces a column heading to something comparable: lower case, with the
// spaces, slashes and punctuation taken out. "Note / Location", "note/location"
// and "Note/Location" are one heading.
func fold(name string) string {
	var out strings.Builder

	for _, character := range strings.ToLower(name) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			out.WriteRune(character)
		}
	}

	return out.String()
}

// The date formats the sheet and the form between them have produced. The
// two-digit year ones are last, because "1/2/24" is genuinely ambiguous with
// nothing and Go's parser takes the first layout that fits.
var layouts = []string{
	"1/2/2006 15:04:05",
	"1/2/2006 15:04",
	"1/2/2006",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"Jan 2, 2006",
	"January 2, 2006",
	"2 Jan 2006",
	"1/2/06 15:04:05",
	"1/2/06",
}

// ParseWhen reads a date, and the time of day if there is one on it.
//
// The time is kept because the form's timestamp carried it and it is worth
// something - a fill logged at seven in the morning is a commute, one logged at
// nine at night is the end of a drive - but nothing is calculated from it, so a
// date with no time on it is not a problem.
func ParseWhen(typed string) (date time.Time, clock string, err error) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return time.Time{}, "", fmt.Errorf("there is no date on this line")
	}

	for _, layout := range layouts {
		parsed, parseErr := time.ParseInLocation(layout, typed, types.Local)
		if parseErr != nil {
			continue
		}

		clock = ""
		if strings.Contains(layout, "15:04") && !(parsed.Hour() == 0 && parsed.Minute() == 0) {
			clock = parsed.Format("15:04")
		}

		return parsed, clock, nil
	}

	return time.Time{}, "", fmt.Errorf("%q is not a date this understands", typed)
}

// ParseNumber reads a decimal the way a person or a spreadsheet writes one.
// Empty is nought rather than an error: a blank gallons cell is what makes a
// row a shop visit.
func ParseNumber(typed string) (float64, error) {
	typed = strings.TrimSpace(strings.ReplaceAll(typed, ",", ""))
	if typed == "" {
		return 0, nil
	}

	value, err := strconv.ParseFloat(typed, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", typed)
	}

	return value, nil
}

// ParseInt reads a whole number, tolerating the thousands separators a
// spreadsheet puts in an odometer reading and the decimal point it occasionally
// puts on the end of one.
func ParseInt(typed string) (int, error) {
	value, err := ParseNumber(typed)
	if err != nil {
		return 0, err
	}

	return int(value), nil
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}

	return value
}
