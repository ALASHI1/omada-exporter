FROM golang:1.22-alpine as builder

WORKDIR /app
RUN apk add --no-cache git

# Build the exporter from GitHub
RUN go install github.com/jamessanford/omada-controller-exporter@latest

# Final minimal image
FROM alpine:latest

WORKDIR /app
COPY --from=builder /go/bin/omada-controller-exporter /app/

# Expose the metrics port
EXPOSE 6779

# By default, it will look for a config.yaml
CMD ["./omada-controller-exporter"]
