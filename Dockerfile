FROM flipop-dev AS flipopbuilder
ARG FLIPOP_VERSION dev
WORKDIR /go/src/github.com/digitalocean/flipop
COPY . /go/src/github.com/digitalocean/flipop
RUN go build -ldflags "-s -w -X github.com/digitalocean/flipop/pkg/version.Version=$FLIPOP_VERSION" ./cmd/flipop

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=flipopbuilder /go/src/github.com/digitalocean/flipop/flipop /app/flipop
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
CMD ["/app/flipop"]