package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type Table struct {
	Name    string
	Schema  string
	SQL     string
}

type EnumType struct {
	Name    string
	Schema  string
	SQL     string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run script.go structure.sql [table_prefix] [whitelist_table1] [whitelist_table2] ...")
		os.Exit(1)
	}

	inputFile := os.Args[1]
	tablePrefix := ""
	var whitelistTables []string
	if len(os.Args) > 2 {
		tablePrefix = os.Args[2]
		if len(os.Args) > 3 {
			whitelistTables = os.Args[3:]
		}
	}

	// Open the input file
	file, err := os.Open(inputFile)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Parse all tables from the file
	tables, err := parseTables(file)
	if err != nil {
		fmt.Printf("Error parsing tables: %v\n", err)
		os.Exit(1)
	}

	// Rewind file for parsing enums
	file.Seek(0, 0)
	enums, err := parseEnums(file)
	if err != nil {
		fmt.Printf("Error parsing enums: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d total tables\n", len(tables))

	if tablePrefix != "" {
		// Filter tables by prefix
		filteredTables := filterTablesByPrefix(tables, tablePrefix)
		fmt.Printf("\nFound %d tables with prefix '%s':\n", len(filteredTables), tablePrefix)
		for _, table := range filteredTables {
			fmt.Printf("%s.%s\n", table.Schema, table.Name)
		}

		// Find related tables
		relatedTables := findRelatedTables(filteredTables, tables)
		fmt.Printf("\nFound %d related tables:\n", len(relatedTables))
		for _, table := range relatedTables {
			fmt.Printf("%s.%s\n", table.Schema, table.Name)
		}

		// Find enums used by filtered tables and whitelisted related tables
		var tablesToCheckForEnums []Table
		tablesToCheckForEnums = append(tablesToCheckForEnums, filteredTables...)

		// Add only whitelisted related tables
		for _, table := range relatedTables {
			for _, whitelist := range whitelistTables {
				if strings.EqualFold(table.Name, whitelist) {
					tablesToCheckForEnums = append(tablesToCheckForEnums, table)
					break
				}
			}
		}

		usedEnums := findUsedEnums(tablesToCheckForEnums, enums)
		fmt.Printf("\nFound %d enum types used by filtered tables and whitelisted related tables:\n", len(usedEnums))
		for _, enum := range usedEnums {
			fmt.Printf("%s.%s\n", enum.Schema, enum.Name)
		}

		// Write filtered and related tables to output file
		outputFile := "filtered_tables.sql"
		out, err := os.Create(outputFile)
		if err != nil {
			fmt.Printf("Error creating output file: %v\n", err)
			os.Exit(1)
		}
		defer out.Close()

		// Write enum definitions first
		if len(usedEnums) > 0 {
			fmt.Fprint(out, "-- Enum type definitions\n")
			for _, enum := range usedEnums {
				fmt.Fprint(out, enum.SQL)
			}
			fmt.Fprint(out, "\n")
		}

		// Write filtered tables
		fmt.Fprint(out, "-- Tables with prefix\n")
		for _, table := range filteredTables {
			fmt.Fprint(out, table.SQL)
		}

		// Write related tables
		fmt.Fprint(out, "\n-- Related tables\n")
		for _, table := range relatedTables {
			isWhitelisted := false
			for _, whitelist := range whitelistTables {
				if strings.EqualFold(table.Name, whitelist) {
					isWhitelisted = true
					break
				}
			}

			if isWhitelisted {
				fmt.Fprintf(out, "\n-- Full definition for whitelisted table\n")
				fmt.Fprint(out, table.SQL)
			} else {
				createTableLine := regexp.MustCompile(`(?m)^CREATE TABLE.*?\(`).FindString(table.SQL)
				idLine := regexp.MustCompile(`(?m)^\s*id\s+[^,]+`).FindString(table.SQL)
				if createTableLine != "" && idLine != "" {
					fmt.Fprintf(out, "%s\n    %s\n);\n\n", createTableLine, idLine)
				}
			}
		}

		fmt.Printf("\nWrote %d tables and %d enum types to %s\n",
			len(filteredTables)+len(relatedTables),
			len(usedEnums),
			outputFile)
		if len(whitelistTables) > 0 {
			fmt.Printf("Included full definitions for whitelisted tables: %v\n", whitelistTables)
		}
	}
}

