#!/usr/bin/env bash
set -euo pipefail

# pull-certs.sh
# Usage: ./hack/pull-certs.sh <namespace> <secretname>
# Fetches a Kubernetes Secret and writes each entry from .data into the certs/ directory.
# Keys are used as filenames. Values are base64-decoded.

die() {
  echo "ERROR: $*" >&2
  exit 1
}

if ! command -v kubectl >/dev/null 2>&1; then
  die "kubectl is required but not found in PATH"
fi

if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <namespace> <secretname>"
  echo "Example: $0 default my-tls-secret"
  exit 2
fi

NAMESPACE="$1"
SECRET_NAME="$2"
OUT_DIR="$(dirname "$0")/../certs"

mkdir -p "$OUT_DIR"
echo "Fetching secret '$SECRET_NAME' from namespace '$NAMESPACE'..."
kubectl -n "$NAMESPACE" get secret "$SECRET_NAME" -o template='{{range $key, $value := .data}}{{printf "%s %s\n" $key $value}}{{end}}' | while read -r key value; do
  echo "Writing $key to $OUT_DIR/$key"
  echo "$value" | base64 --decode > "$OUT_DIR/$key"
done

echo "Done. Files in $OUT_DIR:"
ls -l "$OUT_DIR"
exit 0
