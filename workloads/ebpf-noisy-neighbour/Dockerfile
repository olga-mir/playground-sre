FROM golang:1.25 AS builder

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
    clang \
    llvm \
    linux-headers-amd64 \
    linux-libc-dev \
    libbpf-dev \
    gcc-multilib \
    && rm -rf /var/lib/apt/lists/*

#RUN bpftool btf dump file /sys/kernel/btf/vmlinux format c > vmlinux.h
RUN go install github.com/cilium/ebpf/cmd/bpf2go@latest

# Create symlink for asm/types.h
RUN ln -s /usr/include/x86_64-linux-gnu/asm /usr/include/asm

WORKDIR /app

COPY bpf/* ./bpf/
COPY go.* ./
COPY *.go ./

# Download dependencies and generate eBPF code with verbose output
RUN go mod download && \
    go generate -v ./... && \
    ls -la && \
    find . -name "*.go"

RUN CGO_ENABLED=0 go build -o loader

FROM ubuntu:24.10

WORKDIR /app
COPY --from=builder /app/loader .

ENTRYPOINT ["/app/loader"]
