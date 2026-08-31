# car-tracker

Four years of fill-ups, eleven visits to the shop, and the twelve numbers underneath them that answer
"what does this car actually cost".

It replaces a Google Form feeding a Google Sheet — that sheet's shape, kept on purpose, including the
numbers that have been read for four years — with the parts a spreadsheet cannot do: a form on a phone
at the pump instead of a form that emails you a link, arithmetic that cannot be broken by sorting a
column, more than one car, more than one person, and every figure the data supports rather than the
handful somebody had the patience to write formulas for.

Sign in is passwordless: an allow-listed address requests a magic link, clicking it starts a session,
and the session cookie slides forward every time you come back. Every page needs one.

**The session lasts a year**, rather than the thirty days the sibling services use. That is a decision
this repository's own data made: the gaps between fill-ups in the history it replaces average 25 days
and run to 99, and sixteen of the sixty-six are longer than a month — so a thirty day session would
have demanded a fresh magic link about sixteen times in four years, every one of them while standing
at a pump holding a nozzle. The point of this site is to be faster than the form it replaces, and
"check your email" at the pump is slower than the form it replaces.

The length is not the revocation window, which is what makes it cheap. Every request re-reads the
account from datastore, so disabling one ends its sessions on the next click no matter how long its
token had left. A year is what somebody would have to keep an unlocked phone for, not what they would
keep access for after being shut out. It stays under 400 days deliberately: browsers cap cookie
lifetime there, and anything longer is silently clamped rather than honoured.

