FROM golang:alpine AS builder
WORKDIR /app

# Copy all source code first
COPY . .

# Now run tidy. This looks at the code, finds the imports, 
# generates go.sum, and downloads dependencies.
RUN go mod tidy

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o btrfs-manager ./cmd/server

# --- Final Stage ---
FROM alpine:latest

# Install dependencies
RUN apk add --no-cache btrfs-progs btrfs-compsize smartmontools tzdata ca-certificates bash

WORKDIR /root/
COPY --from=builder /app/btrfs-manager .
# Ensure static folder is copied
COPY static ./static
RUN mkdir /data

EXPOSE 8080
CMD ["./btrfs-manager"]
