# Build stage with gcc toolchain for cgo Uber H3 compilation
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache build-base git gcc

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=1

RUN go build -ldflags="-s -w" -o /bin/gateway ./cmd/gateway
RUN go build -ldflags="-s -w" -o /bin/dispatch ./cmd/dispatch
RUN go build -ldflags="-s -w" -o /bin/archiver ./cmd/archiver
RUN go build -ldflags="-s -w" -o /bin/baseline ./cmd/baseline
RUN go build -ldflags="-s -w" -o /bin/simulator ./cmd/simulator

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /bin/gateway /app/gateway
COPY --from=builder /bin/dispatch /app/dispatch
COPY --from=builder /bin/archiver /app/archiver
COPY --from=builder /bin/baseline /app/baseline
COPY --from=builder /bin/simulator /app/simulator

EXPOSE 8080 8081 8089 9090

ENTRYPOINT ["/app/gateway"]

