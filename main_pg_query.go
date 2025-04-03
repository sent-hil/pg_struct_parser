package main

import (
	"fmt"
	"os"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v4"
)

type TableDef struct {
	Name        string
	Schema      string
	Columns     []ColumnDef
	Constraints []string
	SQL         string
}

type ColumnDef struct {
	Name       string
	Type       string
	IsNotNull  bool
	Default    string
	Constraint string
}

type EnumDef struct {
	Name   string
	Schema string
	Values []string
	SQL    string
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: go run main.go <sql_file> <table_prefix> [whitelisted_tables...]")
		os.Exit(1)
	}

	sqlFile := os.Args[1]
	tablePrefix := os.Args[2]
	whitelistedTables := os.Args[3:]

	sqlContent, err := os.ReadFile(sqlFile)
	if err != nil {
		fmt.Printf("Error reading SQL file: %v\n", err)
		os.Exit(1)
	}

	result, err := pg_query.Parse(string(sqlContent))
	if err != nil {
		fmt.Printf("Error parsing SQL: %v\n", err)
		os.Exit(1)
	}

	// Map to store original SQL text by statement fingerprint
	originalSQL := make(map[string]string)
	lines := strings.Split(string(sqlContent), "\n")
	var currentStmt []string
	inStatement := false

	// First pass: collect original SQL text
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "CREATE TYPE") || strings.HasPrefix(trimmed, "CREATE TABLE") {
			if len(currentStmt) > 0 {
				stmtText := strings.Join(currentStmt, "\n")
				// Extract the name from the CREATE statement
				parts := strings.Fields(currentStmt[0])
				if len(parts) >= 3 {
					name := strings.TrimSuffix(parts[2], " (")
					originalSQL[name] = stmtText + "\n"
				}
			}
			currentStmt = []string{line}
			inStatement = true
			continue
		}

		if inStatement {
			currentStmt = append(currentStmt, line)
			if strings.HasSuffix(trimmed, ";") {
				stmtText := strings.Join(currentStmt, "\n")
				// Extract the name from the CREATE statement
				parts := strings.Fields(currentStmt[0])
				if len(parts) >= 3 {
					name := strings.TrimSuffix(parts[2], " (")
					originalSQL[name] = stmtText + "\n"
				}
				currentStmt = nil
				inStatement = false
			}
		}
	}

	var tables []TableDef
	var allEnums []EnumDef
	var foreignKeys []string

	// Second pass: collect all tables and enums
	var filteredTableNames []string // Track filtered table names for FK filtering
	for _, stmt := range result.Stmts {
		rawStmt := stmt.GetStmt()
		if rawStmt == nil {
			continue
		}

		switch node := rawStmt.Node.(type) {
		case *pg_query.Node_CreateStmt:
			tableName := getTableName(node.CreateStmt.Relation)
			if strings.HasPrefix(tableName, fmt.Sprintf("public.%s_", tablePrefix)) || contains(whitelistedTables, strings.TrimPrefix(tableName, "public.")) {
				table := processCreateTable(node.CreateStmt)
				if sql, ok := originalSQL[tableName]; ok {
					table.SQL = sql
				}
				tables = append(tables, table)
				filteredTableNames = append(filteredTableNames, tableName)
				fmt.Printf("Added table: %s\n", tableName)
			}
		case *pg_query.Node_CreateEnumStmt:
			enum := processCreateEnum(node.CreateEnumStmt)
			enumName := fmt.Sprintf("%s.%s", enum.Schema, enum.Name)
			if sql, ok := originalSQL[enumName]; ok {
				enum.SQL = sql
			}
			allEnums = append(allEnums, enum)
		case *pg_query.Node_AlterTableStmt:
			if fk := getForeignKey(node.AlterTableStmt); fk != "" {
				// Extract source and target tables from the FK constraint
				sourceTable := getTableName(node.AlterTableStmt.Relation)
				targetTable := ""
				if constraint := node.AlterTableStmt.Cmds[0].GetNode().(*pg_query.Node_AlterTableCmd); constraint != nil {
					if def := constraint.AlterTableCmd.GetDef(); def != nil {
						if con := def.GetNode().(*pg_query.Node_Constraint); con != nil {
							if pktable := con.Constraint.GetPktable(); pktable != nil {
								targetTable = getTableName(pktable)
							}
						}
					}
				}

				// Only include FK if either source or target is in our filtered tables
				if contains(filteredTableNames, sourceTable) || contains(filteredTableNames, targetTable) {
					foreignKeys = append(foreignKeys, fk)
				}
			}
		}
	}

	// Find enums used by our tables
	var usedEnums []EnumDef
	enumMap := make(map[string]EnumDef)
	for _, enum := range allEnums {
		enumMap[enum.Name] = enum // Map by just the enum name, not schema.name
	}

	for _, table := range tables {
		for _, col := range table.Columns {
			// Get the base type name without any array brackets or modifiers
			typeName := strings.Split(col.Type, "(")[0]                                  // Remove any type modifiers
			typeName = strings.TrimSuffix(typeName, "[]")                                // Remove array notation
			typeName = strings.Split(typeName, ".")[len(strings.Split(typeName, "."))-1] // Get last part after dot

			if enum, ok := enumMap[typeName]; ok {
				// Check if we already added this enum
				found := false
				for _, used := range usedEnums {
					if used.Name == enum.Name && used.Schema == enum.Schema {
						found = true
						break
					}
				}
				if !found {
					usedEnums = append(usedEnums, enum)
					fmt.Printf("Found enum type %s.%s used by column %s.%s.%s\n",
						enum.Schema, enum.Name, table.Schema, table.Name, col.Name)
				}
			}
		}
	}

	fmt.Printf("\nFound %d tables with prefix '%s'\n", len(tables), tablePrefix)
	for _, table := range tables {
		fmt.Printf("  %s.%s\n", table.Schema, table.Name)
	}
	fmt.Printf("Found %d used enums\n", len(usedEnums))
	fmt.Printf("Found %d foreign keys\n", len(foreignKeys))

	// Write filtered tables and enums to output file
	outputFile := "filtered_tables_pg_query.sql"
	f, err := os.Create(outputFile)
	if err != nil {
		fmt.Printf("Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	// Write enum definitions
	for _, enum := range usedEnums {
		fmt.Fprintln(f, enum.SQL)
	}

	// Write table definitions
	for _, table := range tables {
		fmt.Fprintln(f, table.SQL)
	}

	// Write foreign key constraints
	for _, fk := range foreignKeys {
		fmt.Fprintf(f, "%s\n", fk)
	}
}

func getStatementFingerprint(sql string) string {
	// Simple fingerprint - just use the first line which contains the name
	lines := strings.Split(sql, "\n")
	if len(lines) > 0 {
		return lines[0]
	}
	return sql
}

func getTableName(relation *pg_query.RangeVar) string {
	if relation == nil {
		return ""
	}
	schema := relation.Schemaname
	if schema == "" {
		schema = "public"
	}
	return fmt.Sprintf("%s.%s", schema, relation.Relname)
}

func processCreateTable(stmt *pg_query.CreateStmt) TableDef {
	table := TableDef{
		Name:   stmt.Relation.Relname,
		Schema: "public",
	}
	if stmt.Relation.Schemaname != "" {
		table.Schema = stmt.Relation.Schemaname
	}

	for _, element := range stmt.TableElts {
		switch node := element.Node.(type) {
		case *pg_query.Node_ColumnDef:
			col := processColumnDef(node.ColumnDef)
			table.Columns = append(table.Columns, col)
		case *pg_query.Node_Constraint:
			constraint := processConstraint(node.Constraint)
			if constraint != "" {
				table.Constraints = append(table.Constraints, constraint)
			}
		}
	}

	return table
}

func processColumnDef(def *pg_query.ColumnDef) ColumnDef {
	col := ColumnDef{
		Name:      def.Colname,
		IsNotNull: def.IsNotNull,
	}

	// Build the full type string including array brackets and type modifiers
	if def.TypeName != nil {
		// Get the type name parts
		var typeNames []string
		for _, name := range def.TypeName.Names {
			if strNode := name.GetString_(); strNode != nil {
				typeNames = append(typeNames, strNode.GetSval())
			}
		}

		// Join the type names with dots (for schema-qualified types)
		typeName := strings.Join(typeNames, ".")

		// Add any type modifiers (like varchar length)
		if len(def.TypeName.Typmods) > 0 {
			var modifiers []string
			for _, mod := range def.TypeName.Typmods {
				if aConst := mod.GetAConst(); aConst != nil {
					if intVal := aConst.GetIval(); intVal != nil {
						modifiers = append(modifiers, fmt.Sprintf("%d", intVal.GetIval()))
					}
				}
			}
			if len(modifiers) > 0 {
				typeName += fmt.Sprintf("(%s)", strings.Join(modifiers, ", "))
			}
		}

		// Add array brackets if it's an array type
		if def.TypeName.ArrayBounds != nil && len(def.TypeName.ArrayBounds) > 0 {
			typeName += "[]"
		}

		col.Type = typeName
	}

	// Get default value
	if def.RawDefault != nil {
		switch node := def.RawDefault.Node.(type) {
		case *pg_query.Node_String_:
			col.Default = fmt.Sprintf("'%s'", node.String_.GetSval())
		case *pg_query.Node_Integer:
			col.Default = fmt.Sprintf("%d", node.Integer.Ival)
		case *pg_query.Node_Float:
			col.Default = node.Float.GetFval()
		case *pg_query.Node_Boolean:
			if node.Boolean.Boolval {
				col.Default = "true"
			} else {
				col.Default = "false"
			}
		case *pg_query.Node_TypeCast:
			if strNode := node.TypeCast.Arg.GetString_(); strNode != nil {
				col.Default = fmt.Sprintf("'%s'::%s", strNode.GetSval(), getTypeName(node.TypeCast.TypeName))
			}
		}
	}

	// Get column constraints
	for _, constraint := range def.Constraints {
		if constraint.Node != nil {
			switch node := constraint.Node.(type) {
			case *pg_query.Node_Constraint:
				if node.Constraint.Contype == pg_query.ConstrType_CONSTR_PRIMARY {
					col.Constraint = "PRIMARY KEY"
				}
			}
		}
	}

	return col
}

func getTypeName(typeName *pg_query.TypeName) string {
	if typeName == nil || len(typeName.Names) == 0 {
		return ""
	}

	var names []string
	for _, name := range typeName.Names {
		if strNode := name.GetString_(); strNode != nil {
			names = append(names, strNode.GetSval())
		}
	}
	return strings.Join(names, ".")
}

func processConstraint(constraint *pg_query.Constraint) string {
	switch constraint.Contype {
	case pg_query.ConstrType_CONSTR_PRIMARY:
		var keys []string
		for _, key := range constraint.Keys {
			if strNode := key.GetString_(); strNode != nil {
				keys = append(keys, strNode.GetSval())
			}
		}
		if len(keys) > 0 {
			return fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(keys, ", "))
		}
	case pg_query.ConstrType_CONSTR_UNIQUE:
		var keys []string
		for _, key := range constraint.Keys {
			if strNode := key.GetString_(); strNode != nil {
				keys = append(keys, strNode.GetSval())
			}
		}
		if len(keys) > 0 {
			return fmt.Sprintf("UNIQUE (%s)", strings.Join(keys, ", "))
		}
	}
	return ""
}

