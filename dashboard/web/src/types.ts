export type WorkflowStatus = 'success' | 'active' | 'pending' | 'error' | 'skipped' | 'cancelled'

export interface Summary {
  total_orders: number
  completed_orders: number
  pending_orders: number
  hardware_delay_seconds: number
  processing_jobs: number
  retrying_jobs: number
  waiting_jobs: number
  failed_jobs: number
  unscheduled_orders: number
  webhook_events: number
  updated_at: string
}

export interface OrderItem {
  sku: string
  quantity: number
  price: number
  is_hardware: boolean
}

export interface DashboardOrder {
  order_id: string
  customer_email: string
  status: string
  payment_status: string
  paid_at?: string
  created_at: string
  updated_at: string
  sync_status: string
  due_at?: string
  attempts?: number
  last_error?: string
  sap_internal_id?: string
  synced_at?: string
  item_count: number
  total_value: number
  has_hardware: boolean
}

export interface WebhookEvent {
  event_id: string
  event_type: string
  payload: Record<string, unknown>
  processed_at?: string
  created_at: string
}

export interface WorkflowNode {
  id: string
  label: string
  status: WorkflowStatus
  detail?: string
  timestamp?: string
}

export interface WorkflowEdge {
  id: string
  source: string
  target: string
  label?: string
  detail?: string
  status: WorkflowStatus
}

export interface Workflow {
  nodes: WorkflowNode[]
  edges: WorkflowEdge[]
}

export interface OrderDetail {
  order: DashboardOrder
  items: OrderItem[]
  events: WebhookEvent[]
  workflow: Workflow
}

export interface SimulationItem {
  sku: string
  quantity: number
  price: number
  isHardware?: boolean
}

export interface OrderSimulationRequest {
  event_id: string
  order_id: string
  customer_email: string
  items: SimulationItem[]
}

export interface PaymentSimulationRequest {
  event_id: string
  reference_order_id: string
  payment_status: string
  timestamp: string
}

export interface WebhookSimulationResponse {
  kind: 'order' | 'payment'
  event_id: string
  order_id?: string
  reference_order_id?: string
  upstream_status: number
  response_body?: unknown
}

export interface SimulationResult {
  order_id: string
}

export interface MockSapResponseOverride {
  status_code: number
  body: Record<string, unknown>
  retry_after?: string
  delay_ms?: number
}
