FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY main.go .
RUN go mod init mesh-app && \
    CGO_ENABLED=0 GOOS=linux go build -o mesh-app main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/mesh-app .
CMD ["./mesh-app"]