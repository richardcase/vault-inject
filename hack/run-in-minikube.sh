#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})
APP_ROOT=${SCRIPT_ROOT}/..

# Set docker env
eval $(minikube docker-env)

# Build
# make -f ${APP_ROOT}/Makefile build-debug

# Build image
docker build -f ${APP_ROOT}/Dockerfile-nobuild -t richardcase/vault-admission:0.0.0d ${APP_ROOT}

# Run in minikube
kubectl run vault-admission --image=richardcase/vault-admission:0.0.0d --image-pull-policy=Never

# Check that it's running
kubectl get pods
