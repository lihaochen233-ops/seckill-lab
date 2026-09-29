FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/mall/dist ./web/mall/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mall ./cmd/mall && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mallbench ./cmd/mallbench

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 mall
USER mall
WORKDIR /app
COPY --from=backend /mall /app/mall
COPY --from=backend /mallbench /app/mallbench
EXPOSE 8088
ENTRYPOINT ["/app/mall"]
