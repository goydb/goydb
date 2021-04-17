FROM golang:1.16.3 AS builder

RUN mkdir /app
WORKDIR /app

COPY . .

RUN CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o /usr/local/bin/goydb ./cmd/goydb

FROM alpine

RUN mkdir -p /usr/local/bin/ /usr/local/share/goydb
COPY --from=builder /usr/local/bin/goydb /usr/local/bin/goydb
COPY --from=builder /app/public /usr/local/share/goydb
RUN chmod 755 /usr/local/bin/goydb
RUN mkdir -p /var/local/goydb/

VOLUME [ "/var/local/goydb/" ]

EXPOSE 7070

CMD [ "/usr/local/bin/goydb", "-public", "/usr/local/share/goydb", "-dbs", "/var/local/goydb/" ]
