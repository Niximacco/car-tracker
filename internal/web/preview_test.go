package web

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Niximacco/car-tracker/internal/seed"
	"github.com/Niximacco/car-tracker/internal/stats"
	"github.com/Niximacco/car-tracker/internal/types"
)

// A throwaway that writes the two big pages to disk, drawn from the four years
// of history compiled into the binary, so they can be looked at in a browser
// without a datastore behind them. It only runs when it is asked for by name
// and a directory to write into, so an ordinary "go test ./..." never sees it.
//
//	CAR_TRACKER_PREVIEW_DIR=/some/dir go test ./internal/web/ -run Preview
func TestWritePreviewPages(t *testing.T) {
	into := os.Getenv("CAR_TRACKER_PREVIEW_DIR")
	if into == "" {
		t.Skip("set CAR_TRACKER_PREVIEW_DIR to write the preview pages")
	}

	vehicle := seed.Vehicle("anthony@example.com")

	preview, err := seed.Load(vehicle)
	if err != nil {
		t.Fatalf("could not read the built-in history: %s", err.Error())
	}

	fills := []types.Fillup{}
	visits := []types.Service{}

	for _, row := range preview.Rows {
		switch {
		case !row.Readable():

		case row.Kind == "fillup":
			fills = append(fills, row.Fillup)

		default:
			visits = append(visits, row.Service)
		}
	}

	report := stats.Analyze(vehicle, fills, visits, time.Now())

	css, err := assetFS.ReadFile("static/app.css")
	if err != nil {
		t.Fatalf("could not read the stylesheet: %s", err.Error())
	}

	page := New(vehicle.Called())
	page.Email = "anthony@example.com"
	page.SignedIn = true
	page.IsAdmin = true
	page.CanManage = true
	page.Vehicle = vehicle
	page.Report = report

	page.MPGChart = MPGChart(report)
	page.PriceChart = PriceChart(report)
	page.CostChart = CostChart(report)
	page.PerMileChart = PerMileChart(report)
	page.SpendChart = SpendChart(report)
	page.MilesChart = MilesChart(report)
	page.TotalChart = TotalChart(report)

	for _, year := range report.Years {
		if value := float64(year.TotalCents); value > page.WidestYear {
			page.WidestYear = value
		}
	}

	for _, station := range report.Stations {
		if value := float64(station.Fills); value > page.WidestStation {
			page.WidestStation = value
		}
	}

	for _, month := range report.Seasons {
		if month.MPG > page.WidestMonth {
			page.WidestMonth = month.MPG
		}
	}

	for _, month := range report.Timeline {
		if value := float64(month.TotalCents); value > page.WidestPeriod {
			page.WidestPeriod = value
		}
	}

	for _, day := range report.Habits.Days {
		if value := float64(day.Fills); value > page.WidestDay {
			page.WidestDay = value
		}
	}

	for name, file := range map[string]string{
		VehiclePage:  "overview.html",
		InsightsPage: "insights.html",
		FillupsPage:  "fuel.html",
		ServicesPage: "service.html",
	} {
		page.Nav = strings.TrimSuffix(file, ".html")

		body := render(t, name, page)
		body = inlineStylesheet(body, string(css))

		if err := os.WriteFile(into+"/"+file, []byte(body), 0o644); err != nil {
			t.Fatalf("could not write %s: %s", file, err.Error())
		}

		t.Logf("wrote %s/%s", into, file)
	}
}

// inlineStylesheet swaps the versioned <link> for the stylesheet itself, so the
// written file stands on its own with no server to fetch it from.
func inlineStylesheet(body string, css string) string {
	open := strings.Index(body, `<link rel="stylesheet"`)
	if open < 0 {
		return body
	}

	shut := strings.Index(body[open:], ">")
	if shut < 0 {
		return body
	}

	return body[:open] + "<style>" + css + "</style>" + body[open+shut+1:]
}
