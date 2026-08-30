import { useEffect, useState, type FormEvent } from 'react'
import { sendOrderSimulation, sendPaymentSimulation } from './api'
import { clearMockSapResponse, getMockSapResponse, setMockSapResponse } from './api'
import type { MockSapResponseOverride, OrderSimulationRequest, PaymentSimulationRequest, SimulationItem, SimulationResult, WebhookSimulationResponse } from './types'

interface BuilderProps { onStarted: (result: SimulationResult) => void }

const HISTORY_PAGE_SIZE = 5

function presetSapID() {
  return `SAP-${Math.floor(100000 + Math.random() * 90000000)}`
}

const SAP_PRESETS: { label: string; value: MockSapResponseOverride | (() => MockSapResponseOverride) }[] = [
  { label: 'SAP success', value: () => ({ status_code: 201, body: { status: 'success', sap_internal_id: presetSapID() } }) },
  { label: 'Business failure', value: () => ({ status_code: 200, body: { status: 'failed', sap_internal_id: presetSapID() } }) },
  { label: 'Missing SAP ID', value: { status_code: 201, body: { status: 'success' } } },
  { label: 'Bad request', value: { status_code: 400, body: { status: 'error', message: 'bad request' } } },
  { label: 'Unauthorized', value: { status_code: 401, body: { status: 'error', message: 'unauthorized' } } },
  { label: 'Not found', value: { status_code: 404, body: { status: 'error', message: 'not found' } } },
  { label: 'Rate limited — seconds', value: { status_code: 429, body: { status: 'error', message: 'rate limited' }, retry_after: '7' } },
  { label: 'Rate limited — HTTP date', value: () => ({ status_code: 429, body: { status: 'error', message: 'rate limited' }, retry_after: new Date(Date.now() + 7000).toUTCString() }) },
  { label: 'Rate limited — invalid header', value: { status_code: 429, body: { status: 'error', message: 'rate limited' }, retry_after: 'later' } },
  { label: 'Rate limited — no header', value: { status_code: 429, body: { status: 'error', message: 'rate limited' } } },
  { label: 'Internal server error', value: { status_code: 500, body: { status: 'error', message: 'internal server error' } } },
  { label: 'Bad gateway', value: { status_code: 502, body: { status: 'error', message: 'bad gateway' } } },
  { label: 'Service unavailable', value: { status_code: 503, body: { status: 'error', message: 'service unavailable' } } },
  { label: 'Gateway timeout', value: { status_code: 504, body: { status: 'error', message: 'gateway timeout' } } },
  { label: 'Delayed SAP response', value: () => ({ status_code: 201, body: { status: 'success', sap_internal_id: presetSapID() }, delay_ms: 5000 }) },
]

function eventId(prefix: string) { return `evt-dashboard-${prefix}-${Date.now()}-${crypto.randomUUID()}` }

function responseText(response: WebhookSimulationResponse) {
  return JSON.stringify(response.response_body ?? null, null, 2)
}

