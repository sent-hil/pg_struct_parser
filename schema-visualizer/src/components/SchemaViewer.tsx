import { useCallback, useState, useEffect, useMemo } from 'react'
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useEdgesState,
  ReactFlowProvider,
  Panel,
  ConnectionMode,
  MarkerType,
  BackgroundVariant,
  Edge
} from 'reactflow'
import 'reactflow/dist/style.css'
import styled from '@emotion/styled'
import { TableNode } from './TableNode'
import { parseSQLSchema, createReactFlowElements } from '../utils/sqlParser'

const nodeTypes = {
  tableNode: TableNode,
}

const LayoutContainer = styled.div`
  width: 100vw;
  height: 100vh;
  position: relative;
`

const StatsPanel = styled.div`
  background: white;
  padding: 10px;
  border-radius: 5px;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
  display: flex;
  flex-direction: column;
  gap: 8px;
`

const SearchInput = styled.input`
  padding: 6px 12px;
  border: 1px solid #ccc;
  border-radius: 4px;
  font-size: 14px;
  width: 200px;
  &:focus {
    outline: none;
    border-color: #666;
  }
  &::placeholder {
    color: #999;
  }
`

const FloatingLabel = styled.div<{ x: number; y: number }>`
  position: fixed;
  left: ${props => props.x}px;
  top: ${props => props.y + 20}px;
  transform: translate(-50%, 0);
  background: white;
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 12px;
  font-family: system-ui, -apple-system, sans-serif;
  font-weight: 500;
  color: #333;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
  pointer-events: none;
  z-index: 1000;
`

interface SchemaViewerProps {
  sqlContent: string
}

export function SchemaViewer ({ sqlContent }: SchemaViewerProps) {
  // Memoize the initial schema parsing and element creation
  const { nodes: initialNodes, edges: initialEdges } = useMemo(() => {
    const schemaData = parseSQLSchema(sqlContent)
    return createReactFlowElements(schemaData)
  }, [sqlContent])

  const [hoveredEdge, setHoveredEdge] = useState<Edge | null>(null)
  const [mousePosition, setMousePosition] = useState({ x: 0, y: 0 })
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes)
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges)
  const [searchQuery, setSearchQuery] = useState('')

  // Calculate total foreign keys by counting foreign key columns across all tables
  const foreignKeyCount = useMemo(() => {
    return nodes.reduce((count, node) => {
      return count + (node.data.table.columns?.filter((col: { isForeignKey: boolean }) => col.isForeignKey)?.length || 0)
    }, 0)
  }, [nodes])

  // Update styles based on search and hover
  useEffect(() => {
    if (!searchQuery) {
      setNodes(nodes => nodes.map(node => ({
        ...node,
        style: { ...node.style, backgroundColor: undefined, boxShadow: undefined, opacity: 1 }
      })))
      setEdges(edges => edges.map(edge => ({
        ...edge,
        style: {
          stroke: hoveredEdge?.id === edge.id ? '#333' : '#666',
          strokeWidth: hoveredEdge?.id === edge.id ? 2 : 1.5,
          opacity: hoveredEdge?.id === edge.id ? 1 : 0.6
        }
      })))
      return
    }

    try {
      const regex = new RegExp(searchQuery, 'i')
      const matchingNodeIds = new Set(
        nodes
          .filter(node => regex.test((node.data as any).table.name))
          .map(node => node.id)
      )

      setNodes(nodes => nodes.map(node => ({
        ...node,
        style: {
          ...node.style,
          backgroundColor: matchingNodeIds.has(node.id) ? '#f0f9ff' : undefined,
          boxShadow: matchingNodeIds.has(node.id) ? '0 0 0 2px #3b82f6' : undefined,
          opacity: matchingNodeIds.has(node.id) ? 1 : 0.2
        }
      })))

      setEdges(edges => edges.map(edge => {
        const isHovered = hoveredEdge?.id === edge.id
        const sourceMatches = matchingNodeIds.has(edge.source)
        return {
          ...edge,
          style: {
            stroke: isHovered ? '#333' : '#666',
            strokeWidth: isHovered ? 2 : 1.5,
            opacity: isHovered ? 1 : (sourceMatches ? 0.6 : 0)
          }
        }
      }))
    } catch (e) {
      // Invalid regex, silently ignore
    }
  }, [searchQuery, hoveredEdge])

  // Handle edge hover events
  const onEdgeMouseEnter = useCallback((event: React.MouseEvent, edge: Edge) => {
    setHoveredEdge(edge)
    setMousePosition({ x: event.clientX, y: event.clientY })
  }, [])

  const onEdgeMouseMove = useCallback((event: React.MouseEvent, edge: Edge) => {
    if (hoveredEdge?.id === edge.id) {
      setMousePosition({ x: event.clientX, y: event.clientY })
    }
  }, [hoveredEdge])

  const onEdgeMouseLeave = useCallback((event: React.MouseEvent, edge: Edge) => {
    setHoveredEdge(null)
  }, [])

  return (
    <LayoutContainer>
      <ReactFlowProvider>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onEdgeMouseEnter={onEdgeMouseEnter}
          onEdgeMouseMove={onEdgeMouseMove}
          onEdgeMouseLeave={onEdgeMouseLeave}
          nodeTypes={nodeTypes}
          fitView
          attributionPosition="bottom-right"
          style={{ background: '#f8f8f8' }}
          defaultEdgeOptions={{
            type: 'smoothstep',
            markerEnd: {
              type: MarkerType.ArrowClosed,
              width: 15,
              height: 15,
              color: '#666'
            }
          }}
          connectionMode={ConnectionMode.Loose}
          minZoom={0.1}
          maxZoom={1.5}
          zoomOnScroll={true}
          panOnScroll={true}
          panOnDrag={true}
          selectionOnDrag={true}
          selectNodesOnDrag={false}
        >
          <Background variant={BackgroundVariant.Dots} gap={12} size={1} />
          <Controls />
          <MiniMap
            nodeStrokeColor="#666"
            nodeColor="#fff"
            nodeBorderRadius={2}
          />
          <Panel position="top-left">
            <StatsPanel>
              <SearchInput
                placeholder="Search tables (regex)"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
              />
              <div>Tables: {nodes.length}</div>
              <div>Foreign Keys: {foreignKeyCount}</div>
            </StatsPanel>
          </Panel>
        </ReactFlow>
        {hoveredEdge && (
          <FloatingLabel x={mousePosition.x} y={mousePosition.y}>
            {hoveredEdge.source}.{hoveredEdge.data?.fromColumn} → {hoveredEdge.target}.{hoveredEdge.data?.toColumn}
          </FloatingLabel>
        )}
      </ReactFlowProvider>
    </LayoutContainer>
  )
}
