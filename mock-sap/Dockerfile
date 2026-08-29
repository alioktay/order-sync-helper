FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go server.go ./
RUN CGO_ENABLED=0 go build -o /out/mock-sap .

FROM alpine:3.22
WORKDIR /app
COPY --from=build /out/mock-sap /app/mock-sap
USER nobody
ENTRYPOINT ["/app/mock-sap"]
