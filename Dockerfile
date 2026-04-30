# Build stage
# TODO: Pin to specific digest for reproducible builds
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/nous ./cmd/nous

# Final stage
# TODO: Pin to specific digest for reproducible builds
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /bin/nous /bin/nous

EXPOSE 50051 8080

ENTRYPOINT ["/bin/nous"]
