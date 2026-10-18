// Command localdb makes a database for running the service on a desk: a SQLite
// file with the tables pillar.json declares, made the way the deploy makes them
// on the host. A file that already has them is left as it is.
//
//	go run ./tools/localdb car-tracker.sqlite
//
// It is not part of the service, and the container is not built with it.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	data "github.com/Niximacco/car-tracker/internal/cloud"

	_ "modernc.org/sqlite"
)

func main() {
	pillarPath := flag.String("pillar", "pillar.json", "the pillar.json whose tables to make")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/localdb [-pillar pillar.json] car-tracker.sqlite")
		os.Exit(2)
	}
	path := flag.Arg(0)

	pillar, err := os.ReadFile(*pillarPath)
	if err != nil {
		log.Fatal(err)
	}

	statements, err := data.Tables(pillar)
	if err != nil {
		log.Fatalf("%s: %v", *pillarPath, err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(wal)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			log.Fatalf("%s: %v", statement, err)
		}
	}

	log.Printf("%s has the tables %s declares. Run the service with DATABASE_PATH=%s", path, *pillarPath, path)
}
