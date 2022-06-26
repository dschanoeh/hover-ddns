FROM scratch
ENTRYPOINT ["/hover-ddns" "--config" "/hover-ddns.yaml"]
COPY hover-ddns /
