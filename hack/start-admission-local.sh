#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail


SCRIPT_ROOT=$(dirname "${BASH_SOURCE}")/..

# Build the controller first
make build

# Start the controller
${SCRIPT_ROOT}/vault-admission \
    -tlsCertFile=${SCRIPT_ROOT}/hack/testcerts/ServerCert.pem \
    -tlsKeyFile=${SCRIPT_ROOT}/hack/testcerts/ServerKey.pem \
    -caCertFile=${SCRIPT_ROOT}/hack/testcerts/CACert.pem \
    -kubeconfig=${HOME}/.kube/config \
    -logtostderr=true \
    -v=2