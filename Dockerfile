FROM golang:1.26-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
ARG TARGETARCH
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-arm64} go build -ldflags="-s -w" -o /out/opspilot-agent ./cmd/agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    addgroup -S -g 65532 opspilot && \
    adduser -S -u 65532 -G opspilot opspilot
WORKDIR /var/lib/opspilot
COPY --from=builder /out/opspilot-agent /usr/local/bin/opspilot-agent
USER 65532:65532
ENV OPSPILOT_DATA_DIR=/var/lib/opspilot
ENTRYPOINT ["/usr/local/bin/opspilot-agent"]
