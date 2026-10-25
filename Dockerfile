# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.1.0
ARG COMMIT=docker
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/gpu/urna-watch/internal/version.Version=${VERSION} \
      -X github.com/gpu/urna-watch/internal/version.Commit=${COMMIT} \
      -X github.com/gpu/urna-watch/internal/version.Date=${BUILD_DATE}" \
    -o /out/urna-watch ./cmd/urna-watch

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S urna \
    && adduser -S -G urna -h /app urna
WORKDIR /app
COPY --from=build /out/urna-watch /app/urna-watch
RUN mkdir -p /app/data && chown -R urna:urna /app
USER urna
ENV URNA_TUI=false \
    URNA_STATE_PATH=/app/data/state.json \
    URNA_LOG_FORMAT=json
EXPOSE 8080
ENTRYPOINT ["/app/urna-watch"]
