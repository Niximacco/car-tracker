// Package data is everything that touches Cloud Datastore.
//
// The user half is carried over from wild-games and niximacco-recipes, because
// the allow list it implements is the same allow list. What is this site's own
// is the vehicle, and the two kinds of entry that hang off it.
//
// Fill-ups and shop visits are datastore children of their vehicle. Two things
// follow from that and both of them matter. An ancestor query is strongly
// consistent, so pasting four years of history and then looking at it cannot
// show a car with half its fill-ups missing. And an entry's id only has to be
// unique inside its own car, which is what lets the id be the day and the
// odometer reading rather than a random string - so importing the same rows
// twice updates them instead of doubling them.
package data

import (
	"context"
	"errors"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	datastore "cloud.google.com/go/datastore"
	"github.com/Niximacco/car-tracker/internal/types"
)

var datastoreClient *datastore.Client
var ctx context.Context
var namespace string

var AlreadyExistsErr = errors.New("entity already exists")

var (
	UserNotFoundErr = errors.New("user not found in datastore")
	UserDisabledErr = errors.New("user is disabled")

	VehicleNotFoundErr = errors.New("vehicle not found")
	FillupNotFoundErr  = errors.New("fill-up not found")
	ServiceNotFoundErr = errors.New("service record not found")
	NoVehiclesErr      = errors.New("no vehicles have been added yet")
)

const (
	userKind    = "user"
	vehicleKind = "vehicle"
	fillupKind  = "fillup"
	serviceKind = "service"
)

// The list caps. None of these queries carries a sort order: sorting in
// datastore alongside an ancestor filter would need a composite index, and at
// one car's scale - a fill-up a fortnight for a decade is under three hundred
// rows - sorting the results here is free.
const (
	USER_LIST_LIMIT    = 500
	VEHICLE_LIST_LIMIT = 200
	FILLUP_LIST_LIMIT  = 5000
	SERVICE_LIST_LIMIT = 1000
)

func Initialize() {
	projID := os.Getenv("DATASTORE_PROJECT_ID")
	if projID == "" {
		log.Fatal(`You need to set the environment variable "DATASTORE_PROJECT_ID"`)
	}

	namespace = os.Getenv("DATASTORE_NAMESPACE")
	if namespace == "" {
		log.Fatal(`You need to set the environment variable "DATASTORE_NAMESPACE"`)
	}

	ctx = context.Background()
	client, err := datastore.NewClient(ctx, projID)
	if err != nil {
		log.Fatalf("Could not create datastore client: %v", err)
	}

	datastoreClient = client
}

// ---------------------------------------------------------------- users ----

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidAddress is a deliberately loose check. The real check is whether the
// address exists in the user kind; this only catches obvious junk before we
// bother datastore, or auth.ajn.me, with it.
func ValidAddress(email string) bool {
	if len(email) < 3 || len(email) > 254 {
		return false
	}

	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 {
		return false
	}

	domain := email[at+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	return !strings.ContainsAny(email, " \t\r\n<>\"")
}

func userKey(email string) *datastore.Key {
	key := datastore.NameKey(userKind, NormalizeEmail(email), nil)
	key.Namespace = namespace
	return key
}

func GetUser(email string) (user types.User, err error) {
	err = datastoreClient.Get(ctx, userKey(email), &user)
	if errors.Is(err, datastore.ErrNoSuchEntity) {
		return user, UserNotFoundErr
	}
	// A user entity created by hand in the console may carry properties this
	// struct doesn't know about. Everything it does know about still loaded, so
	// that isn't a reason to refuse the login.
	if err != nil && !isFieldMismatch(err) {
		return user, err
	}

	if user.Email == "" {
		user.Email = NormalizeEmail(email)
	}

	if user.Disabled {
		return user, UserDisabledErr
	}

	return user, nil
}

// NewUser adds an address to the allow list. An account is either an admin, a
// view-only reader, or an ordinary member - admin and view only are opposites,
// so an account asked to be both is made an admin.
func NewUser(email string, name string, admin bool, viewOnly bool) (user types.User, err error) {
	email = NormalizeEmail(email)
	key := userKey(email)

	user = types.User{
		Email:    email,
		Name:     strings.TrimSpace(name),
		Created:  time.Now().Unix(),
		Admin:    admin,
		ViewOnly: viewOnly && !admin,
	}

	_, err = datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var existing types.User
		if err := tx.Get(key, &existing); err != datastore.ErrNoSuchEntity {
			if err == nil || isFieldMismatch(err) {
				return AlreadyExistsErr
			}
			return err
		}

		_, err := tx.Put(key, &user)
		return err
	})

	if err != nil {
		return types.User{}, err
	}

	return user, nil
}

