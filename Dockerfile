FROM golang:1.25-alpine AS builder

RUN mkdir /src
COPY ./ /src

WORKDIR /src
RUN go build .
RUN chmod +x /src/home_controller

FROM alpine:3.23

COPY --from=builder /src/home_controller /home_controller/
COPY --from=builder /src/web /home_controller/web
WORKDIR /home_controller
CMD ["./home_controller"]