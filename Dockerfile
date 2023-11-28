FROM golang:1.21 as builder
WORKDIR $GOPATH/src
COPY ./ .
RUN CGO_ENABLED=0 go build -o /usr/local/bin/client ./client/...
RUN CGO_ENABLED=0 go build -o /usr/local/bin/server ./server/...

FROM scratch
COPY --from=builder /usr/local/bin/server /server
COPY --from=builder /usr/local/bin/client /client
ENTRYPOINT ["/server"]