func ListUsers(limit int) (users []types.User, err error) {
	query := datastore.NewQuery(userKind).Namespace(namespace)
	if limit > 0 {
		query = query.Limit(limit)
	}

	keys, err := datastoreClient.GetAll(ctx, query, &users)
	if err = tolerateFieldMismatch(err); err != nil {
		return nil, err
	}

	if users == nil {
		users = []types.User{}
	}

	for i := range users {
		if users[i].Email == "" && i < len(keys) {
			users[i].Email = keys[i].Name
		}
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].Email < users[j].Email
	})

	return users, nil
}

// UpdateUser changes what an account is allowed to do.
//
// Admin and view-only cannot both be true: one manages the site, the other may
// not change anything at all. Rather than refusing the combination, the flag
// being set wins and the other is cleared - "make them view only" is a clear
// instruction from an admin who can see both boxes, and answering it with an
// error about a state nobody asked for would be pedantry.
func UpdateUser(email string, name *string, admin *bool, viewOnly *bool,
	disabled *bool) (user types.User, err error) {
	email = NormalizeEmail(email)

	values := map[string]interface{}{}

	if name != nil {
		values["Name"] = strings.TrimSpace(*name)
	}
	if admin != nil {
		values["Admin"] = *admin
		if *admin {
			values["ViewOnly"] = false
		}
	}
	if viewOnly != nil {
		values["ViewOnly"] = *viewOnly
		if *viewOnly {
			values["Admin"] = false
		}
	}
	if disabled != nil {
		values["Disabled"] = *disabled
	}

	if len(values) == 0 {
		return types.User{}, nil
	}

	if err = setUserProperties(email, values); err != nil {
		return types.User{}, err
	}

	// Read it back so the caller gets what is actually stored. GetUser refuses
	// to return a disabled user, so build that case from what we just wrote.
	user, err = GetUser(email)
	if errors.Is(err, UserDisabledErr) {
		user.Email = email
		user.Disabled = true
		return user, nil
	}

	return user, err
}

// SetDefaultVehicle remembers which car an account opens on, or forgets it when
// the slug is empty.
//
// It is its own call rather than another pointer on UpdateUser because it is a
// different kind of change: UpdateUser is an admin deciding what an account may
// do, and this is an account deciding where it lands. Nothing here needs to
// check the slug - the handler has already made sure this account can see that
// car, and the page it points at checks again on every visit.
func SetDefaultVehicle(email string, slug string) error {
	return setUserProperties(email, map[string]interface{}{
		"DefaultVehicle": strings.TrimSpace(slug),
	})
}

func MarkLoggedIn(email string, at time.Time) error {
	return setUserProperties(email, map[string]interface{}{"LastLogin": at.Unix()})
}

func mutateUser(email string, change func(current datastore.PropertyList) ([]datastore.Property, error)) error {
	key := userKey(email)
	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var properties datastore.PropertyList
		if err := tx.Get(key, &properties); err != nil {
			if errors.Is(err, datastore.ErrNoSuchEntity) {
				return UserNotFoundErr
			}
			return err
		}

		writes, err := change(properties)
		if err != nil {
			return err
		}

		// A write replaces the whole property, indexing included, so what ends
		// up on the entity is what the caller asked for rather than a mix of
		// that and however the property happened to be stored before.
		for _, write := range writes {
			updated := false
			for i := range properties {
				if properties[i].Name == write.Name {
					properties[i] = write
					updated = true
					break
				}
			}

			if !updated {
				properties = append(properties, write)
			}
		}

		_, err = tx.Put(key, &properties)
		return err
	})

	return err
}

