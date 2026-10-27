FROM golang:1.25 AS builder

ARG TAG="dev"
ENV TAG=${TAG}
ENV COMMIT=""

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN make build

# Download yt-dlp
RUN wget -O /usr/bin/yt-dlp https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp && \
    chmod a+rwx /usr/bin/yt-dlp

# Alpine 3.24 will go EOL on 2028-05-01
FROM alpine:3.24

WORKDIR /app

# deno is required for yt-dlp (ref: https://github.com/yt-dlp/yt-dlp/issues/14404)
RUN apk --no-cache add ca-certificates python3 py3-pip ffmpeg tzdata libc6-compat deno

RUN chmod 777 /usr/local/bin
COPY --from=builder /usr/bin/yt-dlp /usr/local/bin/youtube-dl
RUN ln -s /usr/local/bin/youtube-dl /usr/local/bin/yt-dlp
COPY --from=builder /build/bin/podsync /app/podsync
COPY --from=builder /build/html/ /app/html/

ENTRYPOINT ["/app/podsync"]
CMD ["--no-banner"]
