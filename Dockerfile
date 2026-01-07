FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY *.go ./
RUN go build -o server main.go
RUN go build -o worker worker.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/worker .
EXPOSE 80
CMD ["./server"]
