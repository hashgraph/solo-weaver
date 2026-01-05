# SPDX-License-Identifier: Apache-2.0
# Squid Cache Proxy with SSL MITM for Integration Tests
FROM debian:bookworm-slim

# Install Squid with OpenSSL support and OpenSSL tools
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        squid-openssl \
        ssl-cert \
        openssl \
        ca-certificates \
        perl && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /var/cache/squid \
        /var/log/squid \
        /var/lib/squid/ssl_db \
        /var/run/squid \
        /etc/squid/certs && \
    chown -R proxy:proxy /var/cache/squid \
        /var/log/squid \
        /var/lib/squid \
        /var/run/squid

# Copy Squid configuration
COPY squid.conf /etc/squid/squid.conf

# Copy store-id helper for URL normalization
COPY store-id-helper.pl /usr/local/bin/store-id-helper.pl
RUN chmod +x /usr/local/bin/store-id-helper.pl

# Copy entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Expose proxy port
EXPOSE 3128

# Volume for cache persistence and CA certificates
VOLUME ["/var/cache/squid", "/etc/squid/certs"]

# Run entrypoint
ENTRYPOINT ["/entrypoint.sh"]
