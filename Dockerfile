FROM golang:1.15-alpine as builder

# Fetch certs to allow use of TLS.
RUN apk add -U --no-cache ca-certificates git

WORKDIR /build

# Fetch dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Copy code.
COPY main.go ./

# Build the command inside the container.
RUN CGO_ENABLED=0 GOOS=linux go build -v -o main ./main.go

FROM scratch

# Copy the binary to the production image from the builder stage.
COPY --from=builder /build/main /main

# Copy CA certificates (allows HTTPS calls).
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Entrypoint.
ENTRYPOINT ["/main"]
