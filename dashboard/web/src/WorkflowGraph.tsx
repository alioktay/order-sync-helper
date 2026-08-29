import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import type { Workflow, WorkflowNode, WorkflowStatus } from './types'

type FlowNodeData = { node: WorkflowNode; expanded: boolean; selected: boolean; onSelect?: () => void }
type FlowNode = Node<FlowNodeData, 'workflow'>
type LayoutPositions = Record<string, { x: number; y: number }>
type Selection = { kind: 'node'; id: string } | { kind: 'edge'; id: string } | null
type HoverPayload = { kind: 'node' | 'edge'; label: string; status: WorkflowStatus; detail?: string }
type HoveredElement = { kind: 'node' | 'edge'; label: string; status: WorkflowStatus; detail?: string; x: number; y: number } | null

const compactFourPositions: LayoutPositions = {
  shop: { x: 0, y: 0 }, order: { x: 176, y: 0 },
  payment: { x: 0, y: 108 }, gate: { x: 176, y: 108 }, schedule: { x: 352, y: 108 },
  delay: { x: 0, y: 216 }, worker: { x: 176, y: 216 }, sap: { x: 352, y: 216 },
  synced: { x: 352, y: 324 }, retry: { x: 528, y: 324 }, dead: { x: 704, y: 324 },
}

const compactThreePositions: LayoutPositions = {
  shop: { x: 0, y: 0 }, order: { x: 176, y: 0 }, gate: { x: 352, y: 0 },
  payment: { x: 0, y: 108 }, schedule: { x: 352, y: 108 }, delay: { x: 528, y: 108 }, worker: { x: 704, y: 108 }, sap: { x: 880, y: 108 },
  synced: { x: 880, y: 216 }, retry: { x: 1056, y: 216 }, dead: { x: 1232, y: 216 },
}

const expandedPositions: LayoutPositions = {
  shop: { x: 0, y: 0 }, order: { x: 260, y: 0 }, gate: { x: 520, y: 0 },
  payment: { x: 0, y: 210 }, schedule: { x: 520, y: 210 }, delay: { x: 780, y: 210 }, worker: { x: 1040, y: 210 }, sap: { x: 1300, y: 210 },
  synced: { x: 1300, y: 420 }, retry: { x: 1560, y: 420 }, dead: { x: 1820, y: 420 },
}

const statusLabel: Record<WorkflowStatus, string> = {
  success: 'complete', active: 'running', pending: 'waiting', error: 'issue', skipped: 'skipped', cancelled: 'cancelled',
}

function statusColor(status: WorkflowStatus): string {
  return { success: '#5c9b6b', active: '#6f9eb4', pending: '#caa84f', error: '#c9796b', skipped: '#9aa49a', cancelled: '#8a7a62' }[status]
}

function WorkflowNodeView({ data }: NodeProps<FlowNode>) {
  const { node, expanded, selected } = data
  const color = statusColor(node.status)
  return (
    <div className={`workflow-node status-${node.status} ${expanded ? 'workflow-node-expanded' : ''} ${selected ? 'workflow-node-selected' : ''}`} title={`${node.label}: ${node.detail ?? statusLabel[node.status]}`} role="button" tabIndex={0} aria-label={`${node.label}, ${statusLabel[node.status]}`} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); data.onSelect?.() } }}>
      <Handle id="target-left" type="target" position={Position.Left} />
      <Handle id="target-top" type="target" position={Position.Top} />
      <Handle id="target-bottom" type="target" position={Position.Bottom} />
      <Handle id="target-right" type="target" position={Position.Right} />
      <div className="workflow-node-heading"><span className="node-dot" style={{ backgroundColor: color }} /><span>{node.label}</span></div>
      <span className="node-status">{statusLabel[node.status]}</span>
      {expanded && node.detail && <p>{node.detail}</p>}
      {!expanded && node.detail && <span className="workflow-node-focus-detail">{node.detail}</span>}
      {node.timestamp && <time>{new Date(node.timestamp).toLocaleTimeString()}</time>}
      <Handle id="source-left" type="source" position={Position.Left} />
      <Handle id="source-top" type="source" position={Position.Top} />
      <Handle id="source-bottom" type="source" position={Position.Bottom} />
      <Handle id="source-right" type="source" position={Position.Right} />
    </div>
  )
}

const nodeTypes = { workflow: WorkflowNodeView }

