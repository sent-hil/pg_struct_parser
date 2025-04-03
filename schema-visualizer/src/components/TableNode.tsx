import { Handle, Position, useReactFlow, NodeToolbar, getRectOfNodes, getViewportForBounds, useStore } from 'reactflow'
import styled from '@emotion/styled'
import { useState, useCallback } from 'react'
import ELK from 'elkjs/lib/elk.bundled'

const elk = new ELK()

interface ElkNode {
  id: string
  x?: number
  y?: number
  width: number
  height: number
  children?: ElkNode[]
}

interface NodeDimensions {
  width: number
  height: number
  x: number
  y: number
}

const NodeContainer = styled.div`
  padding: 10px;
  border-radius: 5px;
  background: white;
  border: 1px solid #ccc;
  box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
  font-family: system-ui, -apple-system, sans-serif;
  min-width: 250px;
  max-width: 400px;
  width: fit-content;
  position: relative;
`

const TableHeader = styled.div`
  padding: 8px;
  background: #f8f8f8;
  border-bottom: 1px solid #ddd;
  font-weight: 500;
  cursor: pointer;

  &:hover {
    background: #f0f0f0;
  }
`

const ColumnList = styled.div`
  margin-top: 8px;
  max-height: 400px;
  overflow-y: auto;
  position: relative;
  min-width: 100%;

  &::-webkit-scrollbar {
    width: 8px;
  }

  &::-webkit-scrollbar-track {
    background: #f1f1f1;
  }

  &::-webkit-scrollbar-thumb {
    background: #888;
    border-radius: 4px;
  }
`

const ColumnItem = styled.div<{ isPrimaryKey: boolean; isForeignKey: boolean }>`
  padding: 4px 12px;
  display: grid;
  grid-template-columns: minmax(120px, 1fr) minmax(120px, auto);
  gap: 12px;
  position: relative;
  border-bottom: 1px solid #eee;
  font-size: 13px;
  cursor: ${props => props.isForeignKey ? 'pointer' : 'default'};

  ${props => props.isPrimaryKey && `
    font-weight: bold;
    background: #f8f9fa;
    &::before {
      content: "🔑";
      font-size: 12px;
      position: absolute;
      left: 2px;
    }
  `}

  ${props => props.isForeignKey && `
    &::after {
      content: "🔗";
      font-size: 12px;
      position: absolute;
      right: 2px;
    }
  `}

  &:hover {
    background: #f5f5f5;
  }

  &:last-child {
    border-bottom: none;
  }
`

interface ColumnNameProps {
  isPrimaryKey: boolean
  isForeignKey: boolean
  title?: string
}

const ColumnName = styled.span<ColumnNameProps>`
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding-left: ${props => props.isPrimaryKey ? '16px' : '0'};
  padding-right: ${props => props.isForeignKey ? '16px' : '0'};

  ${props => props.isForeignKey && `
    cursor: pointer;
    text-decoration: underline dotted;
  `}
`

interface TypeLabelProps {
  isForeignKey: boolean
  isEnum: boolean
}

const TypeWrapper = styled.div`
  position: relative;
  display: inline-block;

  &:hover .enum-tooltip {
    display: block;
  }
`

const TypeLabel = styled.span<TypeLabelProps>`
  color: #666;
  font-size: 0.9em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: right;
  padding-right: ${props => props.isForeignKey ? '16px' : '0'};

  ${props => props.isEnum && `
    cursor: pointer;
    text-decoration: underline dotted;
  `}
`

const EnumTooltip = styled.div`
  background: white;
  border: 1px solid #ccc;
  border-radius: 4px;
  padding: 8px;
  font-size: 12px;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
  max-width: 300px;
  white-space: normal;

  ul {
    margin: 4px 0;
    padding-left: 20px;
    list-style-type: disc;
  }

  li {
    margin: 2px 0;
    color: #666;
  }
`

const HandleContainer = styled.div`
  position: absolute;
  top: 0;
  bottom: 0;
  width: 15px;
  display: flex;
  flex-direction: column;
  justify-content: space-around;
  gap: 4px;
  z-index: 1;
`

const LeftHandleContainer = styled(HandleContainer)`
  left: -15px;
`

const RightHandleContainer = styled(HandleContainer)`
  right: -15px;
`

const StyledHandle = styled(Handle)`
  width: 10px !important;
  height: 10px !important;
  background: #555 !important;
  border: 2px solid white !important;
  border-radius: 50% !important;

  &:hover {
  width: 8px;
  height: 8px;
  background: #555;
  border-radius: 50%;

  &:hover {
    background: #888;
  }
`

interface TableNodeProps {
  data: {
    table: {
      name: string
      schema: string
      columns: Array<{
        name: string
        type: string
        isPrimaryKey: boolean
        isForeignKey: boolean
        enumValues?: string[]
        references?: {
          table: string
          column: string
        }
      }>
    }
  }
}

