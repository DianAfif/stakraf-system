FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN go mod tidy && go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api
FROM alpine:3.21
RUN addgroup -S stakraf && adduser -S stakraf -G stakraf
USER stakraf
COPY --from=build /out/api /api
EXPOSE 8080
CMD ["/api"]
