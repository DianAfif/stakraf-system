FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/agents/stakraf-agent-linux-amd64 ./cmd/agent
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o /out/agents/stakraf-agent-linux-arm64 ./cmd/agent
FROM alpine:3.21
RUN addgroup -S stakraf && adduser -S stakraf -G stakraf
USER stakraf
COPY --from=build /out/api /api
COPY --from=build /out/agents /opt/stakraf/agents
ENV AGENT_ASSETS_DIR=/opt/stakraf/agents
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --start-period=15s --retries=6 CMD ["/api", "--healthcheck"]
CMD ["/api"]
