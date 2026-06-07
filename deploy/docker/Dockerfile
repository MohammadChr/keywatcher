# Builder stage: compile the Go binary
FROM golang:1.25-alpine AS builder

ARG VERSION=dev

WORKDIR /src

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary (statically linked, no CGO)
# Include version in binary if needed (can be accessed via ldflags)
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /keywatcher ./main.go

# Final stage: minimal runtime image
FROM scratch

# Copy the binary from builder
COPY --from=builder /keywatcher /keywatcher

# Copy static files
COPY internal/server/static /internal/server/static

WORKDIR /
EXPOSE 8080
ENTRYPOINT ["/keywatcher"]
