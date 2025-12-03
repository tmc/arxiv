FROM golang:1.24-alpine AS builder
WORKDIR /build

# Enable CGO and install build tools for sqlite3
ENV CGO_ENABLED=1
RUN apk add --no-cache build-base

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && go build -o arxiv-server ./cmd/arxiv

FROM alpine:latest
RUN apk add --no-cache ca-certificates poppler-utils
WORKDIR /app
COPY --from=builder /build/arxiv-server .
EXPOSE 80
ENV ARXIV_CACHE=/data/arxiv
CMD ["./arxiv-server", "serve", "-port", "80"]
