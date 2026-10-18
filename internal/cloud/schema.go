package data

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Tables is the CREATE TABLE statements for the tables pillar.json declares,
// as the host makes them.
//
// On the host the deploy makes the tables, before the container starts, and
// this is never asked. It is for a database that nothing deployed: the tests'
// temporary files, and tools/localdb's for running the service on a desk. Both
// read the same pillar.json the deploy sends, so the tables they get are the
// tables the host has, and a column the code wants that the file does not
// declare fails a test rather than a deploy.
func Tables(pillar []byte) ([]string, error) {
	var file struct {
		Workload struct {
			Database struct {
				Tables map[string]struct {
					Columns []struct {
						Name       string          `json:"name"`
						Type       string          `json:"type"`
						NotNull    bool            `json:"not_null"`
						Default    json.RawMessage `json:"default"`
						References string          `json:"references"`
						OnDelete   string          `json:"on_delete"`
					} `json:"columns"`
					PrimaryKey []string `json:"primary_key"`
					Indexes    map[string]struct {
						Columns []string `json:"columns"`
						Unique  bool     `json:"unique"`
					} `json:"indexes"`
				} `json:"tables"`
			} `json:"database"`
		} `json:"workload"`
	}
	if err := json.Unmarshal(pillar, &file); err != nil {
		return nil, err
	}

	tables := file.Workload.Database.Tables
	if len(tables) == 0 {
		return nil, fmt.Errorf("pillar.json declares no tables")
	}

	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)

	var statements []string
	for _, name := range names {
		table := tables[name]

		var parts []string
		for _, column := range table.Columns {
			part := fmt.Sprintf("%q %s", column.Name, strings.ToUpper(column.Type))
			if column.NotNull {
				part += " NOT NULL"
			}
			if len(column.Default) > 0 {
				literal, err := defaultLiteral(column.Default)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", name, column.Name, err)
				}
				part += " DEFAULT " + literal
			}
			if column.References != "" {
				// A reference is written "table.column" in the config, and the
				// table it names is made in this same run - alphabetically,
				// which may be after this one. SQLite resolves a foreign key
				// when a row is written rather than when the table is made, so
				// the order the statements come out in does not matter.
				reference, err := referenceClause(column.References, column.OnDelete)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", name, column.Name, err)
				}
				part += reference
			}
			parts = append(parts, part)
		}

		if len(table.PrimaryKey) > 0 {
			parts = append(parts, "PRIMARY KEY ("+strings.Join(quoted(table.PrimaryKey), ", ")+")")
		}

		statements = append(statements, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %q (%s) STRICT", name, strings.Join(parts, ", ")))

		// The indexes are made too, in the order of their names, so a query the
		// host answers out of an index is answered out of one here as well.
		indexNames := make([]string, 0, len(table.Indexes))
		for index := range table.Indexes {
			indexNames = append(indexNames, index)
		}
		sort.Strings(indexNames)

		for _, index := range indexNames {
			unique := ""
			if table.Indexes[index].Unique {
				unique = "UNIQUE "
			}

			statements = append(statements, fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %q ON %q (%s)",
				unique, index, name, strings.Join(quoted(table.Indexes[index].Columns), ", ")))
		}
	}

	return statements, nil
}

func quoted(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = fmt.Sprintf("%q", name)
	}

	return out
}

// referenceClause is a column's "table.column" reference as SQL, with the
// on-delete rule the config asked for. An unknown rule is an error rather than
// a clause quietly left off, because the clause is the thing doing the work:
// a fill-up's reference to its vehicle is what deletes it with the car.
func referenceClause(reference string, onDelete string) (string, error) {
	table, column, found := strings.Cut(reference, ".")
	if !found || table == "" || column == "" {
		return "", fmt.Errorf("a reference has to be written \"table.column\", not %q", reference)
	}

	clause := fmt.Sprintf(" REFERENCES %q(%q)", table, column)

	switch strings.ToLower(strings.TrimSpace(onDelete)) {
	case "":
	case "cascade":
		clause += " ON DELETE CASCADE"
	case "set null":
		clause += " ON DELETE SET NULL"
	case "restrict":
		clause += " ON DELETE RESTRICT"
	default:
		return "", fmt.Errorf("on_delete has to be cascade, set null or restrict, not %q", onDelete)
	}

	return clause, nil
}

// defaultLiteral is a column's json default as SQL: a string quoted, a number
// as it is.
func defaultLiteral(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return "'" + strings.ReplaceAll(text, "'", "''") + "'", nil
	}

	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String(), nil
	}

	return "", fmt.Errorf("a default has to be a string or a number, not %s", raw)
}
