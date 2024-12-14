FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o waiter-service

FROM alpine:3.18
WORKDIR /app
COPY --from=builder /app/waiter-service .
EXPOSE 8081
CMD ["./waiter-service"]