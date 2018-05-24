# Vault Inject [![Build Status](https://travis-ci.org/richardcase/vault-initializer.svg?branch=master)](https://travis-ci.org/richardcase/vault-initializer) [![Go Report Card](https://goreportcard.com/badge/github.com/richardcase/vault-initializer)](https://goreportcard.com/report/github.com/richardcase/vault-initializer) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0) #

Vault Inject is a [Mutating Admission Webhook](https://kubernetes.io/docs/admin/admission-controllers/#mutatingadmissionwebhook-beta-in-19) that injects secrets from Vault into a container when a deployment is created. It currently supports 2 ways to publish secrets into a container:
- Environment Variables
- A Kubernetes secret (automatically created) which is then mounted as a volume automatically into the POD.

> This isn't production ready yet. If you would like to help making in production production ready then see the [contributing guide](CONTRIBUTING.md)

## Getting Started

You need Kubernetes 1.9.0+. If you want to use minikube you can use the following to spin up a cluster:

```
minikube start \
	--extra-config=apiserver.Admission.PluginNames=NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota \
	--kubernetes-version=v1.9.0 --vm-driver=hyperkit
```

Edit and deploy the webhook config:
```
kubectl create -f artifacts/admission/configmap.yaml
```

Edit the vault token in the secrets file and deploy:
```
kubectl create -f artifacts/admission/secret.yaml
```
> Only the token authentication backend is currently supported for Vault

Create the CRD that holds the secrets mapping:
```
kubectl create -f artifacts/admission/crd.yaml
```

The Vault Admission Webhook needs to be registered on the cluster:

```
kubectl create -f artifacts/admission/registration.yaml
```

Now deploy the webhook controller:
```
kubectl create -f artifacts/admission/service.yaml
kubectl create -f artifacts/admission/deployment.yaml
```

Now when you create a deployment the Vault Admission webhook will be invoked. For example you can deploy a test app that dumps environment variables to the logs:
```
kubectl create -f artifacts/test/envprinter.yaml
```

## Vault Naming Conventions
When the webhook runs it will look for secrets using the following convention:

secret/{deploymentnamespace}/{containername}

For all the secrets in the following path it will inject an enviroment variable or an entry in a JSON config file into the container with the name of the secret and who's value is the value of the secret.

This is controlled using the following template:
```
vaultPathPattern: /v1/secret/{{.Namespace}}/{{.ContainerName}}
```

For example, if we create a secret using the following:
```
vault write secret/default/envprinter mysecret=Password123
```
An environment variable named *mysecret* will be injected into a container named envprinter when the deployment namespace is *defaul*.

## To debug locally

To debug locally you will need [telepresence](https://www.telepresence.io).


Deploy all artifacts except the service & deployment:
```
kubectl create -f artifacts/admission/configmap.yaml
kubectl create -f artifacts/admission/secret.yaml
kubectl create -f artifacts/admission/crd.yaml
kubectl create -f artifacts/admission/registration.yaml
```

Then start the service locally (or in debugger).

```
.\vault-admission
```

Now create a service & deployment using telepresense the points to the locally running version:
```
telepresence --context minikube --new-deployment vault-admission --expose 8000:443
```

## Contributing

If you would like to contribute see the [guide](CONTRIBUTING.md).
