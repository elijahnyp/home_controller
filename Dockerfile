FROM golang:1.21.6-alpine3.19 as BUILDER

RUN mkdir /src
COPY ./ /src

WORKDIR /src
RUN go build .
RUN chmod +x /src/home_controller

FROM alpine:3.19

COPY --from=BUILDER /src/home_controller /
CMD /home_controller