func setUserProperties(email string, values map[string]interface{}) error {
	return mutateUser(email, func(datastore.PropertyList) ([]datastore.Property, error) {
		writes := make([]datastore.Property, 0, len(values))
		for name, value := range values {
			writes = append(writes, datastore.Property{Name: name, Value: value})
		}

		return writes, nil
	})
}

// ------------------------------------------------------------- vehicles ----

func vehicleKey(slug string) *datastore.Key {
	key := datastore.NameKey(vehicleKind, slug, nil)
	key.Namespace = namespace
	return key
}

func GetVehicle(slug string) (vehicle types.Vehicle, err error) {
	err = datastoreClient.Get(ctx, vehicleKey(slug), &vehicle)
	if errors.Is(err, datastore.ErrNoSuchEntity) {
		return vehicle, VehicleNotFoundErr
	}

	if err != nil && !isFieldMismatch(err) {
		return vehicle, err
	}

	if vehicle.Slug == "" {
		vehicle.Slug = slug
	}

	return vehicle, nil
}

// NewVehicle adds a car. The slug is the caller's to choose and is refused if
// it is taken, rather than being made unique behind their back: two people who
// both call theirs "the wagon" should be told, because the second one is about
// to be sharing the first one's history.
func NewVehicle(vehicle types.Vehicle) (types.Vehicle, error) {
	vehicle.Slug = strings.TrimSpace(vehicle.Slug)
	if vehicle.Slug == "" {
		return types.Vehicle{}, errors.New("a vehicle needs a name")
	}

	vehicle.Owner = NormalizeEmail(vehicle.Owner)
	vehicle.SharedWith = normalizeAddresses(vehicle.SharedWith, vehicle.Owner)
	vehicle.Created = time.Now().Unix()
	vehicle.Updated = vehicle.Created

	key := vehicleKey(vehicle.Slug)

	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var existing types.Vehicle
		if err := tx.Get(key, &existing); err != datastore.ErrNoSuchEntity {
			if err == nil || isFieldMismatch(err) {
				return AlreadyExistsErr
			}
			return err
		}

		_, err := tx.Put(key, &vehicle)
		return err
	})

	if err != nil {
		return types.Vehicle{}, err
	}

	return vehicle, nil
}

// UpdateVehicle reads a vehicle, hands it to the caller to change, and writes
// it back inside a transaction.
//
// The read and the write are in one transaction because two drivers editing the
// same car is the ordinary case here, not a race worth ignoring. A change
// function that returns an error leaves the entity exactly as it was.
func UpdateVehicle(slug string, change func(vehicle *types.Vehicle) error) (vehicle types.Vehicle, err error) {
	key := vehicleKey(slug)

	_, err = datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var current types.Vehicle
		if err := tx.Get(key, &current); err != nil && !isFieldMismatch(err) {
			if errors.Is(err, datastore.ErrNoSuchEntity) {
				return VehicleNotFoundErr
			}
			return err
		}

		if current.Slug == "" {
			current.Slug = slug
		}

		if err := change(&current); err != nil {
			return err
		}

		current.Slug = slug
		current.Owner = NormalizeEmail(current.Owner)
		current.SharedWith = normalizeAddresses(current.SharedWith, current.Owner)
		current.Updated = time.Now().Unix()

		vehicle = current

		_, err := tx.Put(key, &current)
		return err
	})

	if err != nil {
		return types.Vehicle{}, err
	}

	return vehicle, nil
}

