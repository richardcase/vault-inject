package webhook

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/howeyc/fsnotify"
	"istio.io/istio/pkg/log"
	//vault "github.com/hashicorp/vault/api"
	clientset "github.com/richardcase/vault-admission/pkg/client/clientset/versioned"
	mapscheme "github.com/richardcase/vault-admission/pkg/client/clientset/versioned/scheme"
	informers "github.com/richardcase/vault-admission/pkg/client/informers/externalversions"
	listers "github.com/richardcase/vault-admission/pkg/client/listers/vaultinject/v1alpha1"
	"github.com/richardcase/vault-admission/pkg/inject"
	//"github.com/richardcase/vault-admission/pkg/inject/publisher"
	//"github.com/richardcase/vault-admission/pkg/inject/template"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	//"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	//"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	//"k8s.io/apimachinery/pkg/util/strategicpatch"
	//"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	//appslisters "k8s.io/client-go/listers/apps/v1beta2"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	//"k8s.io/kubernetes/pkg/apis/core/v1"
	adv1beta "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()

	// TODO(https://github.com/kubernetes/kubernetes/issues/57982)
	defaulter = runtime.ObjectDefaulter(runtimeScheme)
)

const (
	agentName          = "vault-admission"
	watchDebounceDelay = 100 * time.Millisecond
)

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionregistrationv1beta1.AddToScheme(runtimeScheme)

	// The `v1` package from k8s.io/kubernetes/pkgp/apis/core/v1 has
	// the object defaulting functions which are not included in
	// k8s.io/api/corev1. The default functions are required by
	// runtime.ObjectDefaulter to workaround lack of server-side
	// defaulting with webhooks (see
	// https://github.com/kubernetes/kubernetes/issues/57982).
	_ = v1.AddToScheme(runtimeScheme)
}

// Webhook is the implemntation for the vault mutating webhook
type Webhook struct {
	mu sync.RWMutex

	kubeclientset kubernetes.Interface
	mapclientset  clientset.Interface

	mapsLister listers.VaultMapLister
	mapsSynced cache.InformerSynced

	namespace       string
	secrets         map[string]string
	config          *inject.Config
	initializerName string

	recorder record.EventRecorder

	healthCheckInterval time.Duration
	healthCheckFile     string

	server     *http.Server
	configFile string
	watcher    *fsnotify.Watcher
	certFile   string
	keyFile    string
	cert       *tls.Certificate
}

// NewWebhook returns a new vault initializer
func NewWebhook(
	p Parameters,
	kubeclientset kubernetes.Interface,
	mapclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	mapsInformerFactory informers.SharedInformerFactory,
	namespace string,
	configmapName string,
	secretName string,
	webhookName string,
	stopCh <-chan struct{}) (*Webhook, error) {

	config, err := inject.GetInitializerConfig(kubeclientset, namespace, configmapName)
	if err != nil {
		glog.Fatal(err)
	}

	secrets, err := inject.GetInitializerSecret(kubeclientset, namespace, secretName)
	if err != nil {
		glog.Fatal(err)
	}

	//TODO: with the current version (v1.8) this doesn't pick up unitialized deployments
	// see: https://github.com/kubernetes/kubernetes/pull/51247
	//deploymentInformer := kubeInformerFactory.Apps().V1beta2().Deployments()
	mapsInformer := mapsInformerFactory.Vaultinject().V1alpha1().VaultMaps()

	// TODO: Remove this when the above is true
	//restClient := kubeclientset.AppsV1beta1().RESTClient()

	mapscheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: agentName})

	pair, err := tls.LoadX509KeyPair(p.CertFile, p.KeyFile)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// watch the parent directory of the target files so we can catch
	// symlink updates of k8s ConfigMaps volumes.
	for _, file := range []string{p.ConfigFile, p.MeshFile, p.CertFile, p.KeyFile} {
		watchDir, _ := filepath.Split(file)
		if err := watcher.Watch(watchDir); err != nil {
			return nil, fmt.Errorf("could not watch %v: %v", file, err)
		}
	}

	wh := &Webhook{
		kubeclientset:   kubeclientset,
		mapclientset:    mapclientset,
		namespace:       namespace,
		config:          config,
		secrets:         secrets,
		mapsLister:      mapsInformer.Lister(),
		mapsSynced:      mapsInformer.Informer().HasSynced,
		initializerName: webhookName,
		recorder:        recorder,

		server: &http.Server{
			Addr: fmt.Sprintf(":%v", p.Port),
		},
		configFile:          p.ConfigFile,
		watcher:             watcher,
		healthCheckInterval: p.HealthCheckInterval,
		healthCheckFile:     p.HealthCheckFile,
		certFile:            p.CertFile,
		keyFile:             p.KeyFile,
		cert:                &pair,
	}
	wh.server.TLSConfig = &tls.Config{GetCertificate: wh.getCert}
	h := http.NewServeMux()
	h.HandleFunc("/inject", wh.serveInject)
	wh.server.Handler = h

	glog.Info("Setting up event handlers")
	mapsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			glog.Infof("New map %v", obj)
		},
	})

	return wh, nil
}

