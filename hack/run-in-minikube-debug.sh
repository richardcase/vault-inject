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
make build-debug
docker build -f ${APP_ROOT}/Dockerfile-debug -t richardcase/vault-admission:0.0.0d ${APP_ROOT}

#docker build -f ${APP_ROOT}/Dockerfile-debug -t richardcase/vault-admission:0.0.0d ${APP_ROOT}

# Run in minikube
#kubectl run vault-admission --image=richardcase/vault-admission:0.0.0d --image-pull-policy=Never --port 40000 --port 443 --labels=role=vault-admission --expose 
kubectl apply -f artifacts/admission/deployment_debug.yaml

# Check that it's running
kubectl get pods
