
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY main.go .

# Initialize module and build
RUN go mod init client-app && \
    CGO_ENABLED=0 GOOS=linux go build -o client-app main.go

# Final runtime image
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/client-app .

CMD ["./client-app"]