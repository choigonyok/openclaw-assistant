FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/openclaw-assistant ./cmd/openclaw-assistant

FROM alpine:3.21

RUN addgroup -S openclaw && adduser -S openclaw -G openclaw
WORKDIR /app
COPY --from=build /out/openclaw-assistant /app/openclaw-assistant
USER openclaw
EXPOSE 8080

CMD ["/app/openclaw-assistant"]
