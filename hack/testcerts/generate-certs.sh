#!/bin/bash

# Copyright 2017 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http:#www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Generates the a CA cert, a server key/cert, client key/cert signed by
# the CA.
#
# reference: https://github.com/kubernetes/kubernetes/blob/master/plugin/pkg/admission/webhook/gencerts.sh

set -e

[ -z ${service} ] && service=vault-admission
[ -z ${namespace} ] && namespace=default

cat > client.conf <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${service}
DNS.2 = ${service}.${namespace}
DNS.3 = ${service}.${namespace}.svc
EOF

cat > server.conf <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${service}
DNS.2 = ${service}.${namespace}
DNS.3 = ${service}.${namespace}.svc
EOF

# Create a certificate authority
#openssl genrsa -out CAKey.pem 2048
#openssl req -x509 -new -nodes -key CAKey.pem -days 100000 -out CACert.pem -subj "/CN=${CN_BASE}_ca"
### Commented the above out to force using the minikube CA

CERT_ROOT=${HOME}/.minikube/certs


# Create a server certiticate
openssl genrsa -out ServerKey.pem 2048
openssl req -new -key ServerKey.pem -out server.csr -subj "/CN=${service}.${namespace}.svc" -config server.conf
openssl x509 -req -in server.csr -CA ${CERT_ROOT}/ca.pem -CAkey ${CERT_ROOT}/ca-key.pem -CAcreateserial -out ServerCert.pem -days 100000 -extensions v3_req -extfile server.conf

# Create a client certiticate
openssl genrsa -out ClientKey.pem 2048
openssl req -new -key ClientKey.pem -out client.csr -subj "/CN=${service}.${namespace}.svc" -config client.conf
openssl x509 -req -in client.csr -CA ${CERT_ROOT}/ca.pem -CAkey ${CERT_ROOT}/ca-key.pem -CAcreateserial -out ClientCert.pem -days 100000 -extensions v3_req -extfile client.conf


# Clean up after we're done.
#rm *.pem
rm *.csr
rm *.srl
rm *.conf