// ListVehicles is every car on the site. It is the admin view and the input to
// VehiclesFor; ordinary pages go through that.
func ListVehicles() (vehicles []types.Vehicle, err error) {
	query := datastore.NewQuery(vehicleKind).Namespace(namespace).Limit(VEHICLE_LIST_LIMIT)

	keys, err := datastoreClient.GetAll(ctx, query, &vehicles)
	if err = tolerateFieldMismatch(err); err != nil {
		return nil, err
	}

	if vehicles == nil {
		vehicles = []types.Vehicle{}
	}

	for i := range vehicles {
		if vehicles[i].Slug == "" && i < len(keys) {
			vehicles[i].Slug = keys[i].Name
		}
	}

	// Cars still in the driveway first, then by name. A retired car keeps its
	// whole history and its own page; it just stops being the first thing on
	// the screen.
	sort.SliceStable(vehicles, func(a int, b int) bool {
		if vehicles[a].Retired != vehicles[b].Retired {
			return !vehicles[a].Retired
		}

		return strings.ToLower(vehicles[a].Called()) < strings.ToLower(vehicles[b].Called())
	})

	return vehicles, nil
}

// VehiclesFor is every car an account may see: the ones it owns and the ones it
// has been shared with. An admin sees all of them, because somebody has to be
// able to sort out a car whose owner has left.
//
// The filter is done here rather than in the query. Datastore can filter on an
// array property, but it would take two queries and a merge to get the owned
// and shared sets, and at a garage's scale - a household has a handful of cars,
// not a fleet - reading them and sorting them out here is one round trip.
func VehiclesFor(user types.User) (vehicles []types.Vehicle, err error) {
	all, err := ListVehicles()
	if err != nil {
		return nil, err
	}

	if user.Admin {
		return all, nil
	}

	vehicles = []types.Vehicle{}
	for _, vehicle := range all {
		if vehicle.CanBeSeenBy(user.Email) {
			vehicles = append(vehicles, vehicle)
		}
	}

	return vehicles, nil
}

// DeleteVehicle removes a car and everything logged against it.
//
// The entries go first. A vehicle whose entries outlived it would leave rows
// nothing can reach and nothing can delete, since every query for them is an
// ancestor query on a key that no longer resolves to anything.
func DeleteVehicle(slug string) error {
	parent := vehicleKey(slug)

	for _, kind := range []string{fillupKind, serviceKind} {
		query := datastore.NewQuery(kind).Namespace(namespace).Ancestor(parent).KeysOnly()

		keys, err := datastoreClient.GetAll(ctx, query, nil)
		if err = tolerateFieldMismatch(err); err != nil {
			return err
		}

		// DeleteMulti has a cap on how many keys go in one call, so a long
		// history goes in batches.
		const batch = 400
		for at := 0; at < len(keys); at += batch {
			end := at + batch
			if end > len(keys) {
				end = len(keys)
			}

			if err := datastoreClient.DeleteMulti(ctx, keys[at:end]); err != nil {
				return err
			}
		}
	}

	return datastoreClient.Delete(ctx, parent)
}

// normalizeAddresses lower-cases a share list, drops blanks and duplicates, and
// drops the owner - who is already on the vehicle and does not need to be on
// the list twice, where removing them from it would look like it took their
// access away.
func normalizeAddresses(addresses []string, owner string) []string {
	seen := map[string]bool{NormalizeEmail(owner): true}
	out := []string{}

	for _, address := range addresses {
		address = NormalizeEmail(address)
		if address == "" || seen[address] {
			continue
		}

		seen[address] = true
		out = append(out, address)
	}

	sort.Strings(out)

	return out
}

// ------------------------------------------------------------- fill-ups ----

func fillupKey(vehicle string, id string) *datastore.Key {
	key := datastore.NameKey(fillupKind, id, vehicleKey(vehicle))
	key.Namespace = namespace
	return key
}

func GetFillup(vehicle string, id string) (fillup types.Fillup, err error) {
	err = datastoreClient.Get(ctx, fillupKey(vehicle, id), &fillup)
	if errors.Is(err, datastore.ErrNoSuchEntity) {
		return fillup, FillupNotFoundErr
	}

	if err != nil && !isFieldMismatch(err) {
		return fillup, err
	}

	if fillup.ID == "" {
		fillup.ID = id
	}

	fillup.Vehicle = vehicle

	return fillup, nil
}

