import { Node, Edge, MarkerType } from 'reactflow'

interface Column {
  name: string
  type: string
  isNotNull: boolean
  default?: string
  isPrimaryKey: boolean
  isForeignKey: boolean
  references?: {
    table: string
    column: string
  }
  enumValues?: string[]
}

interface Table {
  name: string
  schema: string
  columns: Column[]
}

export interface SchemaData {
  tables: Table[]
  foreignKeys: Array<{
    fromTable: string
    fromColumn: string
    toTable: string
    toColumn: string
  }>
}

const parseCreateTable = (sql: string): Table => {
  console.log('Parsing CREATE TABLE statement:', sql.slice(0, 100) + '...')

  const tableMatch = sql.match(/CREATE TABLE (?:"?(\w+)"?\.)?\"?(\w+)\"?\s*\(/i)
  if (!tableMatch) {
    console.error('Failed to match CREATE TABLE pattern:', sql)
    throw new Error('Invalid CREATE TABLE statement')
  }

  const [, schema = 'public', name] = tableMatch
  console.log('Found table:', schema + '.' + name)

  const columns: Column[] = []

  // Extract column definitions
  const columnSection = sql.slice(sql.indexOf('(') + 1, sql.lastIndexOf(')'))
  const columnLines = columnSection.split(',\n').map(line => line.trim())

  console.log('Processing columns:', columnLines.length, 'lines')
  for (const line of columnLines) {
    if (!line || line.startsWith('CONSTRAINT')) continue

    const colMatch = line.match(/^"?(\w+)"?\s+([^,]+?)(?:\s+REFERENCES\s+(?:"?(\w+)"?\.)?\"?(\w+)\"?\s*\((\w+)\)|)\s*$/i)
    if (!colMatch) {
      console.log('Skipping line (no match):', line)
      continue
    }

    const [, colName, colType, refSchema = 'public', refTable, refColumn] = colMatch
    const hasReferences = line.toLowerCase().includes('references')

    columns.push({
      name: colName,
      type: colType.split(' ')[0],
      isNotNull: line.toLowerCase().includes('not null'),
      isPrimaryKey: line.toLowerCase().includes('primary key'),
      isForeignKey: hasReferences,
      references: hasReferences ? {
        table: refTable,
        column: refColumn
      } : undefined,
      default: line.toLowerCase().includes('default')
        ? line.match(/default\s+([^,\s]+)/i)?.[1]
        : undefined
    })
    console.log('Added column:', colName, 'type:', colType.split(' ')[0],
      hasReferences ? `references ${refSchema}.${refTable}(${refColumn})` : '')
  }

  return { name, schema, columns }
}

const parseForeignKeys = (sql: string): SchemaData['foreignKeys'] => {
  console.log('Parsing foreign keys...')
  const foreignKeys: SchemaData['foreignKeys'] = []
  const fkRegex = /ALTER TABLE (?:ONLY\s+)?(?:"?(\w+)"?\.)?\"?(\w+)\"?\s+ADD CONSTRAINT \w+ FOREIGN KEY \((\w+)\) REFERENCES (?:"?(\w+)"?\.)?\"?(\w+)\"? \((\w+)\)/gi

  let match
  while ((match = fkRegex.exec(sql)) !== null) {
    // Log the full match for debugging
    console.log('Full regex match:', match)

    // Correct array destructuring - match[0] is full match, then capture groups
    const [fullMatch, fromSchema, fromTable, fromColumn, toSchema, toTable, toColumn] = match
    console.log('Extracted values:', { fromSchema, fromTable, fromColumn, toSchema, toTable, toColumn })

    const fk = {
      fromTable: fromTable,
      fromColumn: fromColumn,
      toTable: toTable,
      toColumn: toColumn
    }
    foreignKeys.push(fk)
    console.log('Added foreign key:', JSON.stringify(fk, null, 2))
  }

  console.log('Total foreign keys found:', foreignKeys.length)
  return foreignKeys
}

const parseEnumValues = (sql: string): Map<string, string[]> => {
  const enums = new Map<string, string[]>()
  // First find all CREATE TYPE statements
  const createTypeMatches = sql.match(/CREATE TYPE (?:"?(\w+)"?\.)?\"?(\w+)\"? AS ENUM\s*\(([\s\S]*?)\)/gi)

  if (createTypeMatches) {
    createTypeMatches.forEach(match => {
      // Extract schema, name and values for each enum
      const parts = match.match(/CREATE TYPE (?:"?(\w+)"?\.)?\"?(\w+)\"? AS ENUM\s*\(([\s\S]*?)\)/i)
      if (parts) {
        const [, schema = 'public', name, valuesStr] = parts
        // Parse the enum values, handling quoted strings and multi-line definitions
        const values = valuesStr
          .split(',')
          .map(v => v.trim())
          .filter(Boolean)
          .map(v => v.replace(/^'|'$/g, '').replace(/\\'/g, "'"))

        const fullName = `${schema}.${name}`
        enums.set(name, values)
        enums.set(fullName, values) // Store both with and without schema qualification
        console.log('Found enum:', fullName, 'with values:', values)
      }
    })
  }

  return enums
}

export const parseSQLSchema = (sql: string): SchemaData => {
  console.log('Starting SQL schema parsing...')
  console.log('SQL content length:', sql.length)

  // Parse enums first
  const enums = parseEnumValues(sql)
  console.log('Found enums:', enums)

  const tables: Table[] = []
  const statements = sql.split(';').map(s => s.trim()).filter(Boolean)
  console.log('Found', statements.length, 'SQL statements')

  // First pass: Create tables and parse column-defined foreign keys
  for (const stmt of statements) {
    if (stmt.toUpperCase().startsWith('CREATE TABLE')) {
      try {
        const table = parseCreateTable(stmt)
        // Add enum values to columns that are enum types
        table.columns.forEach(col => {
          // Try both with and without schema qualification
          const typeWithoutSchema = col.type.replace(/^(?:"?\w+"?\.)?/, '')
          const typeWithSchema = col.type.includes('.') ? col.type : `public.${col.type}`

          if (enums.has(typeWithoutSchema)) {
            col.enumValues = enums.get(typeWithoutSchema)
            console.log(`Found enum type for column ${col.name}: ${typeWithoutSchema} with values:`, enums.get(typeWithoutSchema))
          } else if (enums.has(typeWithSchema)) {
            col.enumValues = enums.get(typeWithSchema)
            console.log(`Found enum type for column ${col.name}: ${typeWithSchema} with values:`, enums.get(typeWithSchema))
          }
        })
        tables.push(table)
      } catch (err) {
        console.error('Error parsing table:', err)
        console.error('Statement:', stmt)
      }
    }
  }

  console.log('Found', tables.length, 'tables')

  // Parse ALTER TABLE foreign keys
  const alterForeignKeys = parseForeignKeys(sql)

  // Update table columns with foreign key information from ALTER TABLE statements
  alterForeignKeys.forEach(fk => {
    const table = tables.find(t => t.name === fk.fromTable)
    if (table) {
      const column = table.columns.find(c => c.name === fk.fromColumn)
      if (column) {
        column.isForeignKey = true
        column.references = {
          table: fk.toTable,
          column: fk.toColumn
        }
        console.log(`Updated foreign key for ${fk.fromTable}.${fk.fromColumn} -> ${fk.toTable}.${fk.toColumn}`)
      }
    }
  })

  // Collect all foreign keys (both from columns and ALTER TABLE statements)
  const foreignKeys: SchemaData['foreignKeys'] = []
  tables.forEach(table => {
    table.columns.forEach(column => {
      if (column.isForeignKey && column.references) {
        foreignKeys.push({
          fromTable: table.name,
          fromColumn: column.name,
          toTable: column.references.table,
          toColumn: column.references.column
        })
      }
    })
  })

  console.log('Total foreign keys found:', foreignKeys.length)
  return { tables, foreignKeys }
}

export const createReactFlowElements = (schemaData: SchemaData): { nodes: Node[], edges: Edge[] } => {
  console.log('Creating React Flow elements...')
  console.log('Processing', schemaData.tables.length, 'tables')

  const nodes: Node[] = []
  const edges: Edge[] = []

  // Calculate positions for tables in a grid layout
  const gridSize = Math.ceil(Math.sqrt(schemaData.tables.length))
  const horizontalSpacing = 500
  const verticalSpacing = 700  // Increased from 400 to 700 to prevent overlapping

  // Calculate total width and height needed
  const totalWidth = gridSize * horizontalSpacing
  const totalHeight = Math.ceil(schemaData.tables.length / gridSize) * verticalSpacing

  // Calculate starting position to center the grid
  const startX = -totalWidth / 2 + horizontalSpacing / 2
  const startY = -totalHeight / 2 + verticalSpacing / 2

  // Sort tables by name for consistent layout
  const sortedTables = [...schemaData.tables].sort((a, b) => a.name.localeCompare(b.name))

  // First pass: calculate max height for each row
  const rowHeights = new Array(Math.ceil(sortedTables.length / gridSize)).fill(0)
  sortedTables.forEach((table, index) => {
    const row = Math.floor(index / gridSize)
    const tableHeight = table.columns.length * 30 + 100  // Estimate height based on number of columns
    rowHeights[row] = Math.max(rowHeights[row], tableHeight)
  })

  // Calculate cumulative row heights
  const cumulativeHeights = rowHeights.reduce((acc, height, index) => {
    acc[index] = (index === 0 ? 0 : acc[index - 1]) + height + 100  // Add 100px padding between rows
    return acc
  }, new Array(rowHeights.length).fill(0))

  // Second pass: position nodes using calculated heights
  sortedTables.forEach((table, index) => {
    const row = Math.floor(index / gridSize)
    const col = index % gridSize

    // Add smaller random offset to prevent perfect alignment
    const randomOffset = {
      x: (Math.random() - 0.5) * 50,
      y: (Math.random() - 0.5) * 20  // Reduced vertical randomness
    }

    const position = {
      x: startX + col * horizontalSpacing + randomOffset.x,
      y: startY + cumulativeHeights[row] + randomOffset.y
    }

    const nodeId = table.name

    nodes.push({
      id: nodeId,
      type: 'tableNode',
      position,
      data: { table },
      style: { zIndex: 1 }
    })
  })

  // Create edges for foreign key relationships
  schemaData.foreignKeys.forEach((fk, index) => {
    edges.push({
      id: `${fk.fromTable}-${fk.fromColumn}-${fk.toTable}-${fk.toColumn}`,
      source: fk.fromTable,
      target: fk.toTable,
      hidden: true,
      style: {
        opacity: 0,
        strokeWidth: 0
      },
      type: 'smoothstep',
      animated: false,
      markerEnd: undefined,
      data: {
        fromColumn: fk.fromColumn,
        toColumn: fk.toColumn
      }
    })
  })

  console.log('Created', nodes.length, 'nodes and', edges.length, 'edges')
  return { nodes, edges }
}
