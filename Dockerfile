ARG RELEASE_TAG=master
FROM golang:latest

ARG RELEASE_TAG

RUN mkdir /build

WORKDIR /build

RUN export GO111MODULE=on
RUN apt-get -y update
RUN go get github.com/resonatecoop/wp-migration@${RELEASE_TAG}
RUN cd /build && git clone --branch ${RELEASE_TAG} --single-branch --depth 1 https://github.com/resonatecoop/wp-migration

RUN cd wp-migration && go build -o main

CMD ["./main"]
