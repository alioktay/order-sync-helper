package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

type Summary struct {
	TotalOrders          int64     `json:"total_orders"`
	CompletedOrders      int64     `json:"completed_orders"`
	PendingOrders        int64     `json:"pending_orders"`
	HardwareDelaySeconds int       `json:"hardware_delay_seconds"`
	ProcessingJobs       int64     `json:"processing_jobs"`
	RetryingJobs         int64     `json:"retrying_jobs"`
	WaitingJobs          int64     `json:"waiting_jobs"`
	FailedJobs           int64     `json:"failed_jobs"`
	Unscheduled          int64     `json:"unscheduled_orders"`
	WebhookEvents        int64     `json:"webhook_events"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type DashboardOrder struct {
	OrderID       string     `json:"order_id"`
	CustomerEmail string     `json:"customer_email"`
	Status        string     `json:"status"`
	PaymentStatus string     `json:"payment_status"`
	PaidAt        *time.Time `json:"paid_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	SyncStatus    string     `json:"sync_status"`
	DueAt         *time.Time `json:"due_at,omitempty"`
	Attempts      *int       `json:"attempts,omitempty"`
	LastError     *string    `json:"last_error,omitempty"`
	SAPInternalID *string    `json:"sap_internal_id,omitempty"`
	SyncedAt      *time.Time `json:"synced_at,omitempty"`
	ItemCount     int64      `json:"item_count"`
	TotalValue    float64    `json:"total_value"`
	HasHardware   bool       `json:"has_hardware"`
}

type OrderItem struct {
	SKU        string  `json:"sku"`
	Quantity   int     `json:"quantity"`
	Price      float64 `json:"price"`
	IsHardware bool    `json:"is_hardware"`
}

type WebhookEvent struct {
	EventID     string          `json:"event_id"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload"`
	ProcessedAt *time.Time      `json:"processed_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type OrderDetail struct {
	Order  DashboardOrder `json:"order"`
	Items  []OrderItem    `json:"items"`
	Events []WebhookEvent `json:"events"`
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Summary(ctx context.Context) (Summary, error) {
	var summary Summary
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total_orders,
			COUNT(*) FILTER (WHERE o.payment_status = 'PAID' AND j.status = 'SYNCED') AS completed_orders,
			COUNT(*) FILTER (WHERE o.payment_status = 'PENDING') AS pending_orders,
			COUNT(*) FILTER (WHERE j.status = 'PROCESSING') AS processing_jobs,
			COUNT(*) FILTER (WHERE j.status IN ('PENDING', 'WAITING') AND j.last_error IS NOT NULL) AS retrying_jobs,
			COUNT(*) FILTER (WHERE j.status = 'WAITING') AS waiting_jobs,
			COUNT(*) FILTER (WHERE j.status = 'DEAD') AS failed_jobs,
			COUNT(*) FILTER (WHERE j.id IS NULL) AS unscheduled_orders,
			(SELECT COUNT(*) FROM webhook_events),
			NOW()
		FROM orders o
		LEFT JOIN sync_jobs j ON j.order_id = o.id`).Scan(
		&summary.TotalOrders,
		&summary.CompletedOrders,
		&summary.PendingOrders,
		&summary.ProcessingJobs,
		&summary.RetryingJobs,
		&summary.WaitingJobs,
		&summary.FailedJobs,
		&summary.Unscheduled,
		&summary.WebhookEvents,
		&summary.UpdatedAt,
	)
	if err != nil {
		return Summary{}, fmt.Errorf("read dashboard summary: %w", err)
	}
	return summary, nil
}

