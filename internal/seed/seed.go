// Package seed is the spreadsheet, compiled into the binary.
//
// Four years of fill-ups and eleven visits to the shop, transcribed out of the
// Google Sheet this site replaces, so that setting the site up for the first
// time is a button rather than an export, a download and a paste. It goes
// through exactly the same importer a paste does - it is two csv files, not a
// list of structs - so the import path is exercised by the thing everybody
// runs first, and there is no second way in for the data to be wrong in.
//
// It is safe to load twice. Every entry's id is its day and its odometer
// reading, so a second load writes over the same entities rather than doubling
// them.
//
// # What was changed on the way in
//
// The transcription is faithful with three exceptions, all of them about
// columns the sheet did not have:
//
// Station names are spelled one way. The sheet has "Speedway crystal" and
// "Crystal speedway", "Hy-Vee new hope" and "Hy-Vee New Hope", which are one
// station each and would otherwise be four rows in the station breakdown.
//
// The note about spilled fuel in February 2022 moved out of the location column
// and into its own, because it is a note and the column it was in is what the
// station breakdown groups on.
//
// The dozen locations that read "Hy-Vee new hope - 1.45 in fuel saver used"
// lost that second half. It is not a note, it is the fuel saver column, and it
// is already in the fuel saver column.
package seed

import (
	"embed"

	"github.com/Niximacco/car-tracker/internal/importer"
	"github.com/Niximacco/car-tracker/internal/types"
)

//go:embed data/*.csv
var files embed.FS

// The two facts the sheet's summary block is measured from, which are not on
// any row of it: the day the car was bought and what the odometer read that
// day. They are recovered from the block itself - it says 27,102 total miles at
// a last reading of 95,176, and 1,705 days owned as of a last fill 18 days
// before - and they are what make every per-day and per-mile figure on the site
// agree with the ones that have been looked at for four years.
const (
	PurchasedOn      = "2021-12-28"
	PurchaseOdometer = 68074
	// TankGallons is what the biggest fill in the history suggests, rounded to
	// the nearest tenth it could plausibly be. It is only used to flag a
	// fill-up that could not have happened, so being a little generous is the
	// right way to be wrong.
	TankGallons = 13.5
)

// Vehicle is what the import screen offers as the car this history belongs to.
//
// It is deliberately half-filled. The dates, the readings and the tank come out
// of the sheet and are worth pre-filling; what the car actually is does not
// appear anywhere in it - "West Side VW" and a DSG service is as much as the
// history says - so the make is offered and the year and model are left for
// somebody who knows to type. The form it lands in is editable before anything
// is written.
func Vehicle(owner string) types.Vehicle {
	return types.Vehicle{
		Slug:             "the-wagon",
		Name:             "The wagon",
		Make:             "Volkswagen",
		Owner:            owner,
		PurchasedOn:      PurchasedOn,
		PurchaseOdometer: PurchaseOdometer,
		TankGallons:      TankGallons,
	}
}

// Load reads the embedded history for a vehicle. It returns a preview rather
// than writing anything, so the same confirm screen a paste goes through is the
// one this goes through.
func Load(vehicle types.Vehicle) (importer.Preview, error) {
	combined := importer.Preview{Rows: []importer.Row{}}

	for _, name := range []string{"data/fillups.csv", "data/services.csv"} {
		body, err := files.ReadFile(name)
		if err != nil {
			return combined, err
		}

		preview, err := importer.Parse(string(body), vehicle, vehicle.TankGallons)
		if err != nil {
			return combined, err
		}

		combined.Rows = append(combined.Rows, preview.Rows...)
		combined.Fillups += preview.Fillups
		combined.Services += preview.Services
		combined.Bad += preview.Bad
		combined.Odd += preview.Odd
	}

	return combined, nil
}
