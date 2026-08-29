import { useCallback, useEffect, useState } from 'react'
import { getOrder, getOrders, getSummary } from './api'
import SimulationBuilder from './SimulationBuilder'
import WorkflowGraph from './WorkflowGraph'
import type { DashboardOrder, OrderDetail, SimulationResult, Summary } from './types'

const emptySummary: Summary = {
  total_orders: 0, completed_orders: 0, pending_orders: 0, hardware_delay_seconds: 30, processing_jobs: 0,
  retrying_jobs: 0, waiting_jobs: 0, failed_jobs: 0, unscheduled_orders: 0, webhook_events: 0, updated_at: '',
}

const POLL_INTERVAL_MS = 5000

function formatDate(value?: string) {
  if (!value) return '-'
  return new Date(value).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
}

function formatMoney(value: number) {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency: 'EUR' }).format(value)
}

function StatusPill({ status, error }: { status: string; error?: boolean }) {
  const normalized = error && status !== 'WAITING' ? 'error' : status.toLowerCase().replace('_', '-')
  return <span className={`status-pill pill-${normalized}`}>{status.replace('_', ' ')}</span>
}

function getSapBehavior(order: DashboardOrder) {
  if (order.status === 'CANCELLED' || order.sync_status === 'CANCELLED') {
    return { label: 'Cancelled', summary: 'The order was cancelled and will not be dispatched to SAP.', dispatch: 'No SAP request', retry: 'Not applicable' }
  }
  switch (order.sync_status) {
    case 'SYNCED':
      return { label: 'Acknowledged', summary: 'SAP accepted the order and returned an internal ID.', dispatch: 'Complete', retry: 'No retry required' }
    case 'PROCESSING':
      return { label: 'In flight', summary: 'The worker is dispatching the order and waiting for SAP to respond.', dispatch: 'Request in flight', retry: 'Retry only after failure' }
    case 'WAITING':
      return { label: 'Automatic recovery', summary: 'SAP is unavailable; the order remains queued for another attempt.', dispatch: 'Paused until retry window', retry: 'Automatic retry scheduled' }
    case 'DEAD':
      return { label: 'Stopped', summary: 'SAP synchronization stopped after the retry budget was exhausted.', dispatch: 'No further dispatch', retry: 'Manual intervention required' }
    case 'NOT_SCHEDULED':
      return { label: 'Not scheduled', summary: 'The payment gate or order workflow has not created an SAP job yet.', dispatch: 'No SAP request', retry: 'Not applicable' }
    default:
      return { label: 'Queued', summary: 'The order is waiting for its SAP sync job to become due.', dispatch: 'Queued', retry: 'Backoff managed by worker' }
  }
}

function displaySyncStatus(order: DashboardOrder) {
  return order.status === 'CANCELLED' ? 'CANCELLED' : order.sync_status
}

function Metric({ label, value, tone }: { label: string; value: number; tone?: string }) {
  return <div className={`metric ${tone ?? ''}`}><span>{label}</span><strong>{value}</strong></div>
}

