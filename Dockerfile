FROM golang:1.17.7 as builder
LABEL maintainer="SkynetLabs <devs@skynetlabs.com>"

WORKDIR /root

ENV CGO_ENABLED=0

COPY api api
COPY clamav clamav
COPY database database
COPY scanner scanner
COPY scanner scanner
COPY go.mod go.sum main.go Makefile ./

RUN go mod download && make release

FROM alpine:3.15.0
LABEL maintainer="SkynetLabs <devs@skynetlabs.com>"

COPY --from=builder /go/bin/malware-scanner /usr/bin/malware-scanner

ENTRYPOINT ["malware-scanner"]
