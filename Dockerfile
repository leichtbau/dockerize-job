ARG ARCH=amd64

FROM $ARCH/golang:1.13.4-alpine3.10 AS binary

RUN apk add -U git

WORKDIR /app
ADD main.go exec.go go.mod go.sum ./

RUN go install

FROM $ARCH/alpine:3.10

COPY --from=binary /go/bin/dockerize-job /usr/local/bin

ENTRYPOINT ["dockerize-job"]
CMD ["--help"]
