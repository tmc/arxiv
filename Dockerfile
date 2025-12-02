FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o arxiv-server ./cmd/arxiv

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /build/arxiv-server .
EXPOSE 80
ENV ARXIV_CACHE=/data/arxiv
CMD ["./arxiv-server", "serve", "-port", "80"]
