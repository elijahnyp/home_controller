FROM golang:1.24.2-alpine3.21 as BUILDER

RUN mkdir /src
COPY ./ /src

WORKDIR /src
RUN go build .
RUN chmod +x /src/home_controller

FROM alpine:3.21

COPY --from=BUILDER /src/home_controller /home_controller/
CMD /home_controller/home_controller