// SaveFillup writes a fill-up, creating it or replacing it.
//
// The id is derived from the day and the reading, so saving a fill-up whose
// date or odometer has been corrected writes a new entity rather than moving
// the old one. The caller passes the id it was editing so the old one can go in
// the same transaction; for a new entry that is empty.
func SaveFillup(fillup types.Fillup, replacing string, by string) (types.Fillup, error) {
	if fillup.Vehicle == "" {
		return types.Fillup{}, VehicleNotFoundErr
	}

	fillup.ID = types.FillupID(fillup.On, fillup.Odometer)
	fillup.Updated = time.Now().Unix()

	key := fillupKey(fillup.Vehicle, fillup.ID)

	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var existing types.Fillup
		switch err := tx.Get(key, &existing); {
		case err == nil, isFieldMismatch(err):
			// Keep who logged it first and when. An edit is an edit; it does
			// not make the fill-up somebody else's.
			fillup.Created = existing.Created
			fillup.By = existing.By

		case errors.Is(err, datastore.ErrNoSuchEntity):
			fillup.Created = fillup.Updated
			fillup.By = NormalizeEmail(by)

		default:
			return err
		}

		if fillup.By == "" {
			fillup.By = NormalizeEmail(by)
		}

		if _, err := tx.Put(key, &fillup); err != nil {
			return err
		}

		if replacing != "" && replacing != fillup.ID {
			return tx.Delete(fillupKey(fillup.Vehicle, replacing))
		}

		return nil
	})

	if err != nil {
		return types.Fillup{}, err
	}

	return fillup, nil
}

func DeleteFillup(vehicle string, id string) error {
	return datastoreClient.Delete(ctx, fillupKey(vehicle, id))
}

// ListFillups is a car's whole fuel history, oldest first.
//
// It is an ancestor query with no sort order on it. Sorting in datastore
// alongside an ancestor filter would need a composite index for the sake of
// ordering a few hundred rows we are about to hold in memory anyway.
func ListFillups(vehicle string) (fillups []types.Fillup, err error) {
	query := datastore.NewQuery(fillupKind).
		Namespace(namespace).
		Ancestor(vehicleKey(vehicle)).
		Limit(FILLUP_LIST_LIMIT)

	keys, err := datastoreClient.GetAll(ctx, query, &fillups)
	if err = tolerateFieldMismatch(err); err != nil {
		return nil, err
	}

	if fillups == nil {
		fillups = []types.Fillup{}
	}

	for i := range fillups {
		if fillups[i].ID == "" && i < len(keys) {
			fillups[i].ID = keys[i].Name
		}

		fillups[i].Vehicle = vehicle
	}

	types.ByDate(fillups)

	return fillups, nil
}

// ImportFillups writes a batch, counting what was new and what was already
// there.
//
// It is safe to run twice on purpose. Every id is the day and the reading, so a
// re-paste of the same export updates the same entities - which is what makes
// importing four years of a spreadsheet something you can do wrong and simply
// do again.
func ImportFillups(fillups []types.Fillup, by string) (added int, updated int, err error) {
	for _, fillup := range fillups {
		id := types.FillupID(fillup.On, fillup.Odometer)

		_, getErr := GetFillup(fillup.Vehicle, id)
		switch {
		case getErr == nil:
			updated++

		case errors.Is(getErr, FillupNotFoundErr):
			added++

		default:
			return added, updated, getErr
		}

		if _, err := SaveFillup(fillup, "", by); err != nil {
			return added, updated, err
		}
	}

	return added, updated, nil
}

// ------------------------------------------------------------- services ----

func serviceKey(vehicle string, id string) *datastore.Key {
	key := datastore.NameKey(serviceKind, id, vehicleKey(vehicle))
	key.Namespace = namespace
	return key
}

