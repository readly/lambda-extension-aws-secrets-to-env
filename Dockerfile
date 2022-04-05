FROM docker.io/golang:1.17-alpine AS builder
WORKDIR /go/build
COPY . /go/build
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o lambda-extension-aws-secrets-to-env

FROM scratch
COPY --from=builder /go/build/lambda-extension-aws-secrets-to-env /opt/extensions/
