# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY main.go server.go ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mock-sap .

FROM alpine:3.22
WORKDIR /app
COPY --from=build /out/mock-sap /app/mock-sap
USER nobody
ENTRYPOINT ["/app/mock-sap"]
