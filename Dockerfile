# Build stage
FROM golang:1.9 as builder
WORKDIR /go/src/github.com/richardcase/vault-admission
ADD . .
RUN make setup && make build-prod


# Final Stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates

COPY --from=builder /go/src/github.comrichardcase/vault-admission/vault-admission /app/vault-admission
ENTRYPOINT /app/vault-admission

# Build-time metadata as defined at http://label-schema.org
ARG BUILD_DATE
ARG VCS_REF
ARG VERSION
LABEL org.label-schema.build-date=$BUILD_DATE \
    org.label-schema.name="Vault Kubernetes Admission Controller" \
    org.label-schema.description="A Kubernetes admission controller that injects secrets from Vault" \
    org.label-schema.url="https://github.com/richardcase/vault-inject" \
    org.label-schema.vcs-ref=$VCS_REF \
    org.label-schema.vcs-url="https://github.com/richardcase/vault-inject" \
    org.label-schema.vendor="Richard Case" \
    org.label-schema.version=$VERSION \
    org.label-schema.schema-version="1.0"
