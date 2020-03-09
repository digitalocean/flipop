FROM flipop-dev AS flipopbuilder
WORKDIR /go/src/github.com/digitalocean/flipop
COPY . /go/src/github.com/digitalocean/flipop
RUN go build ./cmd/flipop

FROM debian:buster-slim
WORKDIR /app
COPY --from=flipopbuilder /go/src/github.com/digitalocean/flipop/flipop /app/flipop

CMD ["/flipop"]