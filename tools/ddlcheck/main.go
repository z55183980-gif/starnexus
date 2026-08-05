package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"

	_ "modernc.org/sqlite"
)

var (
	sqliteSeparator = "`|\"|'|\t"
	tableRegexp     = regexp.MustCompile(fmt.Sprintf(`(?is)(CREATE TABLE [%v]?[\w\d-]+[%v]?)(?:\s*\((.*)\))?`, sqliteSeparator, sqliteSeparator))
	separatorRegexp = regexp.MustCompile(fmt.Sprintf("[%v]", sqliteSeparator))
)

func parseDDL(str string) error {
	if sections := tableRegexp.FindStringSubmatch(str); len(sections) > 0 {
		ddlBody := sections[2]
		ddlBodyRunes := []rune(ddlBody)
		bracketLevel := 0
		var quote rune
		for idx := 0; idx < len(ddlBodyRunes); idx++ {
			var next rune = 0
			c := ddlBodyRunes[idx]
			if idx+1 < len(ddlBodyRunes) {
				next = ddlBodyRunes[idx+1]
			}
			if sc := string(c); separatorRegexp.MatchString(sc) {
				if c == next {
					idx++
				} else if quote > 0 {
					quote = 0
				} else {
					quote = c
				}
			} else if quote == 0 {
				if c == '(' {
					bracketLevel++
				} else if c == ')' {
					bracketLevel--
				}
			}
			if bracketLevel < 0 {
				return errors.New("invalid DDL, unbalanced brackets")
			}
		}
		if bracketLevel != 0 {
			return fmt.Errorf("invalid DDL, unbalanced brackets (level=%d)", bracketLevel)
		}
		return nil
	}
	return errors.New("no match")
}

func main() {
	path := "one-api.db"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name, sql FROM sqlite_master WHERE type='table' AND sql IS NOT NULL ORDER BY name`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	fail := 0
	for rows.Next() {
		var name, sqlText string
		_ = rows.Scan(&name, &sqlText)
		if err := parseDDL(sqlText); err != nil {
			fail++
			fmt.Printf("FAIL %s: %v\nSQL:\n%s\n\n", name, err, sqlText)
		} else {
			fmt.Printf("OK   %s\n", name)
		}
	}
	// Also show defaults that might have CHECK expressions with odd parens
	fmt.Printf("done fails=%d\n", fail)
}
