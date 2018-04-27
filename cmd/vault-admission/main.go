package main

import (
	"flag"
	"io/ioutil"
	"time"

	"github.com/golang/glog"
	clientset "github.com/richardcase/vault-admission/pkg/client/clientset/versioned"
	informers "github.com/richardcase/vault-admission/pkg/client/informers/externalversions"
	"github.com/richardcase/vault-admission/pkg/signals"
	"github.com/richardcase/vault-admission/pkg/util"
	"github.com/richardcase/vault-admission/pkg/version"
	"github.com/richardcase/vault-admission/pkg/webhook"
	"istio.io/istio/pkg/log"
	corev1 "k8s.io/api/core/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultWebhookName         = "vault-admission.k8s.io"
	defaultConfigmap           = "vault-admission"
	defaultSecret              = "vault-admission"
	defaultTlsCertFile         = "/etc/vaultinject/certs/cert-chain.pem"
	defaultTlsKeyFile          = "/etc/vaultinject/certs/key.pem"
	defaultCaCertFile          = "/etc/vaultinject/certs/root-cert.pem"
	defaultPort                = 8000
	defaultHealthCheckInterval = 0
	defaultHealthCheckFile     = ""
	defaultWebhookConfigName   = "vault-admission.k8s.io"
)

var (
	webhookName         string
	namespace           string
	kubeconfig          string
	configmap           string
	secretName          string
	masterURL           string
	tlsCertFile         string
	tlsKeyFile          string
	caCertFile          string
	port                int
	healthCheckInterval time.Duration
	healthCheckFile     string
	webhookConfigName   string
)

func main() {
	flag.Parse()

	glog.Info("Starting the Vault Admission Controller...")
	version.OutputVersion()
	glog.V(2).Infof("Webhook name set to: %s", webhookName)
	glog.V(2).Infof("Using kubeconfig: %s", kubeconfig)

	stopCH := signals.SetupSignalHandler()

	clusterConfig, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		glog.Fatal(err)
	}

	mapClient, err := clientset.NewForConfig(clusterConfig)
	if err != nil {
		glog.Fatalf("Error build vault map clientset: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	mapInformerFactory := informers.NewSharedInformerFactory(mapClient, time.Second*30)

	parameters := webhook.Parameters{
		Port:                port,
		KeyFile:             tlsKeyFile,
		CertFile:            tlsCertFile,
		HealthCheckFile:     healthCheckFile,
		HealthCheckInterval: healthCheckInterval,
	}

	mutatingWebhook, err := webhook.NewWebhook(
		parameters,
		kubeClient,
		mapClient,
		kubeInformerFactory,
		mapInformerFactory,
		namespace,
		configmap,
		secretName,
		webhookName,
		stopCH)
	if err != nil {
		glog.Fatalf("Error creating webhook: %v", err)
	}

	if err := patchCert(); err != nil {
		glog.Fatalf("Failed to patch webhook config: %v", err)
	}

	go kubeInformerFactory.Start(stopCH)
	go mapInformerFactory.Start(stopCH)

	if err = mutatingWebhook.Run(stopCH); err != nil {
		glog.Fatalf("Error running webhook: %s", err.Error())
	}
}

func patchCert() error {
	const retryTimes = 6 // Try for one minute.
	client, err := createClientset(kubeconfig)
	if err != nil {
		return err
	}
	caCertPem, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		return err
	}
	i := 0
	for i < retryTimes {
		err = util.PatchMutatingWebhookConfig(client.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
			webhookConfigName, webhookName, caCertPem)
		if err == nil {
			return nil
		}
		log.Errorf("Register webhook failed: %s. Retrying...", err)
		time.Sleep(time.Second * 10)
	}
	return err
}

func createClientset(kubeconfigFile string) (*kubernetes.Clientset, error) {
	var err error
	var c *rest.Config
	if kubeconfigFile != "" {
		c, err = clientcmd.BuildConfigFromFlags("", kubeconfigFile)
	} else {
		c, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(c)
}

func init() {
	flag.StringVar(&webhookName, "webhookname", defaultWebhookName, "The webhook name")
	flag.StringVar(&namespace, "namespace", corev1.NamespaceDefault, "The configuration namespace")
	flag.StringVar(&configmap, "configmap", defaultConfigmap, "The webhook configuration configmap")
	flag.StringVar(&secretName, "secret", defaultSecret, "The webhook secret")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Absolute path to the kubeconfig file. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")

	flag.StringVar(&tlsCertFile, "tlsCertFile", defaultTlsCertFile, "File containing the x509 Certificate for HTTPS.")
	flag.StringVar(&tlsKeyFile, "tlsKeyFile", defaultTlsKeyFile, "File containing the x509 private key matching --tlsCertFile.")
	flag.StringVar(&caCertFile, "caCertFile", defaultCaCertFile, "File containing the x509 Certificate for HTTPS.")
	flag.IntVar(&port, "port", defaultPort, "Webhook port")
	flag.DurationVar(&healthCheckInterval, "healthCheckInterval", defaultHealthCheckInterval, "Configure how frequently the health check file specified by --healhCheckFile should be updated")
	flag.StringVar(&healthCheckFile, "healthCheckFile", defaultHealthCheckFile, "File that should be periodically updated if health checking is enabled")
	flag.StringVar(&webhookConfigName, "webhookConfigName", defaultWebhookConfigName, "Name of the mutatingwebhookconfiguration resource in Kubernete")
}
