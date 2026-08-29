# AGENTS.md

This repository owns the dashboard, mock-SAP service, and Adminer development tooling. PostgreSQL and order-sync run from the sibling order-sync repository on the shared Docker network `order-sync-shared`.

Start the stacks independently:

```powershell
cd C:\work\projects\order-sync
docker compose up -d --build

cd C:\work\projects\order-sync-dashboard
docker compose up -d --build
```

The helper services must remain cross-project-safe: do not add `depends_on` entries for PostgreSQL or order-sync. Both Compose files declare the fixed network name `order-sync-shared`.
