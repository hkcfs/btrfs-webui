# Build Stage
FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o btrfs-manager .

# Run Stage
FROM alpine:latest

# Install BTRFS tools, Compsize, and Timezone data
# compsize is usually in the community repo
RUN apk add --no-cache btrfs-progs btrfs-compsize tzdata ca-certificates

WORKDIR /root/
COPY --from=builder /app/btrfs-manager .

# Create directory for persisting app state
RUN mkdir /data

EXPOSE 8080

CMD ["./btrfs-manager"]