export default function App() {
  const [summary, setSummary] = useState(emptySummary)
  const [orders, setOrders] = useState<DashboardOrder[]>([])
  const [selectedId, setSelectedId] = useState('')
  const [detail, setDetail] = useState<OrderDetail | null>(null)
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('')
  const [error, setError] = useState('')
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null)

  const refreshList = useCallback(async () => {
    try {
      const [nextSummary, nextOrders] = await Promise.all([getSummary(), getOrders(query, status)])
      setSummary(nextSummary)
      setOrders(nextOrders)
      setLastRefresh(new Date())
      setError('')
      setSelectedId((current) => nextOrders.some((order) => order.order_id === current) ? current : nextOrders[0]?.order_id || '')
    } catch (refreshError) {
      setError(refreshError instanceof Error ? refreshError.message : 'Unable to load dashboard data')
    }
  }, [query, status])

  const refreshDetail = useCallback(async () => {
    if (!selectedId) {
      setDetail(null)
      return
    }
    try {
      setDetail(await getOrder(selectedId))
      setError('')
    } catch (detailError) {
      setError(detailError instanceof Error ? detailError.message : 'Unable to load order details')
    }
  }, [selectedId])

  useEffect(() => { void refreshList(); const timer = window.setInterval(() => void refreshList(), POLL_INTERVAL_MS); return () => window.clearInterval(timer) }, [refreshList])
  useEffect(() => { void refreshDetail(); const timer = window.setInterval(() => void refreshDetail(), POLL_INTERVAL_MS); return () => window.clearInterval(timer) }, [refreshDetail])

  function handleSimulationStarted(result: SimulationResult) {
    setSelectedId(result.order_id)
    void refreshList()
  }

  const sapBehavior = detail ? getSapBehavior(detail.order) : null

  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href="#overview" aria-label="Sync console overview">
          <span className="brand-product">sync console</span>
        </a>
        <nav className="topnav" aria-label="Primary navigation">
          <a className="active" href="#overview">Overview</a>
          <a href="#orders">Orders</a>
          <a href="#simulator">Simulator</a>
        </nav>
        <div className="topbar-actions">
          <span className="status-chip"><span className="connection-dot" /> Live</span>
          <span className="topbar-updated">{lastRefresh ? `Updated ${lastRefresh.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}` : 'Connecting'}</span>
          <span className="avatar" aria-label="Operations user">OP</span>
        </div>
      </header>
      <main className="content" id="overview">
        {error && <div className="error-banner"><strong>Dashboard issue</strong><span>{error}</span><button onClick={() => { void refreshList(); void refreshDetail() }}>Retry</button></div>}

        <section className="metrics-section" aria-labelledby="metrics-title">
          <div className="section-header">
            <div><p className="eyebrow">01 / At a glance</p><h2 id="metrics-title">Everything in sync</h2></div>
            <span className="section-note">Live telemetry <span>-</span> polling every 5s</span>
          </div>
          <div className="metrics-grid">
            <Metric label="Total orders" value={summary.total_orders} />
            <Metric label="Completed + SAP" value={summary.completed_orders} tone="metric-success" />
            <Metric label="Awaiting payment" value={summary.pending_orders} tone="metric-warning" />
            <Metric label="Waiting for SAP" value={summary.waiting_jobs} tone="metric-info" />
            <Metric label="Dead / terminal" value={summary.failed_jobs} tone="metric-error" />
          </div>
        </section>
        <div className="operational-context" aria-label="Operational context">
          <span className="operational-context-label"><span className="mini-dot" /> Operational context</span>
          <span><strong>Hardware hold:</strong> {summary.hardware_delay_seconds}s</span>
          <span><strong>Processing:</strong> {summary.processing_jobs}</span>
          <span><strong>Retrying:</strong> {summary.retrying_jobs}</span>
          <span><strong>Waiting for SAP:</strong> {summary.waiting_jobs}</span>
        </div>

        <div id="simulator"><SimulationBuilder onStarted={handleSimulationStarted} /></div>

        <section className="workspace" id="orders">
          <aside className="order-panel panel">
            <div className="panel-heading"><div><p className="eyebrow">02 / Database stream</p><h2>Orders</h2></div><span className="count-badge">{orders.length}</span></div>
            <div className="filters"><input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search order or email..." /><select value={status} onChange={(e) => setStatus(e.target.value)}><option value="">All sync states</option><option value="SYNCED">SYNCED</option><option value="PENDING">PENDING</option><option value="PROCESSING">PROCESSING</option><option value="WAITING">WAITING</option><option value="DEAD">DEAD</option><option value="CANCELLED">CANCELLED</option><option value="NOT_SCHEDULED">NOT SCHEDULED</option></select></div>
            <div className="order-table" role="table" aria-label="Orders">
              <div className="order-table-head" role="row"><span role="columnheader">Order</span><span role="columnheader">SAP ID</span><span role="columnheader">Status</span></div>
              <div className="order-list">{orders.length === 0 ? <div className="empty-state">No orders match this filter.</div> : orders.map((order) => { const displayStatus = displaySyncStatus(order); return <button className={`order-row ${selectedId === order.order_id ? 'selected' : ''} ${displayStatus === 'DEAD' ? 'order-row-dead' : ''} ${displayStatus === 'PENDING' ? 'order-row-pending' : ''}`} key={order.order_id} onClick={() => setSelectedId(order.order_id)} role="row"><div className="order-identity" role="cell"><strong>{order.order_id}</strong><span>{order.customer_email}</span></div><div className="order-sap-id" role="cell">{order.sap_internal_id ?? '-'}</div><div className="row-meta" role="cell"><StatusPill status={displayStatus} error={Boolean(order.last_error)} /><small>{formatDate(order.created_at)}</small></div></button> })}</div>
            </div>
          </aside>

          <section className="detail-panel panel">
            {!detail ? <div className="empty-detail"><div className="empty-icon">+</div><h2>Select an order</h2><p>Choose an order from the database stream to see its workflow.</p></div> : <>
              <div className="detail-heading"><div><p className="eyebrow">03 / Order drill-down</p><h2>{detail.order.order_id}</h2><p>{detail.order.customer_email}</p></div></div>
              <div className="detail-facts"><div><span>Created</span><strong>{formatDate(detail.order.created_at)}</strong></div><div><span>Payment</span><strong>{detail.order.payment_status.replace('_', ' ')}</strong></div><div><span>Paid</span><strong>{formatDate(detail.order.paid_at)}</strong></div><div><span>Due at</span><strong>{formatDate(detail.order.due_at)}</strong></div><div><span>Attempts</span><strong>{detail.order.attempts ?? 0}</strong></div><div><span>SAP internal ID</span><strong>{detail.order.sap_internal_id ?? '-'}</strong></div><div><span>Value</span><strong>{formatMoney(detail.order.total_value)}</strong></div></div>
              {sapBehavior && <section className="sap-behavior" aria-labelledby="sap-behavior-title"><div className="subheading"><div><p className="eyebrow">SAP integration</p><h3 id="sap-behavior-title">SAP behavior</h3></div><span>{sapBehavior.label}</span></div><p className="sap-behavior-summary">{sapBehavior.summary}</p><div className="sap-behavior-grid"><div><span>Dispatch</span><strong>{sapBehavior.dispatch}</strong></div><div><span>Retry policy</span><strong>{sapBehavior.retry}</strong></div><div><span>Attempts</span><strong>{detail.order.attempts ?? 0}</strong></div><div><span>Last response</span><strong>{detail.order.last_error ?? 'No error recorded'}</strong></div><div><span>SAP internal ID</span><strong>{detail.order.sap_internal_id ?? 'Not assigned'}</strong></div><div><span>Next due</span><strong>{formatDate(detail.order.due_at)}</strong></div></div></section>}
              {detail.order.last_error && <div className={`detail-error ${detail.order.sync_status === 'WAITING' ? 'detail-recovery' : ''}`}><strong>SAP sync note</strong><span>{detail.order.last_error}</span></div>}
              <div className="graph-heading"><div><p className="eyebrow">Recorded workflow</p><h3>Execution graph</h3></div><span>Polling every 5s</span></div>
              <WorkflowGraph workflow={detail.workflow} />
              <div className="lower-detail"><div><div className="subheading"><h3>Order items</h3><span>{detail.items.length} line(s)</span></div><div className="items-table">{detail.items.map((item, index) => <div className="item-row" key={`${item.sku}-${index}`}><span>{item.sku}</span><span>x {item.quantity}</span><strong>{formatMoney(item.price * item.quantity)}</strong></div>)}</div></div><div><div className="subheading"><h3>Webhook events</h3><span>{detail.events.length}</span></div><div className="events-list">{detail.events.map((event) => <details key={event.event_id}><summary><span className={`event-dot event-${event.event_type.toLowerCase()}`} />{event.event_type}<strong>{event.event_id}</strong><small>{event.processed_at ? 'processed' : 'pending'}</small></summary><pre>{JSON.stringify(event.payload, null, 2)}</pre></details>)}</div></div></div>
            </>}
          </section>
        </section>
      </main>
    </div>
  )
}
