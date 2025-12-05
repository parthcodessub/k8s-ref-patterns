# --- Stage 1: Builder ---
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the dependency definition AND the source code
# We need main.go present for 'go mod tidy' to detect imports
COPY go.mod main.go ./

# Download dependencies
# Now that main.go is there, tidy will see the imports and fetch them
RUN go mod tidy && go mod download

# Build the binary named 'metrics-app'
RUN CGO_ENABLED=0 GOOS=linux go build -o metrics-app main.go

# --- Stage 2: Runtime ---
FROM alpine:latest

WORKDIR /app

# Copy only the compiled binary from the Builder stage
COPY --from=builder /app/metrics-app .

# Expose the port defined in main.go
EXPOSE 2112

# Run the binary
CMD ["./metrics-app"]