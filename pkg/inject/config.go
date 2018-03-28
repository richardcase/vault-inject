package inject

import (
	"github.com/golang/glog"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	defaultAnnotation = "initializer.kubernetes.io/vault"
)

// Config represents the configuration of the initilaizer
type Config struct {
	RequireAnnotation      bool   `yaml:"requireAnnotation"`
	AnnotatioName          string `yaml:"annotationName"`
	IgnoreSystemNamespaces bool   `yaml:"ignoreSystemNamespaces"`
	VaultAuthMode          string `yaml:"vaultAuthMode"` //TODO: enum??
	VaultAddress           string `yaml:"vaultAddress"`
	VaultPathPattern       string `yaml:"vaultPathPattern"`
	SecretsPublisher       string `yaml:"secretsPublisher"`
	SecretsFilePathPattern string `yaml:"secretsFilePathPattern"`
	SecretsFileNamePattern string `yaml:"secretsFileNamePattern"`
	SecretNamePattern      string `yaml:"secretNamePattern"`
}

// GetInitializerConfig gets the initializer configuration from a Kubernetes configmap
func GetInitializerConfig(client clientset.Interface, namespace, configName string) (*Config, error) {
	glog.V(2).Infof("Reading config  %s in namespace %s", configName, namespace)
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(configName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	config, err := configmapToConfig(cm)
	if err != nil {
		return nil, err
	}

	if config.AnnotatioName == "" {
		config.AnnotatioName = defaultAnnotation
	}

	return config, nil
}

func configmapToConfig(configmap *corev1.ConfigMap) (*Config, error) {
	var c Config
	err := yaml.Unmarshal([]byte(configmap.Data["config"]), &c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