export function TableNode ({ data: { table }, id }: TableNodeProps & { id: string }) {
  const { getNode, setCenter, getNodes, setNodes, getEdges, fitView, setViewport } = useReactFlow()
  const [hoveredEnum, setHoveredEnum] = useState<string[] | null>(null)
  const [tooltipPosition, setTooltipPosition] = useState<Position>(Position.Right)
  const primaryKeys = table.columns.filter(col => col.isPrimaryKey)
  const foreignKeys = table.columns.filter(col => col.isForeignKey)

  const formatType = (type: string) => {
    // Remove schema prefix from enum types
    return type.replace(/^public\./, '')
  }

  const handleColumnClick = (column: typeof table.columns[0]) => {
    console.log('Column clicked:', {
      name: column.name,
      isForeignKey: column.isForeignKey,
      references: column.references,
      fullColumn: column
    })

    if (!column.isForeignKey || !column.references) {
      console.log('Foreign key check failed:', {
        isForeignKey: column.isForeignKey,
        hasReferences: !!column.references
      })
      return
    }

    const targetTableName = column.references.table.split('.').pop() || column.references.table
    console.log('Looking for table:', targetTableName)

    const targetNode = getNode(targetTableName)
    console.log('Found node:', targetNode ? 'yes' : 'no', targetNode)

    if (targetNode) {
      console.log('Centering on position:', targetNode.position)
      setCenter(targetNode.position.x, targetNode.position.y, { duration: 800 })
    }
  }

  const handleEnumHover = (event: React.MouseEvent, column: typeof table.columns[0]) => {
    if (!column.enumValues) return

    const rect = event.currentTarget.getBoundingClientRect()
    const nodeRect = (event.currentTarget.closest('[data-id]') as HTMLElement)?.getBoundingClientRect()

    if (!nodeRect) return

    // Calculate distances to each edge of the node
    const distanceToRight = Math.abs(rect.right - nodeRect.right)
    const distanceToLeft = Math.abs(rect.left - nodeRect.left)
    const distanceToTop = Math.abs(rect.top - nodeRect.top)
    const distanceToBottom = Math.abs(rect.bottom - nodeRect.bottom)

    // Find the smallest distance
    const minDistance = Math.min(distanceToRight, distanceToLeft, distanceToTop, distanceToBottom)

    let position
    if (minDistance === distanceToRight) position = Position.Right
    else if (minDistance === distanceToLeft) position = Position.Left
    else if (minDistance === distanceToTop) position = Position.Top
    else position = Position.Bottom

    setTooltipPosition(position)
    setHoveredEnum(column.enumValues)
  }

  const handleTableClick = useCallback(() => {
    const edges = getEdges()
    const nodes = getNodes()

    // Find all connected nodes and separate them into referenced and referencing
    const connectedNodeIds = new Set<string>()
    const referencedNodes: typeof nodes = []
    const referencingNodes: typeof nodes = []
    const clickedNode = nodes.find(n => n.id === id)

    if (!clickedNode) return
    connectedNodeIds.add(id)

    edges.forEach(edge => {
      if (edge.source === id) {
        connectedNodeIds.add(edge.target)
        const targetNode = nodes.find(n => n.id === edge.target)
        if (targetNode) referencedNodes.push(targetNode)
      }
      if (edge.target === id) {
        connectedNodeIds.add(edge.source)
        const sourceNode = nodes.find(n => n.id === edge.source)
        if (sourceNode) referencingNodes.push(sourceNode)
      }
    })

    // Calculate node dimensions and initial positions
    const getNodeDimensions = (node: typeof nodes[0]): NodeDimensions => ({
      width: 300, // Fixed width of our nodes
      height: 40 + (node.data.table.columns.length * 24) + 16,
      x: 0,
      y: 0
    })

    // Layout nodes in multiple columns if needed
    const layoutNodes = (columnNodes: typeof nodes, startX: number, isLeft: boolean): NodeDimensions[] => {
      if (columnNodes.length === 0) return []

      const dimensions: NodeDimensions[] = columnNodes.map(node => getNodeDimensions(node))
      const verticalSpacing = 50 // Space between nodes vertically
      const horizontalSpacing = 400 // Increased space between columns
      const maxColumnHeight = window.innerHeight * 0.7 // Reduced to 70% of viewport height for better visibility

      // First, sort nodes by height to optimize space usage
      const sortedNodeIndices = dimensions.map((_, i) => i)
        .sort((a, b) => dimensions[b].height - dimensions[a].height)

      // Initialize columns structure
      const columns: number[][] = [[]]
      let columnHeights: number[] = [0]

      // Distribute nodes across columns using a "best-fit" approach
      sortedNodeIndices.forEach(nodeIndex => {
        const nodeHeight = dimensions[nodeIndex].height + verticalSpacing

        // Find the column with the least height
        let shortestColumnIndex = 0
        let shortestColumnHeight = columnHeights[0]

        for (let i = 0; i < columnHeights.length; i++) {
          if (columnHeights[i] < shortestColumnHeight) {
            shortestColumnIndex = i
            shortestColumnHeight = columnHeights[i]
          }
        }

        // If adding to shortest column exceeds max height and we haven't maxed out columns
        if (shortestColumnHeight + nodeHeight > maxColumnHeight &&
          columnHeights.length < Math.ceil(columnNodes.length / 2)) {
          // Start a new column
          columns.push([])
          columnHeights.push(0)
          shortestColumnIndex = columns.length - 1
          shortestColumnHeight = 0
        }

        // Add node to the selected column
        columns[shortestColumnIndex].push(nodeIndex)
        columnHeights[shortestColumnIndex] += nodeHeight
      })

      // Position nodes in each column
      columns.forEach((columnNodeIndices, columnIndex) => {
        const columnHeight = columnNodeIndices.reduce(
          (sum, nodeIndex) => sum + dimensions[nodeIndex].height + verticalSpacing,
          0
        ) - verticalSpacing // Remove extra spacing after last node

        // Calculate starting Y position to center the column
        let currentY = -columnHeight / 2

        // Calculate X position based on column index
        const xOffset = columnIndex * horizontalSpacing
        const x = isLeft
          ? startX - xOffset // For left side, move left for each column
          : startX + xOffset // For right side, move right for each column

        // Position nodes in this column
        columnNodeIndices.forEach(nodeIndex => {
          const dim = dimensions[nodeIndex]
          dim.x = x
          dim.y = currentY
          currentY += dim.height + verticalSpacing
        })
      })

      return dimensions
    }

    // Calculate dimensions for all nodes
    const centerDim = getNodeDimensions(clickedNode)
    const leftDims = layoutNodes(referencedNodes, -500, true) // Increased initial offset
    const rightDims = layoutNodes(referencingNodes, 500, false) // Increased initial offset

    // Center the clicked node vertically
    centerDim.x = 0
    centerDim.y = -centerDim.height / 2

    // Create a map of node IDs to their dimensions
    const dimensionsMap = new Map<string, NodeDimensions>()
    leftDims.forEach((dim, i) => dimensionsMap.set(referencedNodes[i].id, dim))
    rightDims.forEach((dim, i) => dimensionsMap.set(referencingNodes[i].id, dim))
    dimensionsMap.set(id, centerDim)

    // Update node positions
    const updatedNodes = nodes.map(node => {
      if (!connectedNodeIds.has(node.id)) {
        return { ...node, hidden: true }
      }

      const dim = dimensionsMap.get(node.id)
      if (!dim) return { ...node, hidden: false }

      return {
        ...node,
        hidden: false,
        position: {
          x: dim.x,
          y: dim.y
        }
      }
    })

    setNodes(updatedNodes)

    // Calculate the bounds and center view
    const visibleNodes = updatedNodes.filter(node => !node.hidden)
    if (visibleNodes.length > 0) {
      // Get the bounds of all visible nodes
      const bounds = getRectOfNodes(visibleNodes)

      // Calculate the optimal viewport that maximizes zoom while keeping all nodes visible
      const viewport = getViewportForBounds(
        bounds,
        window.innerWidth,
        window.innerHeight,
        0, // No padding to maximize zoom
        Infinity // No max zoom limit
      )

      // Apply the viewport transformation
      setViewport(viewport, { duration: 800 })
    }
  }, [id, getEdges, getNodes, setNodes, fitView, setViewport])

  return (
    <>
      {hoveredEnum && (
        <NodeToolbar
          position={tooltipPosition}
          align="start"
          offset={5}
        >
          <EnumTooltip>
            <strong>Enum Values:</strong>
            <ul>
              {hoveredEnum.map((value, i) => (
                <li key={i}>{value}</li>
              ))}
            </ul>
          </EnumTooltip>
        </NodeToolbar>
      )}

      <Handle
        type="target"
        position={Position.Left}
        style={{
          opacity: 0,
          width: '8px',
          height: '8px',
          border: '2px solid white'
        }}
      />

      <NodeContainer>
        <TableHeader onClick={handleTableClick}>
          {table.name}
        </TableHeader>
        <ColumnList>
          {table.columns.map((column) => {
            const reference = column.references ? {
              ...column.references,
              table: column.references.table.split('.').pop() || column.references.table
            } : undefined

            const isEnum = column.enumValues && column.enumValues.length > 0

            return (
              <ColumnItem
                key={column.name}
                isPrimaryKey={column.isPrimaryKey}
                isForeignKey={column.isForeignKey}
                title={column.isForeignKey && reference ? `References ${reference.table}(${reference.column})` : undefined}
                onClick={() => handleColumnClick(column)}
              >
                <ColumnName
                  isPrimaryKey={column.isPrimaryKey}
                  isForeignKey={column.isForeignKey}
                  title={column.name}
                >
                  {column.name}
                </ColumnName>
                <TypeLabel
                  isForeignKey={column.isForeignKey}
                  isEnum={Boolean(isEnum)}
                  onMouseEnter={(e) => isEnum && handleEnumHover(e, column)}
                  onMouseLeave={() => setHoveredEnum(null)}
                >
                  {formatType(column.type)}
                </TypeLabel>
              </ColumnItem>
            )
          })}
        </ColumnList>
      </NodeContainer>

      <Handle
        type="source"
        position={Position.Right}
        style={{
          opacity: 0,
          width: '8px',
          height: '8px',
          border: '2px solid white'
        }}
      />
    </>
  )
}
