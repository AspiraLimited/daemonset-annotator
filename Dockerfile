FROM golang:1.22-alpine as build

WORKDIR /build

COPY ./src/ ./
RUN go mod download

RUN CGO_ENABLED=0 go build -o ./annotating-webhook

FROM alpine:3.10

COPY --from=build /build/annotating-webhook /usr/local/bin/annotating-webhook

RUN chmod +x /usr/local/bin/annotating-webhook

ENTRYPOINT ["annotating-webhook"]
