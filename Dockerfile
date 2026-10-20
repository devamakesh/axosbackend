FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY main.go ./
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/axosbackend .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/axosbackend /usr/local/bin/axosbackend
USER app

ENV ALIENX_HOST=0.0.0.0
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/axosbackend"]
CMD ["server"]