function handleDirection(source: { x: number; y: number }, target: { x: number; y: number }) {
  if (target.y > source.y + 20) return { sourceHandle: 'source-bottom', targetHandle: 'target-top' }
  if (target.y < source.y - 20) return { sourceHandle: 'source-top', targetHandle: 'target-bottom' }
  if (target.x >= source.x) return { sourceHandle: 'source-right', targetHandle: 'target-left' }
  return { sourceHandle: 'source-left', targetHandle: 'target-right' }
}

function GraphCanvas({ nodes, edges, expanded, onNodeSelect, onEdgeSelect, onPaneSelect }: {
  nodes: FlowNode[]; edges: Edge[]; expanded: boolean
  onNodeSelect: (node: FlowNode) => void; onEdgeSelect: (edge: Edge) => void; onPaneSelect: () => void
}) {
  const canvasRef = useRef<HTMLDivElement>(null)
  const [hovered, setHovered] = useState<HoveredElement>(null)
  const showHover = useCallback((event: ReactMouseEvent, element: HoverPayload) => {
    const bounds = canvasRef.current?.getBoundingClientRect()
    if (!bounds) return
    setHovered({ ...element, x: event.clientX - bounds.left + 14, y: event.clientY - bounds.top + 14 })
  }, [])

  return (
    <div className="graph-canvas" ref={canvasRef}>
      <ReactFlow
        key={expanded ? 'expanded' : 'compact'} nodes={nodes} edges={edges} nodeTypes={nodeTypes}
        fitView fitViewOptions={{ padding: expanded ? 0.14 : 0.06 }} nodesDraggable={false} nodesConnectable={false}
        onNodeClick={(_, node) => onNodeSelect(node)} onEdgeClick={(_, edge) => onEdgeSelect(edge)} onPaneClick={onPaneSelect}
        onNodeMouseEnter={(event, node) => showHover(event, { kind: 'node', label: node.data.node.label, status: node.data.node.status, detail: node.data.node.detail })}
        onEdgeMouseEnter={(event, edge) => showHover(event, { kind: 'edge', label: edge.label?.toString() || 'Workflow relation', status: edge.data?.status as WorkflowStatus, detail: edge.data?.detail as string | undefined })}
        onNodeMouseLeave={() => setHovered(null)} onEdgeMouseLeave={() => setHovered(null)}
        proOptions={{ hideAttribution: true }}
      >
        <Background color="#dbe3d8" gap={expanded ? 24 : 18} size={1} /><Controls showInteractive={false} />
      </ReactFlow>
      {hovered && <div className="graph-hover-card" style={{ left: hovered.x, top: hovered.y }} role="status"><strong>{hovered.label}</strong><span className={`graph-hover-status status-${hovered.status}`}>{statusLabel[hovered.status]}</span>{hovered.detail && <p>{hovered.detail}</p>}<small>{hovered.kind === 'node' ? 'Click for details' : 'Click to inspect relation'}</small></div>}
    </div>
  )
}

function WorkflowInspector({ workflow, selection, onSelect }: { workflow: Workflow; selection: Selection; onSelect: (selection: Selection) => void }) {
  if (!selection) return null
  if (selection.kind === 'node') {
    const node = workflow.nodes.find((candidate) => candidate.id === selection.id)
    if (!node) return null
    const incoming = workflow.edges.filter((edge) => edge.target === node.id)
    const outgoing = workflow.edges.filter((edge) => edge.source === node.id)
    return <section className="workflow-inspector" aria-label="Selected workflow node"><div className="workflow-inspector-heading"><div><span className="eyebrow">Selected step</span><h4>{node.label}</h4></div><span className={`inspector-status status-${node.status}`}>{statusLabel[node.status]}</span></div>{node.detail && <p className="workflow-inspector-detail">{node.detail}</p>}{node.timestamp && <time>Recorded {new Date(node.timestamp).toLocaleString()}</time>}{(incoming.length > 0 || outgoing.length > 0) && <div className="workflow-relations">{incoming.map((edge) => <button key={edge.id} type="button" onClick={() => onSelect({ kind: 'edge', id: edge.id })}><span>From {workflow.nodes.find((candidate) => candidate.id === edge.source)?.label}</span><strong>{edge.label}</strong></button>)}{outgoing.map((edge) => <button key={edge.id} type="button" onClick={() => onSelect({ kind: 'edge', id: edge.id })}><span>To {workflow.nodes.find((candidate) => candidate.id === edge.target)?.label}</span><strong>{edge.label}</strong></button>)}</div>}</section>
  }

  const edge = workflow.edges.find((candidate) => candidate.id === selection.id)
  if (!edge) return null
  const source = workflow.nodes.find((node) => node.id === edge.source)?.label ?? edge.source
  const target = workflow.nodes.find((node) => node.id === edge.target)?.label ?? edge.target
  return <section className="workflow-inspector" aria-label="Selected workflow relation"><div className="workflow-inspector-heading"><div><span className="eyebrow">Selected relation</span><h4>{source} <span aria-hidden="true">→</span> {target}</h4></div><span className={`inspector-status status-${edge.status}`}>{statusLabel[edge.status]}</span></div><p className="workflow-inspector-detail"><strong>{edge.label}</strong>{edge.detail ? ` — ${edge.detail}` : ''}</p></section>
}

