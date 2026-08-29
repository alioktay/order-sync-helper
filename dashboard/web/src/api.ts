import type {
  DashboardOrder,
  OrderDetail,
  OrderSimulationRequest,
  PaymentSimulationRequest,
  WebhookSimulationResponse,
  Summary,
  Workflow,
} from './types'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { 'content-type': 'application/json', ...(init?.headers ?? {}) },
    ...init,
  })
  if (!response.ok) {
    let message = `Request failed (${response.status})`
    try {
      const body = await response.json() as { detail?: string; error?: string }
      message = body.detail || body.error || message
    } catch {
      // Keep the HTTP status when the server did not return JSON.
    }
    throw new Error(message)
  }
  return response.json() as Promise<T>
}

export async function getSummary(): Promise<Summary> {
  return request<Summary>('/api/dashboard/summary')
}

export async function getOrders(q: string, status: string): Promise<DashboardOrder[]> {
  const params = new URLSearchParams({ limit: '100' })
  if (q) params.set('q', q)
  if (status) params.set('status', status)
  const result = await request<{ orders: DashboardOrder[] }>(`/api/dashboard/orders?${params}`)
  return result.orders
}

export function getOrder(orderId: string): Promise<OrderDetail> {
  return request<OrderDetail>(`/api/dashboard/orders/${encodeURIComponent(orderId)}`)
}

export function getWorkflow(orderId: string): Promise<Workflow> {
  return request<Workflow>(`/api/dashboard/orders/${encodeURIComponent(orderId)}/workflow`)
}

export function sendOrderSimulation(input: OrderSimulationRequest): Promise<WebhookSimulationResponse> {
  return request<WebhookSimulationResponse>('/api/dashboard/simulations/order', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function sendPaymentSimulation(input: PaymentSimulationRequest): Promise<WebhookSimulationResponse> {
  return request<WebhookSimulationResponse>('/api/dashboard/simulations/payment', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}