func findRelatedTables(filteredTables []Table, allTables []Table) []Table {
	relatedMap := make(map[string]Table) // Use map to avoid duplicates

	// Pattern to match column definitions - looking for columns ending in _id
	columnPattern := regexp.MustCompile(`(?i)\s*([a-zA-Z0-9_]+)\s+[a-zA-Z0-9_\(\),\s]+`)

	// First pass: find tables that our filtered tables reference
	for _, table := range filteredTables {
		scanner := bufio.NewScanner(strings.NewReader(table.SQL))
		for scanner.Scan() {
			line := scanner.Text()
			matches := columnPattern.FindStringSubmatch(line)
			if len(matches) > 1 {
				columnName := strings.ToLower(matches[1])
				if strings.HasSuffix(columnName, "_id") {
					// Extract the table name from the column name by removing _id
					referencedTableBase := strings.TrimSuffix(columnName, "_id")

					// Try both singular and plural forms
					singularName := referencedTableBase
					pluralName := referencedTableBase + "s"

					// Look for matching tables
					for _, otherTable := range allTables {
						tableName := strings.ToLower(otherTable.Name)
						if tableName == singularName || tableName == pluralName {
							if !isTableInList(otherTable, filteredTables) {
								relatedMap[otherTable.Schema+"."+otherTable.Name] = otherTable
								fmt.Printf("DEBUG: Found related table %s referenced by column %s in table %s\n",
									otherTable.Name, columnName, table.Name)
							}
						}
					}
				}
			}
		}
	}

	// Second pass: find tables that reference our filtered tables
	for _, table := range filteredTables {
		// Get singular form of the table name
		singularName := strings.TrimSuffix(table.Name, "s")

		// Look through all tables for columns referencing this table
		for _, otherTable := range allTables {
			// Skip if it's one of our filtered tables or already in related tables
			if isTableInList(otherTable, filteredTables) || relatedMap[otherTable.Schema+"."+otherTable.Name].Name != "" {
				continue
			}

			// Scan the table SQL for column definitions
			scanner := bufio.NewScanner(strings.NewReader(otherTable.SQL))
			for scanner.Scan() {
				line := scanner.Text()
				matches := columnPattern.FindStringSubmatch(line)
				if len(matches) > 1 {
					columnName := strings.ToLower(matches[1])
					expectedPattern := strings.ToLower(singularName + "_id")

					if columnName == expectedPattern {
						relatedMap[otherTable.Schema+"."+otherTable.Name] = otherTable
						fmt.Printf("DEBUG: Found related table %s due to column %s referencing %s\n",
							otherTable.Name, columnName, table.Name)
					}
				}
			}
		}
	}

	// Convert map to slice
	var relatedTables []Table
	for _, table := range relatedMap {
		relatedTables = append(relatedTables, table)
	}

	return relatedTables
}

func isTableInList(table Table, list []Table) bool {
	for _, t := range list {
		if t.Schema == table.Schema && t.Name == table.Name {
			return true
		}
	}
	return false
}

func filterTablesByPrefix(tables []Table, prefix string) []Table {
	var filtered []Table
	prefix = strings.ToLower(prefix)

	for _, table := range tables {
		tableName := strings.ToLower(table.Name)
		// Only match tables that start with exactly "submissions_"
		if strings.HasPrefix(tableName, prefix+"_") {
			filtered = append(filtered, table)
		}
	}

	return filtered
}

func parseTables(file *os.File) ([]Table, error) {
	var tables []Table

	// Pattern to match CREATE TABLE statements
	tablePattern := regexp.MustCompile(`(?i)CREATE TABLE\s+"?(?:([a-zA-Z0-9_]+)\.)?([a-zA-Z0-9_]+)"?\s*\(`)
	createTablePattern := regexp.MustCompile(`(?i)^CREATE TABLE`)

	scanner := bufio.NewScanner(file)
	var currentTable *Table
	depth := 0

	for scanner.Scan() {
		line := scanner.Text()

		if currentTable == nil {
			// Look for start of CREATE TABLE
			if createTablePattern.MatchString(line) {
				matches := tablePattern.FindStringSubmatch(line)
				if len(matches) > 2 {
					schema := "public"
					if matches[1] != "" {
						schema = matches[1]
					}
					currentTable = &Table{
						Schema: schema,
						Name:   matches[2],
						SQL:    line + "\n",
					}
					depth = strings.Count(line, "(") - strings.Count(line, ")")
				}
			}
		} else {
			// Already inside a CREATE TABLE block
			currentTable.SQL += line + "\n"
			depth += strings.Count(line, "(") - strings.Count(line, ")")

			// Check if we've reached the end of the CREATE TABLE
			if depth <= 0 && strings.Contains(line, ");") {
				tables = append(tables, *currentTable)
				currentTable = nil
				depth = 0
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %v", err)
	}

	return tables, nil
}

func parseEnums(file *os.File) ([]EnumType, error) {
	var enums []EnumType
	scanner := bufio.NewScanner(file)
	var currentEnum *EnumType

	for scanner.Scan() {
		line := scanner.Text()

		if currentEnum == nil {
			if strings.Contains(line, "CREATE TYPE") && strings.Contains(line, "AS ENUM") {
				// Extract schema and name from CREATE TYPE public.some_enum_type AS ENUM
				parts := strings.Split(strings.Split(line, "AS ENUM")[0], ".")
				if len(parts) == 2 {
					schema := strings.TrimSpace(strings.Split(parts[0], "TYPE")[1])
					name := strings.TrimSpace(parts[1])
					currentEnum = &EnumType{
						Schema: schema,
						Name:   name,
						SQL:    line + "\n",
					}
				}
			}
		} else {
			currentEnum.SQL += line + "\n"
			if strings.Contains(line, ");") {
				enums = append(enums, *currentEnum)
				currentEnum = nil
			}
		}
	}

	return enums, scanner.Err()
}

func findUsedEnums(tables []Table, allEnums []EnumType) []EnumType {
	usedEnums := make(map[string]EnumType)

	// Create a pattern that matches schema.type_name that appears after a type declaration
	enumPattern := regexp.MustCompile(`(?i)public\.[a-zA-Z0-9_]+\b(?:\s+(?:DEFAULT\s+[^:]+::|NOT\s+NULL))?`)

	for _, table := range tables {
		matches := enumPattern.FindAllString(table.SQL, -1)
		for _, match := range matches {
			// Clean up the match to get just the type name
			typeName := strings.TrimSpace(strings.Split(match, "DEFAULT")[0])
			typeName = strings.TrimSpace(strings.Split(typeName, "NOT NULL")[0])

			for _, enum := range allEnums {
				enumFullName := enum.Schema + "." + enum.Name
				if strings.EqualFold(typeName, enumFullName) {
					usedEnums[enumFullName] = enum
				}
			}
		}
	}

	var result []EnumType
	for _, enum := range usedEnums {
		result = append(result, enum)
	}
	return result
}
