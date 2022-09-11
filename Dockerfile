FROM alpine:3.16.2 as builder
RUN apk --no-cache add ca-certificates

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY hover-ddns /

ENTRYPOINT ["/hover-ddns" "--config" "/hover-ddns.yaml"]