func (r *Repository) ListOrders(ctx context.Context, query, status string, limit int) ([]DashboardOrder, error) {
	query = strings.TrimSpace(query)
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			o.order_id, o.customer_email, o.payment_status, COALESCE(p.status, 'NOT_RECEIVED'), o.paid_at, o.created_at, o.updated_at,
			COALESCE(j.status, 'NOT_SCHEDULED'), j.due_at, j.attempts, j.last_error,
			j.sap_internal_id, j.synced_at, COUNT(i.id), COALESCE(SUM(i.quantity * i.price), 0)::float8,
			COALESCE(BOOL_OR(COALESCE(i.is_hardware, sc.category = 'HARDWARE', false)), false)
		FROM orders o
		LEFT JOIN sync_jobs j ON j.order_id = o.id
		LEFT JOIN payments p ON p.order_id = o.id
		LEFT JOIN order_items i ON i.order_id = o.id
		LEFT JOIN sku_classifications sc ON sc.sku = i.sku
		WHERE ($1 = '' OR o.order_id ILIKE '%' || $1 || '%' OR o.customer_email ILIKE '%' || $1 || '%')
		  AND (($2 = 'CANCELLED' AND o.payment_status = 'CANCELLED')
		    OR ($2 <> 'CANCELLED' AND ($2 = '' OR COALESCE(j.status, 'NOT_SCHEDULED') = $2)))
		GROUP BY o.id, o.created_at, j.id, p.id
		ORDER BY o.created_at DESC
		LIMIT $3`, query, strings.ToUpper(strings.TrimSpace(status)), limit)
	if err != nil {
		return nil, fmt.Errorf("list dashboard orders: %w", err)
	}
	defer rows.Close()

	orders := make([]DashboardOrder, 0)
	for rows.Next() {
		order, scanErr := scanOrder(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan dashboard order: %w", scanErr)
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard orders: %w", err)
	}
	return orders, nil
}

func (r *Repository) GetOrder(ctx context.Context, orderID string) (OrderDetail, error) {
	var detail OrderDetail
	row := r.pool.QueryRow(ctx, `
		SELECT
			o.order_id, o.customer_email, o.payment_status, COALESCE(p.status, 'NOT_RECEIVED'), o.paid_at, o.created_at, o.updated_at,
			COALESCE(j.status, 'NOT_SCHEDULED'), j.due_at, j.attempts, j.last_error,
			j.sap_internal_id, j.synced_at, COUNT(i.id), COALESCE(SUM(i.quantity * i.price), 0)::float8,
			COALESCE(BOOL_OR(COALESCE(i.is_hardware, sc.category = 'HARDWARE', false)), false)
		FROM orders o
		LEFT JOIN sync_jobs j ON j.order_id = o.id
		LEFT JOIN payments p ON p.order_id = o.id
		LEFT JOIN order_items i ON i.order_id = o.id
		LEFT JOIN sku_classifications sc ON sc.sku = i.sku
		WHERE o.order_id = $1
		GROUP BY o.id, o.created_at, j.id, p.id`, orderID)
	order, err := scanOrder(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return detail, ErrOrderNotFound
	}
	if err != nil {
		return detail, fmt.Errorf("scan dashboard order %q: %w", orderID, err)
	}
	detail.Order = order

	detail.Items, err = r.loadItems(ctx, orderID)
	if err != nil {
		return detail, fmt.Errorf("load items for dashboard order %q: %w", orderID, err)
	}
	detail.Events, err = r.loadEvents(ctx, orderID)
	if err != nil {
		return detail, fmt.Errorf("load events for dashboard order %q: %w", orderID, err)
	}
	return detail, nil
}

func (r *Repository) loadItems(ctx context.Context, orderID string) ([]OrderItem, error) {
	rows, err := r.pool.Query(ctx, `SELECT i.sku, i.quantity, i.price::float8, COALESCE(i.is_hardware, sc.category = 'HARDWARE', false) AS is_hardware FROM order_items i JOIN orders o ON o.id = i.order_id LEFT JOIN sku_classifications sc ON sc.sku = i.sku WHERE o.order_id = $1 ORDER BY i.id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query dashboard items for order %q: %w", orderID, err)
	}
	defer rows.Close()
	items := make([]OrderItem, 0)
	for rows.Next() {
		var item OrderItem
		if err = rows.Scan(&item.SKU, &item.Quantity, &item.Price, &item.IsHardware); err != nil {
			return nil, fmt.Errorf("scan dashboard item for order %q: %w", orderID, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard items for order %q: %w", orderID, err)
	}
	return items, nil
}

func (r *Repository) loadEvents(ctx context.Context, orderID string) ([]WebhookEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT event_id, event_type, payload, processed_at, created_at
		FROM webhook_events
		WHERE (event_type = 'SHOP' AND payload->>'order_id' = $1)
		   OR (event_type = 'PAYMENT' AND payload->>'reference_order_id' = $1)
		ORDER BY created_at`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query dashboard events for order %q: %w", orderID, err)
	}
	defer rows.Close()
	events := make([]WebhookEvent, 0)
	for rows.Next() {
		var event WebhookEvent
		var payload []byte
		if err = rows.Scan(&event.EventID, &event.EventType, &payload, &event.ProcessedAt, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan dashboard event for order %q: %w", orderID, err)
		}
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard events for order %q: %w", orderID, err)
	}
	return events, nil
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

var ErrOrderNotFound = errors.New("order not found")

type rowScanner interface {
	Scan(dest ...any) error
}

func scanOrder(row rowScanner) (DashboardOrder, error) {
	var order DashboardOrder
	err := row.Scan(
		&order.OrderID,
		&order.CustomerEmail,
		&order.Status,
		&order.PaymentStatus,
		&order.PaidAt,
		&order.CreatedAt,
		&order.UpdatedAt,
		&order.SyncStatus,
		&order.DueAt,
		&order.Attempts,
		&order.LastError,
		&order.SAPInternalID,
		&order.SyncedAt,
		&order.ItemCount,
		&order.TotalValue,
		&order.HasHardware,
	)
	return order, err
}
