#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})
APP_ROOT=${SCRIPT_ROOT}/..

# Check telepresence is installed
# command -v telepresence >/dev/null 2>&1 || { echo >&2 "Telepresence is required.  Aborting."; exit 1; }

# Deploy the k8s artifacts that are needed
kubectl create -f artifacts/admission/configmap.yaml
kubectl create -f artifacts/admission/secret.yaml
kubectl create -f artifacts/admission/crd.yaml
kubectl create -f artifacts/admission/registration.yaml
kubectl create -f artifacts/admission/service.yaml

kubectl create -f artifacts/admission/service_debug.yaml

# Deploy the TLS certs
kubectl create secret tls vault-admission-tls --cert=hack/testcerts/server.crt --key=hack/testcerts/server.key

# Deploy CA key
kubectl create secret generic test-ca --from-file=hack/testcerts/ca.crt

# Show what needs to be done then
#echo "Start the service locally via CLI or in debugger."
#echo "Then start telepresence: telepresence --context minikube --new-deployment vault-admission --expose 8000"
