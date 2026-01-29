FROM golang:1.19-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o near-lake-supervisor .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates docker-cli docker-compose bash

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /build/near-lake-supervisor .

CMD ["./near-lake-supervisor"]