func (wh *Webhook) Run(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()

	glog.Info("Starting vault admission")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	//if ok := cache.WaitForCacheSync(stopCh, i.deploymentsSynced, i.mapsSynced); !ok {
	if ok := cache.WaitForCacheSync(stopCh, wh.mapsSynced); !ok {
		return fmt.Errorf("Failed to wait for caches to sync")
	}

	go func() {
		if err := wh.server.ListenAndServeTLS("", ""); err != nil {
			glog.Errorf("ListenAndServeTLS for admission webhook returned error: %v", err)
		}
	}()
	defer wh.watcher.Close()
	defer wh.server.Close()

	var healthC <-chan time.Time
	if wh.healthCheckInterval != 0 && wh.healthCheckFile != "" {
		t := time.NewTicker(wh.healthCheckInterval)
		healthC = t.C
		defer t.Stop()
	}
	var timerC <-chan time.Time

	for {
		select {
		case <-timerC:
			/*config, err := inject.GetInitializerConfig(wh.kubeclientset, wh.namespace, wh.configmapName)
			if err != nil {
				glog.Errorf("update error: %v", err)
				break
			}
			pair, err := tls.LoadX509KeyPair(wh.certFile, wh.keyFile)
			if err != nil {
				glog.Errorf("reload cert error: %v", err)
				break
			}
			wh.mu.Lock()
			wh.cert = &pair
			wh.mu.Unlock()*/
		case event := <-wh.watcher.Event:
			if event.IsModify() || event.IsCreate() {
				timerC = time.After(watchDebounceDelay)
			}
		case err := <-wh.watcher.Error:
			glog.Errorf("Watcher error: %v", err)
		case <-healthC:
			content := []byte(`ok`)
			if err := ioutil.WriteFile(wh.healthCheckFile, content, 0644); err != nil {
				glog.Errorf("Health check update of %q failed: %v", wh.healthCheckFile, err)
			}
		case <-stopCh:
			break
		}
	}

	glog.Info("Shutting down admission webhook")
	return nil
}

func (wh *Webhook) getCert(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	wh.mu.Lock()
	defer wh.mu.Unlock()
	return wh.cert, nil
}

