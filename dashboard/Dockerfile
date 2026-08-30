# syntax=docker/dockerfile:1

FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --cache /root/.npm --prefer-offline
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY *.go ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dashboard .

FROM alpine:3.22
WORKDIR /app
COPY --from=backend /out/dashboard /app/dashboard
COPY --from=frontend /web/dist /app/web/dist
USER nobody
EXPOSE 3001
ENTRYPOINT ["/app/dashboard"]
