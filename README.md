# Order Sync Dashboard Helpers

Development helpers for the order-sync service: the workflow dashboard, mock SAP API, and Adminer.

Start order-sync and PostgreSQL first or start this stack first; both stacks use the automatically created fixed Docker network `order-sync-shared` and the helper services restart while their peers become available.

```powershell
cd C:\work\projects\order-sync
docker compose up -d --build

cd C:\work\projects\order-sync-dashboard
docker compose up -d --build
```

URLs: dashboard `http://localhost:3001`, mock SAP `http://localhost:4000`, and Adminer `http://localhost:8080`. Adminer is prefilled for PostgreSQL at `postgres:5432`, using `order_sync` / `order_sync` and database `order_sync`.

The dashboard connects to `postgres:5432`, `http://order-sync:3000`, and `http://mock-sap:4000` inside the shared network. `ORDER_SYNC_DATABASE_URL` is the dashboard container's internal PostgreSQL URL; keep it aligned with the PostgreSQL credentials and `ORDER_SYNC_DATABASE_URL` used by the order-sync stack. `DATABASE_URL` remains the host-running-dashboard URL. `HARDWARE_SYNC_DELAY_SECONDS` and `WEBHOOK_SECRET` should also match between stacks when those features are enabled.

Configure mock-SAP behavior globally with `MOCK_SAP_MODE` and `MOCK_SAP_DELAY_MS` in `.env`. The mock-SAP service exposes only its health, order, and received-order endpoints; inspect its request/response logs with `docker compose logs -f mock-sap`. Listener reconnect backoff, startup recovery of stale jobs, and dispatch timing telemetry belong to the order-sync service; this helper dashboard displays the persisted order state without duplicating that worker logic.

Run service tests with `(cd dashboard; go test ./...)` and `(cd mock-sap; go test ./...)`; run the frontend checks from `dashboard/web`.