export default function SimulationBuilder({ onStarted }: BuilderProps) {
  const [orderId, setOrderId] = useState(`ORD-DASH-${Date.now()}`)
  const [orderEventId, setOrderEventId] = useState(eventId('shop'))
  const [customerEmail, setCustomerEmail] = useState('dashboard@example.com')
  const [items, setItems] = useState<SimulationItem[]>([{ sku: 'NUKI-SL3', quantity: 1, price: 169 }])
  const [paymentEventId, setPaymentEventId] = useState(eventId('payment'))
  const [referenceOrderId, setReferenceOrderId] = useState(orderId)
  const [paymentStatus, setPaymentStatus] = useState('PENDING')
  const [timestamp, setTimestamp] = useState(new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'))
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [history, setHistory] = useState<WebhookSimulationResponse[]>([])
  const [historyPage, setHistoryPage] = useState(1)
  const [sapStatus, setSapStatus] = useState(201)
  const [sapBody, setSapBody] = useState(() => JSON.stringify({ status: 'success', sap_internal_id: presetSapID() }))
  const [sapRetryAfter, setSapRetryAfter] = useState('')
  const [sapDelay, setSapDelay] = useState('')
  const [sapPreset, setSapPreset] = useState('')

  function applyPreset(index: string) {
    setSapPreset(index)
    if (index === '') return
    const preset = SAP_PRESETS[Number(index)].value
    const value = typeof preset === 'function' ? preset() : preset
    setSapStatus(value.status_code); setSapBody(JSON.stringify(value.body, null, 2)); setSapRetryAfter(value.retry_after ?? ''); setSapDelay(value.delay_ms === undefined ? '' : String(value.delay_ms))
  }

  async function applySapOverride() {
    try {
      const body = JSON.parse(sapBody) as Record<string, unknown>
      await setMockSapResponse({ status_code: sapStatus, body, ...(sapRetryAfter ? { retry_after: sapRetryAfter } : {}), ...(sapDelay === '' ? {} : { delay_ms: Number(sapDelay) }) })
      setMessage('Mock SAP response override active for all orders')
    } catch (error) { setMessage(error instanceof Error ? error.message : 'Unable to arm Mock SAP override') }
  }

  async function resetSapOverride() {
    try { await clearMockSapResponse(); setMessage('Mock SAP response override cleared') } catch (error) { setMessage(error instanceof Error ? error.message : 'Unable to clear Mock SAP override') }
  }

  function updateItem(index: number, key: keyof SimulationItem, value: string | number | boolean | undefined) {
    setItems(current => current.map((item, itemIndex) => itemIndex === index ? { ...item, [key]: value } : item))
  }

  async function submitOrder(event: FormEvent) {
    event.preventDefault(); setBusy(true); setMessage('Sending shop webhook…')
    const request: OrderSimulationRequest = { event_id: orderEventId, order_id: orderId, customer_email: customerEmail, items }
    try {
      const result = await sendOrderSimulation(request)
      setHistory(current => [result, ...current]); setHistoryPage(1); setMessage(`Shop response: HTTP ${result.upstream_status}`); onStarted({ order_id: orderId } as SimulationResult)
      setPaymentEventId(eventId('payment')); setReferenceOrderId(orderId)
    } catch (error) { setMessage(error instanceof Error ? error.message : 'Order simulation failed') } finally { setBusy(false) }
  }

  async function submitPayment(event: FormEvent) {
    event.preventDefault(); setBusy(true); setMessage('Sending payment webhook…')
    const request: PaymentSimulationRequest = { event_id: paymentEventId, reference_order_id: referenceOrderId, payment_status: paymentStatus, timestamp }
    try {
      const result = await sendPaymentSimulation(request)
      setHistory(current => [result, ...current]); setHistoryPage(1); setMessage(`Payment response: HTTP ${result.upstream_status}`)
      if (referenceOrderId) onStarted({ order_id: referenceOrderId } as SimulationResult)
      setPaymentEventId(eventId('payment'))
    } catch (error) { setMessage(error instanceof Error ? error.message : 'Payment simulation failed') } finally { setBusy(false) }
  }

  const totalHistoryPages = Math.max(1, Math.ceil(history.length / HISTORY_PAGE_SIZE))
  const visibleHistory = history.slice((historyPage - 1) * HISTORY_PAGE_SIZE, historyPage * HISTORY_PAGE_SIZE)

  useEffect(() => {
    setHistoryPage(current => Math.min(current, totalHistoryPages))
  }, [totalHistoryPages])

  return <div className="builder">
    <div className="panel-heading"><div><p className="eyebrow">Webhook console</p><h2>Test order and payment events</h2></div></div>
    <div className="simulation-sections">
      <section className="simulation-section simulation-section-sap">
        <div className="simulation-section-heading"><div><p className="eyebrow">Mock SAP</p><h3>Persistent response preset</h3></div></div>
        <div className="simulation-fields">
          <div className="field"><label className="field-label" htmlFor="sap-preset">Preset</label><select id="sap-preset" value={sapPreset} onChange={e => applyPreset(e.target.value)}><option value="">Choose a response…</option>{SAP_PRESETS.map((preset, index) => <option key={preset.label} value={index}>{preset.label}</option>)}</select></div>
          <div className="field"><label className="field-label" htmlFor="sap-status">HTTP status</label><input id="sap-status" type="number" min="200" max="599" value={sapStatus} onChange={e => setSapStatus(Number(e.target.value))} /></div>
          <div className="field" style={{ gridColumn: '1 / -1' }}><label className="field-label" htmlFor="sap-body">JSON body</label><textarea id="sap-body" rows={4} value={sapBody} onChange={e => setSapBody(e.target.value)} /></div>
          <div className="field"><label className="field-label" htmlFor="sap-retry-after">Retry-After <span className="field-hint">optional</span></label><input id="sap-retry-after" value={sapRetryAfter} onChange={e => setSapRetryAfter(e.target.value)} placeholder="7 or HTTP date" /></div>
        </div>
        <details className="advanced-controls"><summary className="advanced-summary">Advanced response controls <span className="advanced-summary-hint">delay the response</span></summary><div className="advanced-grid"><div className="field"><label className="field-label" htmlFor="sap-delay">Response delay (ms)</label><input id="sap-delay" type="number" min="0" max="60000" value={sapDelay} onChange={e => setSapDelay(e.target.value)} placeholder="uses env default" /></div></div></details>
        <div className="builder-actions"><button className="primary-button" type="button" onClick={() => void applySapOverride()}>Apply to all orders</button><button className="secondary-button" type="button" onClick={() => void resetSapOverride()}>Reset</button><button className="secondary-button" type="button" onClick={() => void getMockSapResponse().then(value => { setSapStatus(value.status_code); setSapBody(JSON.stringify(value.body, null, 2)); setSapRetryAfter(value.retry_after ?? ''); setSapDelay(value.delay_ms === undefined ? '' : String(value.delay_ms)); setMessage('Loaded active Mock SAP override') }).catch(() => setMessage('No active Mock SAP override'))}>Load active</button></div>
      </section>
      <form className="simulation-section simulation-section-order" onSubmit={submitOrder}>
        <div className="simulation-section-heading"><div><p className="eyebrow">Shop webhook</p><h3>Create order</h3></div></div>
        <div className="simulation-fields">
          <div className="field"><label className="field-label" htmlFor="simulation-order-event-id">Event ID</label><input id="simulation-order-event-id" value={orderEventId} onChange={e => setOrderEventId(e.target.value)} /></div>
          <div className="field"><label className="field-label" htmlFor="simulation-order-id">Order ID</label><input id="simulation-order-id" value={orderId} onChange={e => { setOrderId(e.target.value); setReferenceOrderId(e.target.value) }} /></div>
          <div className="field"><label className="field-label" htmlFor="simulation-customer-email">Customer email</label><input id="simulation-customer-email" type="email" value={customerEmail} onChange={e => setCustomerEmail(e.target.value)} /></div>
        </div>
        <div className="item-editor"><div className="item-editor-heading"><span>Order items</span><button className="secondary-button" type="button" onClick={() => setItems([...items, { sku: '', quantity: 1, price: 0 }])}>+ Add item</button></div>
          {items.map((item, index) => <div className="item-input-row" key={index}><div className="field"><label className="field-label" htmlFor={`simulation-item-sku-${index}`}>SKU</label><input id={`simulation-item-sku-${index}`} value={item.sku} onChange={e => updateItem(index, 'sku', e.target.value)} /></div><div className="field"><label className="field-label" htmlFor={`simulation-item-quantity-${index}`}>Quantity</label><input id={`simulation-item-quantity-${index}`} type="number" min="1" value={item.quantity} onChange={e => updateItem(index, 'quantity', Number(e.target.value))} /></div><div className="field"><label className="field-label" htmlFor={`simulation-item-price-${index}`}>Unit price</label><input id={`simulation-item-price-${index}`} type="number" min="0" step="0.01" value={item.price} onChange={e => updateItem(index, 'price', Number(e.target.value))} /></div><div className="field item-hardware-field"><label className="field-label" htmlFor={`simulation-item-hardware-${index}`}>Hardware</label><select id={`simulation-item-hardware-${index}`} value={item.isHardware === undefined ? '' : String(item.isHardware)} onChange={e => updateItem(index, 'isHardware', e.target.value === '' ? undefined : e.target.value === 'true')}><option value="">Not specified</option><option value="true">Yes</option><option value="false">No</option></select></div><button className="icon-button" type="button" disabled={items.length === 1} onClick={() => setItems(items.filter((_, itemIndex) => itemIndex !== index))}>×</button></div>)}
        </div>
        <button className="primary-button" disabled={busy} type="submit">Create order</button>
      </form>
      <form className="simulation-section simulation-section-payment" onSubmit={submitPayment}>
        <div className="simulation-section-heading"><div><p className="eyebrow">Payment webhook</p><h3>Send payment event</h3></div></div>
        <div className="simulation-fields">
          <div className="field"><label className="field-label" htmlFor="simulation-payment-event-id">Event ID</label><input id="simulation-payment-event-id" value={paymentEventId} onChange={e => setPaymentEventId(e.target.value)} /></div>
          <div className="field"><label className="field-label" htmlFor="simulation-reference-order-id">Reference order ID</label><input id="simulation-reference-order-id" value={referenceOrderId} onChange={e => setReferenceOrderId(e.target.value)} /></div>
          <div className="field"><label className="field-label" htmlFor="simulation-payment-status">Payment status</label><select id="simulation-payment-status" value={paymentStatus} onChange={e => setPaymentStatus(e.target.value)}><option>PENDING</option><option>COMPLETED</option><option>FAILED</option><option>CANCELLED</option></select></div>
          <div className="field"><label className="field-label" htmlFor="simulation-payment-timestamp">Timestamp</label><input id="simulation-payment-timestamp" value={timestamp} onChange={e => setTimestamp(e.target.value)} /></div>
        </div>
        <button className="primary-button" disabled={busy} type="submit">Send payment</button>
      </form>
    </div>
    <div className="builder-actions"><span className="builder-message">{message}</span></div>
    {history.length > 0 && <section className="simulation-history"><div className="subheading"><div><p className="eyebrow">Downstream responses</p><h3>Webhook history</h3></div></div>{visibleHistory.map((response, index) => <article className="simulation-history-entry" key={`${response.event_id}-${(historyPage - 1) * HISTORY_PAGE_SIZE + index}`}><div><strong>{response.kind} · HTTP {response.upstream_status}</strong><span>event_id: {response.event_id}</span><span>{response.order_id ?? response.reference_order_id}</span></div><pre>{responseText(response)}</pre></article>)}<div className="history-pagination"><button className="secondary-button" type="button" onClick={() => setHistoryPage(current => Math.max(1, current - 1))} disabled={historyPage === 1}>Previous</button><span aria-live="polite">Page {historyPage} of {totalHistoryPages}</span><button className="secondary-button" type="button" onClick={() => setHistoryPage(current => Math.min(totalHistoryPages, current + 1))} disabled={historyPage === totalHistoryPages}>Next</button></div></section>}
  </div>
}
