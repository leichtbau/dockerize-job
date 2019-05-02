FROM golang:1.12-alpine AS binary

RUN apk add -U git

WORKDIR /app
ADD main.go exec.go go.mod go.sum ./

RUN go install

FROM alpine:3.6

COPY --from=binary /go/bin/dockerize-job /usr/local/bin

ENTRYPOINT ["dockerize-job"]
CMD ["--help"]
