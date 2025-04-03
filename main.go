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

type ForeignKey struct {
	SQL           string
	FromTable     string
	FromSchema    string
	ToTable       string
	ToSchema      string
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

	// Rewind file for parsing foreign keys
	file.Seek(0, 0)
	foreignKeys, err := parseForeignKeys(file)
	if err != nil {
		fmt.Printf("Error parsing foreign keys: %v\n", err)
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

		// Find relevant foreign keys
		relevantFKs := findRelevantForeignKeys(filteredTables, relatedTables, whitelistTables, foreignKeys)
		fmt.Printf("\nFound %d foreign key constraints for these tables:\n", len(relevantFKs))
		for _, fk := range relevantFKs {
			fmt.Printf("%s.%s -> %s.%s\n", fk.FromSchema, fk.FromTable, fk.ToSchema, fk.ToTable)
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

		// Write foreign key constraints
		if len(relevantFKs) > 0 {
			fmt.Fprint(out, "\n-- Foreign key constraints\n")
			for _, fk := range relevantFKs {
				fmt.Fprintln(out, fk.SQL)
			}
		}

		fmt.Printf("\nWrote %d tables, %d enum types, and %d foreign key constraints to %s\n",
			len(filteredTables)+len(relatedTables),
			len(usedEnums),
			len(relevantFKs),
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

func parseForeignKeys(file *os.File) ([]ForeignKey, error) {
	var foreignKeys []ForeignKey
	scanner := bufio.NewScanner(file)

	// Pattern to match ALTER TABLE ... DROP CONSTRAINT ... fk_rails_...
	dropPattern := regexp.MustCompile(`ALTER TABLE IF EXISTS ONLY ([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+) DROP CONSTRAINT IF EXISTS (fk_rails_[a-zA-Z0-9_]+);`)

	// Map to store constraint names and their corresponding tables
	constraintMap := make(map[string]string)

	// First pass: collect all constraint names and their tables
	for scanner.Scan() {
		line := scanner.Text()
		if matches := dropPattern.FindStringSubmatch(line); len(matches) == 4 {
			tableName := fmt.Sprintf("%s.%s", matches[1], matches[2])
			constraintName := matches[3]
			constraintMap[constraintName] = tableName
		}
	}

	// Rewind file for second pass
	file.Seek(0, 0)
	scanner = bufio.NewScanner(file)

	// Pattern to match REFERENCES in ALTER TABLE statements
	refPattern := regexp.MustCompile(`REFERENCES ([a-zA-Z0-9_]+)\.([a-zA-Z0-9_]+)`)

	// Second pass: find the actual foreign key definitions
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "ADD CONSTRAINT") && strings.Contains(line, "FOREIGN KEY") {
			// Extract constraint name
			parts := strings.Split(line, "ADD CONSTRAINT")
			if len(parts) != 2 {
				continue
			}
			constraintPart := strings.TrimSpace(parts[1])
			constraintName := strings.Split(constraintPart, " ")[0]

			// Get the source table from our constraint map
			if fromTable, ok := constraintMap[constraintName]; ok {
				fromParts := strings.Split(fromTable, ".")
				if len(fromParts) != 2 {
					continue
				}

				// Extract the referenced table
				refMatches := refPattern.FindStringSubmatch(line)
				if len(refMatches) == 3 {
					fk := ForeignKey{
						SQL:        line,
						FromSchema: fromParts[0],
						FromTable:  fromParts[1],
						ToSchema:   refMatches[1],
						ToTable:    refMatches[2],
					}
					foreignKeys = append(foreignKeys, fk)
				}
			}
		}
	}

	return foreignKeys, scanner.Err()
}

func findRelevantForeignKeys(filteredTables []Table, relatedTables []Table, whitelistTables []string, allForeignKeys []ForeignKey) []ForeignKey {
	relevantFKs := make(map[string]ForeignKey) // Use map to avoid duplicates

	// Helper function to check if a table is whitelisted
	isWhitelisted := func(schema, name string) bool {
		for _, whitelist := range whitelistTables {
			if strings.EqualFold(name, whitelist) {
				return true
			}
		}
		return false
	}

	// Helper function to check if a table is in a list
	isTableInList := func(schema, name string, tables []Table) bool {
		for _, t := range tables {
			if strings.EqualFold(t.Schema, schema) && strings.EqualFold(t.Name, name) {
				return true
			}
		}
		return false
	}

	for _, fk := range allForeignKeys {
		// Include FK if:
		// 1. From filtered table to any table
		// 2. From any table to filtered table
		// 3. From whitelisted table to any table
		// 4. From any table to whitelisted table
		fromFiltered := isTableInList(fk.FromSchema, fk.FromTable, filteredTables)
		toFiltered := isTableInList(fk.ToSchema, fk.ToTable, filteredTables)
		fromWhitelisted := isWhitelisted(fk.FromSchema, fk.FromTable)
		toWhitelisted := isWhitelisted(fk.ToSchema, fk.ToTable)

		if fromFiltered || toFiltered || fromWhitelisted || toWhitelisted {
			relevantFKs[fk.SQL] = fk
		}
	}

	var result []ForeignKey
	for _, fk := range relevantFKs {
		result = append(result, fk)
	}
	return result
}
