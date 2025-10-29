FROM docker.io/library/alpine:3.22
# TARGETOS TARGETARCH already set by '--platform'
ARG TARGETOS TARGETARCH
COPY ${TARGETOS}-${TARGETARCH}/ /usr/local/bin/
WORKDIR /app
