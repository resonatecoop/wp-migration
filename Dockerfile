FROM golang:latest

RUN mkdir /build

WORKDIR /build

RUN export GO111MODULE=on
RUN apt-get -y update
RUN go get github.com/resonatecoop/wp-migration@latest
RUN cd /build && git clone https://github.com/resonatecoop/wp-migration

RUN cd wp-migration && go build -o main

CMD ["./main"]