func GetService(vehicle string, id string) (service types.Service, err error) {
	err = datastoreClient.Get(ctx, serviceKey(vehicle, id), &service)
	if errors.Is(err, datastore.ErrNoSuchEntity) {
		return service, ServiceNotFoundErr
	}

	if err != nil && !isFieldMismatch(err) {
		return service, err
	}

	if service.ID == "" {
		service.ID = id
	}

	service.Vehicle = vehicle

	return service, nil
}

// SaveService writes a shop visit, on the same terms a fill-up is written on.
func SaveService(service types.Service, replacing string, by string) (types.Service, error) {
	if service.Vehicle == "" {
		return types.Service{}, VehicleNotFoundErr
	}

	service.ID = types.ServiceID(service.On, service.Odometer)
	service.Updated = time.Now().Unix()

	key := serviceKey(service.Vehicle, service.ID)

	_, err := datastoreClient.RunInTransaction(ctx, func(tx *datastore.Transaction) error {
		var existing types.Service
		switch err := tx.Get(key, &existing); {
		case err == nil, isFieldMismatch(err):
			service.Created = existing.Created
			service.By = existing.By

		case errors.Is(err, datastore.ErrNoSuchEntity):
			service.Created = service.Updated
			service.By = NormalizeEmail(by)

		default:
			return err
		}

		if service.By == "" {
			service.By = NormalizeEmail(by)
		}

		if _, err := tx.Put(key, &service); err != nil {
			return err
		}

		if replacing != "" && replacing != service.ID {
			return tx.Delete(serviceKey(service.Vehicle, replacing))
		}

		return nil
	})

	if err != nil {
		return types.Service{}, err
	}

	return service, nil
}

func DeleteService(vehicle string, id string) error {
	return datastoreClient.Delete(ctx, serviceKey(vehicle, id))
}

// ListServices is a car's whole maintenance history, oldest first.
func ListServices(vehicle string) (services []types.Service, err error) {
	query := datastore.NewQuery(serviceKind).
		Namespace(namespace).
		Ancestor(vehicleKey(vehicle)).
		Limit(SERVICE_LIST_LIMIT)

	keys, err := datastoreClient.GetAll(ctx, query, &services)
	if err = tolerateFieldMismatch(err); err != nil {
		return nil, err
	}

	if services == nil {
		services = []types.Service{}
	}

	for i := range services {
		if services[i].ID == "" && i < len(keys) {
			services[i].ID = keys[i].Name
		}

		services[i].Vehicle = vehicle
	}

	types.ServicesByDate(services)

	return services, nil
}

// ImportServices writes a batch of shop visits, on the same re-runnable terms
// as ImportFillups.
func ImportServices(services []types.Service, by string) (added int, updated int, err error) {
	for _, service := range services {
		id := types.ServiceID(service.On, service.Odometer)

		_, getErr := GetService(service.Vehicle, id)
		switch {
		case getErr == nil:
			updated++

		case errors.Is(getErr, ServiceNotFoundErr):
			added++

		default:
			return added, updated, getErr
		}

		if _, err := SaveService(service, "", by); err != nil {
			return added, updated, err
		}
	}

	return added, updated, nil
}

// ------------------------------------------------------------- tolerance ----

// tolerateFieldMismatch turns the one error worth ignoring into no error. An
// entity written by an older version of this binary, or by hand in the console,
// can carry properties the struct has no field for. Everything the struct does
// know about loaded fine.
func tolerateFieldMismatch(err error) error {
	if err == nil || isFieldMismatch(err) {
		return nil
	}

	// GetAll reports per-entity problems as a MultiError, which doesn't unwrap.
	var multi datastore.MultiError
	if errors.As(err, &multi) {
		for _, single := range multi {
			if single != nil && !isFieldMismatch(single) {
				return err
			}
		}
		return nil
	}

	return err
}

func isFieldMismatch(err error) bool {
	var mismatch *datastore.ErrFieldMismatch
	return errors.As(err, &mismatch)
}