/*
func (wh *Webhook) initializeDeployment(deployment *v1beta1.Deployment) error {

	//TODO: Move this else where
	vaultConfig := vault.DefaultConfig()
	if i.config.VaultAddress != "" {
		vaultConfig.Address = i.config.VaultAddress
	}
	vaultClient, err := vault.NewClient(vaultConfig)
	if err != nil {
		glog.Fatal(err.Error())
	}

	if deployment.ObjectMeta.GetInitializers() != nil {
		pendingInitializers := deployment.ObjectMeta.GetInitializers().Pending

		if i.initializerName == pendingInitializers[0].Name {
			glog.Infof("Initializing deployment: %s", deployment.Name)

			o, err := runtime.NewScheme().DeepCopy(deployment)
			if err != nil {
				return err
			}
			initializedDeployment := o.(*v1beta1.Deployment)

			// Remove self from the list of pending Initializers while preserving ordering.
			if len(pendingInitializers) == 1 {
				initializedDeployment.ObjectMeta.Initializers = nil
			} else {
				initializedDeployment.ObjectMeta.Initializers.Pending = append(pendingInitializers[:0], pendingInitializers[1:]...)
			}

			if i.config.IgnoreSystemNamespaces && deployment.Namespace == "kube-system" {
				glog.Infof("Ignoring deployments in kube-system namespace")
				_, err = i.kubeclientset.AppsV1beta1().Deployments(deployment.Namespace).Update(initializedDeployment)
				return err
			}

			if i.config.RequireAnnotation {
				a := deployment.ObjectMeta.GetAnnotations()
				_, ok := a[i.config.AnnotatioName]
				if !ok {
					glog.V(2).Infof("Required '%s' annotation missing; skipping vault injection", i.config.AnnotatioName)
					_, err = i.kubeclientset.AppsV1beta1().Deployments(deployment.Namespace).Update(initializedDeployment)
					return err
				}
			}

			maps, err := i.mapsLister.VaultMaps(initializedDeployment.Namespace).List(labels.NewSelector())
			if err != nil {
				return err
			}
			if len(maps) == 0 {
				glog.V(2).Infof("No VaultMap for namespace %s; skipping vault injection", initializedDeployment.Namespace)
				_, err = i.kubeclientset.AppsV1beta1().Deployments(deployment.Namespace).Update(initializedDeployment)
				return err
			}
			vaultmap := maps[0]

			vaultPath, err := template.ResolveTemplate(initializedDeployment, vaultmap.Spec.VaultPathPattern)
			if err != nil {
				return err
			}
			glog.V(2).Infof("Querying vault with path: %s", vaultPath)
			request := vaultClient.NewRequest("GET", vaultPath)
			if i.config.VaultAuthMode == "Token" {
				request.ClientToken = i.secrets["vaultToken"]
			}
			resp, err := vaultClient.RawRequest(request)
			if err != nil {
				glog.Errorf("Error querying vault for secrets for %s: %v", vaultPath, err.Error())
				return err
			}

			defer func() {
				if resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
			}()

			if resp != nil && resp.StatusCode == 404 {
				glog.Infof("No secrets in vault for path %s", vaultPath)
				_, err = i.kubeclientset.AppsV1beta1().Deployments(deployment.Namespace).Update(initializedDeployment)
				return err
			}
			secret, err := vault.ParseSecret(resp.Body)
			if err != nil {
				return err
			}
			secrets := make(map[string]string)
			for key, value := range secret.Data {
				i.secrets[key] = value.(string)
			}
			publisher, err := publisher.CreatePublisher(vaultmap.Spec.SecretsPublisher)
			if err != nil {
				return err
			}
			err = publisher.PublishSecrets(vaultmap, i.kubeclientset.(*kubernetes.Clientset), initializedDeployment, secrets)
			if err != nil {
				return err
			}

			oldData, err := json.Marshal(deployment)
			if err != nil {
				return err
			}

			// Flag that this container has vault secrets
			if initializedDeployment.Spec.Template.Annotations == nil {
				annotations := make(map[string]string)
				initializedDeployment.Spec.Template.SetAnnotations(annotations)
			}
			initializedDeployment.Spec.Template.Annotations["vault-secrets-initialized"] = "true"

			newData, err := json.Marshal(initializedDeployment)
			if err != nil {
				return err
			}

			patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1beta1.Deployment{})
			if err != nil {
				return err
			}

			_, err = i.kubeclientset.AppsV1beta1().Deployments(deployment.Namespace).Patch(deployment.Name, types.StrategicMergePatchType, patchBytes)
			if err != nil {
				return err
			}
			glog.Infof("Patched Deployment: %s\n", deployment.Name)
		}
	}
	return nil
}
*/

// TODO(https://github.com/kubernetes/kubernetes/issues/57982)
// remove this workaround once server-side defaulting is fixed.
func applyDefaultsWorkaround(initContainers, containers []corev1.Container, volumes []corev1.Volume) {
	// runtime.ObjectDefaulter only accepts top-level resources. Construct
	// a dummy pod with fields we needed defaulted.
	defaulter.Default(&corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: initContainers,
			Containers:     containers,
			Volumes:        volumes,
		},
	})
}

// It would be great to use https://github.com/mattbaird/jsonpatch to
// generate RFC6902 JSON patches. Unfortunately, it doesn't produce
// correct patches for object removal. Fortunately, our patching needs
// are fairly simple so generating them manually isn't horrible (yet).
type rfc6902PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func toAdmissionResponse(err error) *adv1beta.AdmissionResponse {
	return &adv1beta.AdmissionResponse{Result: &metav1.Status{Message: err.Error()}}
}

