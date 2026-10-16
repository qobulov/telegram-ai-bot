# Build stage
FROM golang:1.27 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/telegram-ai-agent ./cmd/bot

# Runtime stage: no shell, no package manager, runs as a non-root user.
# The image ships CA certificates, which HTTPS calls to Telegram and the model provider need.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/telegram-ai-agent /telegram-ai-agent
USER nonroot:nonroot
ENTRYPOINT ["/telegram-ai-agent"]
