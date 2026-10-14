# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go/go.mod ./
COPY go/ ./
RUN go build -trimpath -ldflags="-s -w" -o /shave ./cmd/shave

FROM alpine:3.21
RUN adduser -D -u 10001 shave
COPY --from=build /shave /usr/local/bin/shave
USER shave
ENTRYPOINT ["/usr/local/bin/shave"]
CMD ["version"]