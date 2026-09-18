# Image choice: https://docs.cloud.google.com/profiler/docs/profiling-go#running_with_linux_alpine

FROM golang:1.25-bookworm AS builder

ARG GIT_SHA=unknown

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.gitSHA=${GIT_SHA}" -o server ./cmd/api

FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /app/server .

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["./server"]