func processCreateEnum(stmt *pg_query.CreateEnumStmt) EnumDef {
	enum := EnumDef{
		Schema: "public",
	}

	// Get enum name
	if len(stmt.TypeName) > 0 {
		lastNameNode := stmt.TypeName[len(stmt.TypeName)-1]
		if strNode := lastNameNode.GetString_(); strNode != nil {
			enum.Name = strNode.GetSval()
		}
		if len(stmt.TypeName) > 1 {
			if strNode := stmt.TypeName[0].GetString_(); strNode != nil {
				enum.Schema = strNode.GetSval()
			}
		}
	}

	// Get enum values
	for _, val := range stmt.Vals {
		if strNode := val.GetString_(); strNode != nil {
			enum.Values = append(enum.Values, strNode.GetSval())
		}
	}

	return enum
}

func getForeignKey(stmt *pg_query.AlterTableStmt) string {
	if stmt == nil {
		return ""
	}

	for _, cmd := range stmt.Cmds {
		if cmd == nil {
			continue
		}

		alterCmd, ok := cmd.Node.(*pg_query.Node_AlterTableCmd)
		if !ok || alterCmd == nil {
			continue
		}

		if alterCmd.AlterTableCmd.GetSubtype() == pg_query.AlterTableType_AT_AddConstraint {
			def := alterCmd.AlterTableCmd.GetDef()
			if def == nil {
				continue
			}

			constraint, ok := def.Node.(*pg_query.Node_Constraint)
			if !ok || constraint == nil {
				continue
			}

			if constraint.Constraint.GetContype() == pg_query.ConstrType_CONSTR_FOREIGN {
				// Format foreign key constraint
				var fkCols []string
				var pkCols []string

				for _, col := range constraint.Constraint.GetFkAttrs() {
					if strNode := col.GetString_(); strNode != nil {
						fkCols = append(fkCols, strNode.GetSval())
					}
				}

				for _, col := range constraint.Constraint.GetPkAttrs() {
					if strNode := col.GetString_(); strNode != nil {
						pkCols = append(pkCols, strNode.GetSval())
					}
				}

				if len(fkCols) > 0 && len(pkCols) > 0 && constraint.Constraint.GetPktable() != nil {
					return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
						getTableName(stmt.Relation),
						constraint.Constraint.GetConname(),
						strings.Join(fkCols, ", "),
						getTableName(constraint.Constraint.GetPktable()),
						strings.Join(pkCols, ", "))
				}
			}
		}
	}
	return ""
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
