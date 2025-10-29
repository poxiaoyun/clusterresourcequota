#!/usr/bin/env sh

TARGET_DIR=certs

mkdir -p "$TARGET_DIR"
cd "$TARGET_DIR"

# Generate CA key and self-signed certificate
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -subj "/CN=clusterresourcequota-ca" -out ca.crt

# Create an OpenSSL config with SANs for the server certificate
cat > openssl.cnf <<'EOF'
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = clusterresourcequota

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = clusterresourcequota
DNS.2 = clusterresourcequota.default
DNS.3 = clusterresourcequota.default.svc
DNS.4 = localhost
IP.1 = 127.0.0.1
EOF

# Generate server key and CSR
openssl genrsa -out tls.key 2048
openssl req -new -key tls.key -out tls.csr -config openssl.cnf

# Sign the CSR with our CA
openssl x509 -req -in tls.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out tls.crt -days 3650 -sha256 -extfile openssl.cnf -extensions v3_req

# Secure the private key
chmod 600 tls.key

# Remove sensitive or temporary files
rm -f tls.csr ca.key openssl.cnf ca.srl

echo "Generated files in $TARGET_DIR:"
ls -l