func (wh *Webhook) inject(ar *adv1beta.AdmissionReview) *adv1beta.AdmissionResponse {
	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		log.Errorf("Could not unmarshal raw object: %v", err)
		return toAdmissionResponse(err)
	}

	log.Infof("AdmissionReview for Kind=%v Namespace=%v Name=%v (%v) UID=%v Rfc6902PatchOperation=%v UserInfo=%v",
		req.Kind, req.Namespace, req.Name, pod.Name, req.UID, req.Operation, req.UserInfo)
	log.Debugf("Object: %v", string(req.Object.Raw))
	log.Debugf("OldObject: %v", string(req.OldObject.Raw))

	/*if !injectRequired(ignoredNamespaces, wh.sidecarConfig.Policy, &pod.Spec, &pod.ObjectMeta) {
		log.Infof("Skipping %s/%s due to policy check", pod.Namespace, pod.Name)
		return &adv1beta.AdmissionResponse{
			Allowed: true,
		}
	}*/

	/*spec, status, err := injectionData(wh.sidecarConfig.Template, wh.sidecarTemplateVersion, &pod.Spec, &pod.ObjectMeta, wh.meshConfig.DefaultConfig, wh.meshConfig) // nolint: lll
	if err != nil {
		return toAdmissionResponse(err)
	}

	applyDefaultsWorkaround(spec.InitContainers, spec.Containers, spec.Volumes)
	annotations := map[string]string{istioSidecarAnnotationStatusKey: status}

	*/
	//patchBytes, err := createPatch(&pod, injectionStatus(&pod), annotations, spec)
	patchBytes, err := createPatch(&pod, nil)
	if err != nil {
		return toAdmissionResponse(err)
	}

	log.Infof("AdmissionResponse: patch=%v\n", string(patchBytes))

	reviewResponse := adv1beta.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *adv1beta.PatchType {
			pt := adv1beta.PatchTypeJSONPatch
			return &pt
		}(),
	}
	return &reviewResponse
}

func (wh *Webhook) serveInject(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		log.Errorf("no body found")
		http.Error(w, "no body found", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		log.Errorf("contentType=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var reviewResponse *adv1beta.AdmissionResponse
	ar := adv1beta.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		log.Errorf("Could not decode body: %v", err)
		reviewResponse = toAdmissionResponse(err)
	} else {
		reviewResponse = wh.inject(&ar)
	}

	response := adv1beta.AdmissionReview{}
	if reviewResponse != nil {
		response.Response = reviewResponse
		if ar.Request != nil {
			response.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(response)
	if err != nil {
		log.Errorf("Could not encode response: %v", err)
		http.Error(w, fmt.Sprintf("could encode response: %v", err), http.StatusInternalServerError)
	}
	if _, err := w.Write(resp); err != nil {
		log.Errorf("Could not write response: %v", err)
		http.Error(w, fmt.Sprintf("could write response: %v", err), http.StatusInternalServerError)
	}
}

// escape JSON Pointer value per https://tools.ietf.org/html/rfc6901
func escapeJSONPointerValue(in string) string {
	step := strings.Replace(in, "~", "~0", -1)
	return strings.Replace(step, "/", "~1", -1)
}

func updateAnnotation(target map[string]string, added map[string]string) (patch []rfc6902PatchOperation) {
	for key, value := range added {
		if target == nil {
			target = map[string]string{}
			patch = append(patch, rfc6902PatchOperation{
				Op:   "add",
				Path: "/metadata/annotations",
				Value: map[string]string{
					key: value,
				},
			})
		} else {
			op := "add"
			if target[key] != "" {
				op = "replace"
			}
			patch = append(patch, rfc6902PatchOperation{
				Op:    op,
				Path:  "/metadata/annotations/" + escapeJSONPointerValue(key),
				Value: value,
			})
		}
	}
	return patch
}

//func createPatch(pod *corev1.Pod, prevStatus *SidecarInjectionStatus, annotations map[string]string, sic *SidecarInjectionSpec) ([]byte, error) {
func createPatch(pod *corev1.Pod, annotations map[string]string) ([]byte, error) {
	var patch []rfc6902PatchOperation

	// Remove any containers previously injected by kube-inject using
	// container and volume name as unique key for removal.
	//patch = append(patch, removeContainers(pod.Spec.InitContainers, prevStatus.InitContainers, "/spec/initContainers")...)
	//patch = append(patch, removeContainers(pod.Spec.Containers, prevStatus.Containers, "/spec/containers")...)
	//patch = append(patch, removeVolumes(pod.Spec.Volumes, prevStatus.Volumes, "/spec/volumes")...)

	//patch = append(patch, addContainer(pod.Spec.InitContainers, sic.InitContainers, "/spec/initContainers")...)
	//patch = append(patch, addContainer(pod.Spec.Containers, sic.Containers, "/spec/containers")...)
	//patch = append(patch, addVolume(pod.Spec.Volumes, sic.Volumes, "/spec/volumes")...)

	patch = append(patch, updateAnnotation(pod.Annotations, annotations)...)

	return json.Marshal(patch)
}
