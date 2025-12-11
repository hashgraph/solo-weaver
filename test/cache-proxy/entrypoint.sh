#!/bin/bash
# SPDX-License-Identifier: Apache-2.0

set -e

CA_CERT_FILE="/etc/squid/certs/ca-cert.pem"
CA_KEY_FILE="/etc/squid/certs/ca-key.pem"
SSL_DB_DIR="/var/lib/squid/ssl_db"

# Function to generate CA certificate
generate_ca_cert() {
    echo "Generating new CA certificate for SSL MITM..."

    mkdir -p /etc/squid/certs

    # Generate CA private key
    openssl genrsa -out "$CA_KEY_FILE" 2048

    # Generate CA certificate
    openssl req -new -x509 -days 3650 \
        -key "$CA_KEY_FILE" \
        -out "$CA_CERT_FILE" \
        -subj "/C=US/ST=California/L=San Francisco/O=Solo Weaver/OU=Cache Proxy/CN=Solo Weaver Cache Proxy CA"

    chmod 644 "$CA_CERT_FILE"
    chmod 600 "$CA_KEY_FILE"

    echo "✓ CA certificate generated"
    echo "  Certificate: $CA_CERT_FILE"
    echo "  Private key: $CA_KEY_FILE"
    echo ""
    echo "⚠️  IMPORTANT: Install CA certificate on clients for HTTPS caching to work"
    echo "   To install on Linux: cp $CA_CERT_FILE /usr/local/share/ca-certificates/ && update-ca-certificates"
}

# Check if CA certificate exists, generate if not
if [ ! -f "$CA_CERT_FILE" ] || [ ! -f "$CA_KEY_FILE" ]; then
    generate_ca_cert
else
    echo "Using existing CA certificate: $CA_CERT_FILE"
    openssl x509 -in "$CA_CERT_FILE" -noout -subject -dates
fi

# Initialize SSL certificate database
echo "Initializing SSL certificate database..."
if [ ! -f "$SSL_DB_DIR/index.txt" ]; then
    echo "Creating SSL certificate database..."
    # Remove and recreate directory to ensure it's clean
    rm -rf "$SSL_DB_DIR"
    mkdir -p "$(dirname $SSL_DB_DIR)"
    /usr/lib/squid/security_file_certgen -c -s "$SSL_DB_DIR" -M 16MB
    chown -R proxy:proxy "$SSL_DB_DIR"
    chmod -R 750 "$SSL_DB_DIR"
    echo "✓ SSL certificate database created"
else
    echo "✓ SSL certificate database already exists"
    # Ensure proper permissions anyway
    chown -R proxy:proxy "$SSL_DB_DIR"
    chmod -R 750 "$SSL_DB_DIR"
fi

# Ensure proper permissions first
chown -R proxy:proxy /var/cache/squid
chown -R proxy:proxy /var/log/squid
chmod 750 /var/cache/squid

# Create cache directory structure
echo "Setting up cache directories..."
# Check if cache is properly initialized by looking for multiple subdirectories
CACHE_DIRS=$(find /var/cache/squid -maxdepth 1 -type d -name '[0-9]*' | wc -l)
if [ "$CACHE_DIRS" -lt 10 ]; then
    echo "Initializing cache directories..."
    # Create a minimal config for cache initialization (avoids SSL cert requirements)
    cat > /tmp/squid-init.conf <<EOF
http_port 3128
cache_dir ufs /var/cache/squid 10000 16 256
pid_filename /var/run/squid/squid.pid
EOF
    # Initialize cache directories (must be done as proxy user)
    su -s /bin/sh proxy -c "squid -f /tmp/squid-init.conf -z 2>&1" | head -20
    rm -f /tmp/squid-init.conf
    echo "✓ Cache directories created"
else
    echo "✓ Cache directories already initialized ($CACHE_DIRS subdirectories found)"
fi


echo ""
echo "Starting Squid cache proxy..."
echo "Listening on: 0.0.0.0:3128"
echo "Cache directory: /var/cache/squid (10GB)"
echo "SSL MITM: Enabled"
echo ""

# Start Squid in foreground mode
exec squid -N -d 1


