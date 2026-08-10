FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /waypoint-api ./cmd/api

FROM alpine:3.21
RUN apk add --no-cache poppler-utils tesseract-ocr && adduser -D -u 65532 waypoint
COPY --from=build /waypoint-api /waypoint-api
EXPOSE 8080
USER waypoint
ENTRYPOINT ["/waypoint-api"]