The link itself comes from [ajn_auth](https://github.com/Niximacco/ajn_auth), the shared service at
`auth.ajn.me`. Minting the token, mailing it, hosting the "yes, it was me" page and the per-address
send caps all live there. What stays here is the half a shared service cannot hold: **who may sign
in**. The user list, the session cookie and its signing key are ours, and the service is asked nothing
but "did this person prove they can read that address".

Go, gin and Cloud Datastore. It renders its own html from templates compiled into the binary, draws
its own charts as inline svg with nothing fetched from anywhere, and carries the old spreadsheet
inside itself as two csv files — so a deploy is one container and there is nothing to sync anywhere.

## The idea

**Store what was observed. Work out everything else.** A fill-up holds what you could read at the
pump: the day, the odometer, the gallons, the price per gallon, and whatever came off it. It does not
hold the miles covered, the miles per gallon, what the tank cost, or the running total — every one of
those depends on the fill-up *before* it, and a stored number that depends on a neighbour is a number
that goes quietly wrong the moment a forgotten fill-up is added in the middle. Sixty-seven rows is
nothing to add up on every request, so they are added up on every request.

This is the whole reason for the move. A spreadsheet formula is right until somebody sorts a column.

**A tank only has a miles per gallon if it started full and ended full.** That is the rule the stats
package is built around, and it is the one thing the sheet had no way to express. A splash-and-go
puts real fuel in the tank and real money on the card, and the distance since the last fill divided
by *that* is not a fuel economy figure — it is a larger number that will sit in your average forever.
So a fill-up can be marked **partial**, or marked as having a **gap** before it, and either one
contributes its gallons and its cost to every total and contributes no mpg at all. So does the fill
*after* a partial, because the distance it covers started from a tank that was not full.

Everything that is not mpg counts every fill, because you paid for all of them.

**Two numbers called "miles per gallon", and both are true.** *Lifetime* is the sheet's headline —
every mile since the day you bought it, over every gallon you have logged. It is a fraction low,
because the miles include the ones driven before the logging started and the gallons do not. It is
kept exactly as the sheet had it, to the eighth decimal place, because it is the number that has been
watched for four years. *Measured* is the same question asked strictly: only tanks that started full
and ended full, over only the gallons that filled them. That is the one to quote at somebody. Both are
on the page, labelled.

**Total miles is measured from the day you bought it.** Not from the first fill-up that happened to
get logged. That is why the lifetime mileage is larger than the distance between the first and last
rows of the fuel log, and it is why a vehicle carries a purchase date and a purchase odometer as
first-class fields. Without them the site can still tell you what the fuel cost; it cannot tell you
what the car costs, and it says so on the page rather than dividing by nothing.

**Miles are divided by a different denominator than money.** The odometer is only known up to the last
entry, so miles a day is the distance over the days *to that reading*. The money genuinely was spent
across the whole time you have owned it, so cost per day is over the days you have owned it. The sheet
did exactly this — it is the one place its two denominators differ — and it was right to. Dividing the
distance by the days since the last fill as well would report a car that has been sitting still for a
fortnight as one that covers less ground.

**Money is whole cents in an `int64`, and it is rounded where a receipt rounds.** A tank's cost is
worked out once, from gallons times price, and rounded there — so every total on the site is a sum of
what you actually paid rather than a sum of unrounded products. It comes out three cents away from the
spreadsheet's `SUMPRODUCT` over four years. The three cents are on our side: this total can be checked
against a bank statement.

**The fuel saver is its own column, not a note.** The price stored is what was actually paid; the
discount a gallon is stored beside it, and the pump price is the two added back. Half the history has
a grocery-store discount on it, running to two dollars a gallon at its best, and a site that stored
only one of those numbers would be a site with a chart of the wrong one. What it comes to over a year
turns out to be one of the more satisfying figures here.

**A car is shared, not owned twice.** A vehicle belongs to whoever added it and carries a list of
other addresses. Everybody on that list logs fill-ups and reads every figure; only the owner changes
what the car *is*, because the purchase date and odometer are the origin every lifetime figure is
measured from and somebody fixing their own typo should not be able to move them. Two drivers have to
have one set of fill-ups or none of the arithmetic means anything — and that is a different thing from
everybody on the site seeing everybody's cars.

**An odd row is flagged, not refused.** A reading that goes backwards, a tank bigger than the tank, a
miles per gallon no petrol engine has returned — every one of those is occasionally true. An odometer
does go backwards when a cluster is replaced. So they are marked on the row and imported anyway,
because a log that cannot record what happened is not a log.

**Every write is a form post that redirects.** There is no API to talk to and no javascript a page
needs in order to work. The two lines of script on the site ask "are you sure" before a deletion and
stop a double submit. This is the one deliberate difference from the sibling services, and it is
because this site is a data-entry form used one-handed at a pump: a page that works when a script does
not is worth more here than a page that never reloads.

## The pages

| | |
|---|---|
| **Garage** | Every car you own or have been shared on, with its odometer, its mpg, and what it costs per mile and per month |
| **Overview** | The block from the bottom of the spreadsheet, what the car costs per month split into gas and maintenance, four charts, and the last few entries of each kind |
| **Fuel log** | The whole history, with the sheet's own columns — including the running total, days owned and cost per day the sheet had headings for and never filled in — and the form that adds to it |
| **Service** | Every visit, the work as the receipt listed it, the usual interval between visits, and how far it has been since the oil was done |
| **Insights** | Year by year and month by month, how much it varies, which way it is going, the weather, where you buy gas, when you stop for it, the extremes, and what the discount has been worth |
| **Import** | A paste, or four years of the original sheet, previewed row by row before anything is written |
| **Settings** | What the car is, where the clock started, who can see it, and how to delete it |
| **Access** | Who may sign in, and what they may do |

## The insights

Everything the sheet computed is here, in the order it had it. These are the ones it did not:

- **The weather.** April to September against October to March. A cold engine spends longer warming
  up and a cold tank does less work, and the gap is worth seeing rather than wondering about. Also
  broken down by month of the year, across every year at once.
- **Where you buy fuel.** Every station with its share of your fill-ups, what you have paid there a
  gallon, what the sign said, and what the discount saved. The price column is exactly what it looks
  like; the mpg column mostly says something about the roads you are on when you stop somewhere.
- **Year by year.** Miles, gallons, mpg, price per gallon, fuel, shop, and what the discount saved,
  with the total drawn as a bar so a bad year is visible without reading.
- **Month by month.** The same again at the resolution the spending actually happens at, gaps
  included: a month you did not fill up in is a row of nothing rather than a row that is missing,
  because a line that skips it draws a flat stretch where there should be a dip.
- **How much it varies.** The median tank, the standard deviation, the band eight tanks in ten fall
  inside, and the same three for what a gallon has cost. An average cannot tell a car that returns 34
  every time from one that alternates 29 and 39, and those are different cars to own.
- **Which way it is going.** A least-squares line fitted through every measured tank, in miles per
  gallon per year, and another through the price paid. Beside them, the last twelve months held
  against the twelve before them for spending, distance and economy. A car quietly losing half a mile
  per gallon a year is invisible in a column that bounces four either side of the average every winter.
- **When you stop for gas.** Which day of the week, what share of it is a weekend, and — for the
  fill-ups that carry a clock — what time of day. A fact about your week rather than about the car,
  and the only place the log knows it.
- **The extremes.** Best and worst tank, cheapest and most expensive gallon, farthest on one tank,
  longest a tank has lasted, biggest fill, the priciest month and the farthest — and the averages they
  are extreme against.
- **If nothing changes.** Where the odometer probably is *today* rather than at the last entry, when
  the next fill-up is due, where the clock will be in a year, and what the next twelve months cost at
  the rate of the last twelve. All of it labelled as what it is.
- **The oil.** Found by reading the invoices rather than by a checkbox nobody would tick, with the
  interval taken as the median of what has actually been done — so a change done early because the car
  was in for something else does not lower the expectation for good. It is measured against the
  *estimated* odometer, so "due in 400 miles" means something between fill-ups.
- **Cost of ownership**, kept deliberately separate from cost of running. What the car cost, less what
  it sold for, plus everything spent on it. It only appears when the purchase price is on file, because
  a zero would read as a car that was free.
- **What it uses**, as opposed to what it costs: gallons per hundred miles, which is the figure the
  money scales with in a way miles per gallon does not; how far a full tank goes; and the carbon in
  the gasoline that has been burned, at the EPA's 19.6 pounds a gallon.
- **Seven charts.** On the overview: every tank's mpg with a five-fill average through it, what a
  gallon cost both ways with the discount as the gap between the lines, and the running cost per day
  beside the running cost per mile. On the insights page: where the money went month by month with the
  shop drawn apart from the gas, how far the car went each month, and everything spent adding up.

## Bringing the spreadsheet in

The import screen takes a csv download or a straight copy and paste out of the browser, which arrives
tab separated. The first line has to be the column headings; every column is found by name rather than
by position, because the two sheets have different columns in different orders and a positional reader
is one that silently imports the odometer into the gallons the day somebody adds a column.

It needs a date and an odometer. From there a row with gallons on it is a fill-up and a row with work
on it is a visit to the shop — decided from what is on the row rather than from the sheet's own **Log
Type** column, which the form stopped filling in three years ago and which the maintenance sheet never
had. Columns this site works out for itself — the difference, the mpg, the running cost — are ignored
rather than complained about.

Nothing is written until you have looked at it. Reading the paste and writing it are separate
requests, with a screen between them showing every row as it was understood, what would not read and
why, and which rows are worth a second look.

**It is safe to run twice.** Every entry is stored under its day and its odometer reading, so a second
run writes over the same entities rather than doubling them. That is the difference between an import
you can get wrong and simply do again, and one you get a single attempt at.

### The built-in copy

The original sheet — 67 fill-ups from December 2021 to August 2026, and 11 visits to West Side VW — is
compiled into the binary as two csv files, so the very first import needs no export at all. It goes
through the same reader a paste does and lands on the same confirm screen.

The transcription is faithful with three exceptions, all of them about columns the sheet did not have.
Station names are spelled one way, because "Speedway crystal" and "Crystal speedway" are one station
and would otherwise be four rows in the breakdown. The note about spilled fuel in February 2022 moved
out of the location column into its own. And the dozen locations reading "Hy-Vee new hope - 1.45 in
fuel saver used" lost that second half, which is not a note — it is the fuel saver column, and it is
already in the fuel saver column.

Two facts the summary block is measured from do not appear on any row of the sheet: the day the car
was bought and what the odometer read that day. They are recovered from the block itself — 27,102
total miles at a last reading of 95,176, and 1,705 days owned as of a last fill 18 days earlier — which
gives **28 December 2021 at 68,074 miles**. Those are pre-filled on the vehicle; what the car actually
*is* is not, because the history never says. "West Side VW" and a DSG service is as much as it knows.

## Who can do what

| | |
|---|---|
| **Admin** | Everything below, plus every vehicle on the site and the page that decides who may sign in |
| **Member** | Their own vehicles, and any shared with them. Creates vehicles, logs against them, owns what they create |
| **Read only** | Can look at anything shared with them and change nothing |
| **Blocked** | Kept for history, cannot sign in |

An account's flags are read from datastore on every request rather than carried in the token, so
revoking access takes effect on somebody's next click rather than whenever the cookie happens to
expire. That is what makes the long session below safe: its length is what somebody would have to
keep an unlocked device for, not what they would keep access for after being shut out.

On a particular vehicle there are two more questions, and they are on the vehicle rather than on the
account. **Anybody it is shared with** may log fill-ups and shop visits, and correct or delete them.
**Only the owner** may change what the car is, move the purchase date or odometer, run an import,
change the share list, or delete it.

An admin cannot take their own access away from the Access page. There is no reason to do it that
signing in as the other admin does not serve better, and the way back from it is an environment
variable and a redeploy.

## Configuration

| Variable | Required | What it is |
|---|---|---|
| `DATASTORE_PROJECT_ID` | yes | GCP project, `ajnhosting-163818` |
| `DATASTORE_NAMESPACE` | yes | Datastore namespace, `cartracker` |
| `JWT_SIGNING_KEY` | yes | Signs session cookies. The service refuses to start without it |
| `APP_BASE_URL` | yes in production | Public origin, `https://car.ajn.me` |
| `SITE_NAME` | no | What the pages call this. Defaults to `Car Tracker` |
| `AJN_AUTH_URL` | for login | The magic link service, `https://auth.ajn.me` |
| `AJN_AUTH_API_KEY` | for login | This site's key, generated in the service's admin pages. Without it nobody can sign in |
| `AJN_AUTH_REDIRECT_URI` | no | Only needed if this site registers more than one callback. Empty uses the first one on the roster |
| `FONTAWESOME_KIT` | no | Font Awesome kit id, for the icons. Without it the pages render without them |
| `BOOTSTRAP_ADMIN_EMAIL` | first run | Creates this address as an admin if it does not exist |
| `BOOTSTRAP_ADMIN_NAME` | no | What to call that account |
| `SESSION_COOKIE_NAME` | no | Defaults to `ct_session` |
| `COOKIE_DOMAIN` | no | Leave unset for a host-only cookie |
| `COOKIE_SECURE` | no | Set `false` only for plain http local development |
| `TRUSTED_PROXY_DEPTH` | no | `0` is right behind a Cloud Run domain mapping. A load balancer or CDN in front adds a hop |

`BOOTSTRAP_ADMIN_EMAIL` is safe to leave set: an existing user is never touched, so it cannot be used
to quietly restore admin to somebody who had it taken away. It is the way out of the chicken-and-egg a
brand new namespace is in — nobody can sign in, and only somebody signed in can let anybody in.

There is no `RESEND_API_KEY` here and no `MAIL_FROM`. This service sends no email of its own: the only
message it ever caused was the sign-in link, and that belongs to `auth.ajn.me` now.

### Registering this site with auth.ajn.me

The service will not mail a link on behalf of a site it does not know, and will not deliver an
exchange code to a url that site did not register. Both live in the roster, edited at
`https://auth.ajn.me/admin`:

| Field | Value |
|---|---|
| Id | `car-tracker` |
| Name | `Car Tracker` |
| Base url | `https://car.ajn.me` |
| Redirect uris | `https://car.ajn.me/auth/callback`, and `http://localhost:8080/auth/callback` to develop against the deployed service |
| From | `Car Tracker <login@ajn.me>` |
| Accent | `#1d4e77` — the site's own `--accent`, so the button in the email is the button on the page |

Then generate a key on that page and set it as `AJN_AUTH_API_KEY` here. The plaintext is shown once
and is not recoverable, so rotating means generating a second, deploying it, and revoking the first.

### The clock

Every date on this site is a wall-clock fact about a place — "the Tuesday I filled up" — so they are
all read in one zone, `America/Chicago`, set in `internal/types`. A server running in UTC without this
would put every evening fill-up on the following day and give the day it belongs to one fewer.

The tz database is compiled into the binary through the `time/tzdata` import in `main`, because the
runtime image is a bare alpine with no zoneinfo in it.

### Icons and the stylesheet

The styling is one file, `internal/web/static/app.css`, compiled into the binary like the templates
and served at a name carrying a hash of its own contents — `/static/app.a1b2c3d4e5.css`. Change the
file and the name changes with it, which is what makes it safe to tell a browser to keep it forever.

The charts are inline `<svg>` built on the server by `internal/chart`, which works out the geometry
and hands the templates strings. There is no charting library, nothing fetched from a third party, and
the lines take their colours from the same css variables the rest of the page does — so a chart is
right in both light and dark without being drawn twice, and the arithmetic behind every line is
somewhere a test can reach it.

The two running-cost charts leave their settling-in period off the front. A tank of gas divided by the
four days you had owned the car is not a fact about the car, and drawn to scale it puts the next four
years in the bottom eighth of the box. The band a reading has to be inside is Tukey's — the middle
half of the column, opened out by one and a half times its own width — and only the front of the line
is trimmed, so a step later on, which is what a transmission looks like, is never dropped. Nothing is
dropped quietly either: the chart carries the count and the page underneath says how many.

The vertical axis of those charts does not start at zero, deliberately. These are fuel figures in the
thirties and prices between one and five dollars; an axis from zero would flatten every one of them
into a line near the top of the box. They are charts of how something changed, and both ends are
labelled so they cannot be read as anything else.

Icons come from a Font Awesome kit, which may not be configured. Everything on every page is labelled
in words as well, so a kit that is missing, blocked or slow leaves a site with no icons on it rather
than a site with holes in it.

### Datastore

Four kinds, in one namespace:

| Kind | Key | Notes |
|---|---|---|
| `user` | Lower-cased email | Having one is what grants access |
| `vehicle` | Slug, `the-wagon` | |
| `fillup` | `2026-08-11-95176`, **child of its vehicle** | The day and the odometer reading |
| `service` | `2026-08-07-95077`, **child of its vehicle** | The same |

Entries hang off their vehicle as datastore children rather than carrying the vehicle in a property,
and two things follow from that. An ancestor query is strongly consistent, so pasting four years of
history and then looking at it cannot show a car with half its fill-ups missing. And an entry's id
only has to be unique inside its own car — which is what lets the id be the day and the reading rather
than a random string, and that is what makes an import safe to run twice.

Deleting a vehicle deletes its entries first. Entries that outlived their vehicle would be rows
nothing can reach and nothing can delete, since every query for them is an ancestor query on a key
that no longer resolves.

None of the list queries carries a sort order. Sorting in datastore alongside an ancestor filter would
need a composite index for the sake of ordering a few hundred rows that are about to be held in memory
anyway.

## Running it locally

Everything except datastore runs with no cloud access at all:

```bash
go test ./...
```

To run the service you need the datastore emulator:

```bash
gcloud beta emulators datastore start --project=ajnhosting-163818 --host-port=localhost:8081
```

Then, in another shell:

```bash
export DATASTORE_EMULATOR_HOST=localhost:8081 DATASTORE_PROJECT_ID=ajnhosting-163818 DATASTORE_NAMESPACE=cartracker JWT_SIGNING_KEY=local-development-only APP_BASE_URL=http://localhost:8080 COOKIE_SECURE=false BOOTSTRAP_ADMIN_EMAIL=you@example.com BOOTSTRAP_ADMIN_NAME=You && go run ./cmd/car-tracker
```

With no `AJN_AUTH_URL` and `AJN_AUTH_API_KEY` the login form says sign in is unavailable, because
there is nothing to ask for a link. Signing in locally means pointing at the deployed service with a
real key and having `http://localhost:8080/auth/callback` on this site's redirect uris — that is the
one place the service allows plain http, and only for localhost:

```bash
export AJN_AUTH_URL=https://auth.ajn.me AJN_AUTH_API_KEY=ajnauth_xxx AJN_AUTH_REDIRECT_URI=http://localhost:8080/auth/callback
```

The link still arrives by email and its confirm page is still on `auth.ajn.me`; only the redirect at
the end of it comes back to the laptop.

`FONTAWESOME_KIT` is left unset above, so the pages come up without icons. That is what they are built
to survive; add the variable if you want to see them locally.

## What the tests cover

They are the parts that are painful to debug in production, and nothing that needs a cloud connection:

- **The summary block, against the spreadsheet.** `internal/seed` loads the four years of embedded
  history and checks every figure the sheet showed: total miles, days owned, days at the last fill,
  total gallons to the thousandth, total maintenance to the cent, cumulative mpg to eight decimal
  places, and all twelve cells of the three costs read three ways. Those numbers came off the bottom of
  the sheet rather than out of this code, so if a change moves one of them that is a bug or a decision
  — and either way it is worth finding out here rather than by noticing the front page says something
  different from last week.
- **The rule about full tanks.** All four ways a tank fails to carry an mpg — the first row, a partial,
  the fill after a partial, and a known gap — with the money and the gallons still counting in every
  one of them. Getting these wrong produces a page of plausible numbers that are quietly wrong.
- **The things that only happen once.** A car with no purchase date, a retired car whose clock has to
  stop, a history that arrives out of order, an empty history, a single fill-up dividing by nothing.
- **The spreadsheet reader.** The sheet's own export, the maintenance sheet with line breaks inside a
  cell, a browser paste with a comma inside one, one bad line not costing you the other sixty-six,
  the odd rows being flagged rather than refused, and every date format the form and the sheet have
  between them produced.
- **The route set** gin builds at startup, which panics on a conflict rather than failing a request —
  a crash at startup rather than a bad page, and `/vehicles/new` sits one letter from `/vehicle/:slug`.
- **Every template rendering**, populated and empty, because a template only fails when it is asked to
  render and a typo in a field name is a silently truncated page. Plus the two that matter: a
  view-only account is not offered a button that would come back refused, and what somebody types
  cannot get out of the page as markup.
- **The chart geometry.** Points inside the box, a gap breaking the line rather than being drawn
  through, a flat series not dividing by a range of nothing, and sixty-seven readings not producing
  sixty-seven axis labels.
- **The shape of the history**, which is the arithmetic that describes the numbers rather than adding
  them up: a month nothing happened in still being a month, a month's mpg counting only the tanks that
  can carry one, the spread refusing to describe two readings, the fitted slope finding a car that is
  getting worse, the year-on-year comparison waiting for a year to compare against, the monthly bill
  dividing by the days it actually covers, and the outlier fences catching the settling-in period
  while leaving the ordinary spread alone.

## Deploying

A commit on `main` runs the tests, builds the container, pushes it to Artifact Registry and deploys it
to Cloud Run. Authentication to Google is keyless, through Workload Identity Federation: GitHub mints
a short-lived OIDC token and Google exchanges it for one on the deploy service account, so there is no
JSON key to store or rotate.

Setting it up is in the header of `.github/workflows/google-cloudrun-docker.yml`. In short: enable Cloud
Run, Artifact Registry and Datastore; create the Artifact Registry repository named `car-tracker` in
`us-central1`; add the `WIF_PROVIDER` and `WIF_SERVICE_ACCOUNT` repository secrets and bind this repo
to that service account; and give the Cloud Run service's own runtime account `roles/datastore.user`
plus access to the secrets above.

`car.ajn.me` is a Cloud Run domain mapping onto the service. It has to match `APP_BASE_URL` and the
base url registered at `auth.ajn.me`, or the sign-in link comes back somewhere the session cookie is
not.

## Moving the sheet over

1. Deploy, with `BOOTSTRAP_ADMIN_EMAIL` set to your address.
2. Sign in. The garage is empty.
3. **Add a vehicle.** The name is all it needs, but fill in the purchase date and odometer while you
   are there — they are what every per-day and per-mile figure is measured from. For the wagon that is
   28 December 2021 at 68,074 miles.
4. **Import.** Either paste the sheet, or press *Read the built-in history* and get all four years at
   once. Look at the preview, then confirm.
5. Check the overview against the bottom of the spreadsheet. It should agree to the penny on
   maintenance and to within a nickel on fuel, and the reason for the nickel is above.
6. Turn the Google Form off, or leave it — the import is safe to re-run, so a few more rows landing in
   the sheet before you get round to it costs nothing.
