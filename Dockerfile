FROM golang:1.26-alpine AS build
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/file-transfer-backend .

FROM alpine:3.21
RUN addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=build /out/file-transfer-backend /app/file-transfer-backend
RUN mkdir -p /data && chown -R app:app /data /app
USER app
ENV DATA_DIR=/data ADDR=:8080
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/app/file-transfer-backend"]