export default function WorkflowGraph({ workflow }: { workflow: Workflow }) {
  const [expanded, setExpanded] = useState(false)
  const [graphWidth, setGraphWidth] = useState(0)
  const [selection, setSelection] = useState<Selection>(null)
  const compactShellRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const element = compactShellRef.current
    if (!element) return
    const updateWidth = () => setGraphWidth(element.clientWidth)
    updateWidth()
    const observer = new ResizeObserver(updateWidth)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    if (!selection) return
    const exists = selection.kind === 'node' ? workflow.nodes.some((node) => node.id === selection.id) : workflow.edges.some((edge) => edge.id === selection.id)
    if (!exists) setSelection(null)
  }, [selection, workflow])

  const compactPositions = graphWidth >= 980 ? compactThreePositions : compactFourPositions
  const positions = expanded ? expandedPositions : compactPositions
  const nodes = useMemo<FlowNode[]>(() => workflow.nodes.map((node) => ({ id: node.id, type: 'workflow', position: positions[node.id] ?? { x: 0, y: 0 }, selected: selection?.kind === 'node' && selection.id === node.id, data: { node, expanded, selected: selection?.kind === 'node' && selection.id === node.id, onSelect: () => setSelection({ kind: 'node', id: node.id }) } })), [expanded, positions, selection, workflow.nodes])
  const edges = useMemo<Edge[]>(() => workflow.edges.map((edge) => ({ id: edge.id, source: edge.source, target: edge.target, ...handleDirection(positions[edge.source] ?? { x: 0, y: 0 }, positions[edge.target] ?? { x: 0, y: 0 }), label: edge.label, data: { status: edge.status, detail: edge.detail }, selected: selection?.kind === 'edge' && selection.id === edge.id, animated: edge.status === 'active', style: { stroke: statusColor(edge.status), strokeWidth: selection?.kind === 'edge' && selection.id === edge.id ? 4 : edge.status === 'error' ? 3 : 2 }, labelStyle: { fill: '#68766a', fontSize: expanded ? 11 : 10, fontWeight: 700 }, labelBgStyle: { fill: '#f4f6f1', fillOpacity: 0.96 }, markerEnd: { type: MarkerType.ArrowClosed, color: statusColor(edge.status) } })), [expanded, positions, selection, workflow.edges])

  const selectNode = (node: FlowNode) => setSelection({ kind: 'node', id: node.id })
  const selectEdge = (edge: Edge) => setSelection({ kind: 'edge', id: edge.id })
  return <>
    <div className="graph-shell graph-shell-compact" ref={compactShellRef}><button className="graph-expand-button" type="button" onClick={() => setExpanded(true)}>Expand graph</button><GraphCanvas nodes={nodes} edges={edges} expanded={false} onNodeSelect={selectNode} onEdgeSelect={selectEdge} onPaneSelect={() => setSelection(null)} /></div>
    <WorkflowInspector workflow={workflow} selection={selection} onSelect={setSelection} />
    {expanded && <div className="graph-overlay" role="dialog" aria-modal="true" aria-labelledby="expanded-workflow-title"><div className="graph-overlay-panel"><div className="graph-overlay-heading"><div><p className="eyebrow">Recorded workflow</p><h3 id="expanded-workflow-title">Execution graph</h3></div><button className="graph-close-button" type="button" onClick={() => setExpanded(false)}>Close</button></div><div className="graph-shell graph-shell-expanded"><GraphCanvas nodes={nodes} edges={edges} expanded onNodeSelect={selectNode} onEdgeSelect={selectEdge} onPaneSelect={() => setSelection(null)} /></div><WorkflowInspector workflow={workflow} selection={selection} onSelect={setSelection} /></div></div>}
  </>
}
