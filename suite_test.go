package clusterresourcequota

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ClusterResourceQuota E2E Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	// reuse downloaded envtest binaries across runs to avoid re-downloading
	binaryDir := filepath.Join(os.Getenv("HOME"), ".cache", "envtest")
	if err := os.MkdirAll(binaryDir, 0o755); err != nil {
		Expect(err).NotTo(HaveOccurred())
	}
	var err error

	ctx, cancel = context.WithCancel(context.Background())

	// Build validating webhook configuration similar to Helm chart template
	// read deploy/install.yaml to obtain webhook configuration (addr, cert dir)
	installPath := filepath.Join("deploy", "install.yaml")
	installContent, err := os.ReadFile(installPath)
	Expect(err).NotTo(HaveOccurred())

	objects, err := ParseYamlToObjects(string(installContent))
	Expect(err).NotTo(HaveOccurred())

	validationWebhookObjects := []*admissionv1.ValidatingWebhookConfiguration{}
	mutationWebhookObjects := []*admissionv1.MutatingWebhookConfiguration{}
	for _, obj := range objects {
		if whc, isWHC := obj.(*admissionv1.ValidatingWebhookConfiguration); isWHC {
			// remove webhooks's namespace selector to allow testing in any namespace
			for i := range whc.Webhooks {
				whc.Webhooks[i].NamespaceSelector = nil
			}
			validationWebhookObjects = append(validationWebhookObjects, whc)
		}
		if whc, isWHC := obj.(*admissionv1.MutatingWebhookConfiguration); isWHC {
			// remove webhooks's namespace selector to allow testing in any namespace
			for i := range whc.Webhooks {
				whc.Webhooks[i].NamespaceSelector = nil
			}
			mutationWebhookObjects = append(mutationWebhookObjects, whc)
		}
	}

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("deploy", "clusterresourcequota", "crds")},
		ErrorIfCRDPathMissing: true,
		DownloadBinaryAssets:  true,
		BinaryAssetsDirectory: binaryDir,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			ValidatingWebhooks: validationWebhookObjects,
			MutatingWebhooks:   mutationWebhookObjects,
		},
	}

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())

	schema := GetScheme()

	k8sClient, err = client.New(cfg, client.Options{Scheme: schema})
	Expect(err).NotTo(HaveOccurred())

	Expect(err).NotTo(HaveOccurred())

	options := NewDefaultOptions()
	options.Webhook.CertDir = testEnv.WebhookInstallOptions.LocalServingCertDir
	options.Webhook.Addr = fmt.Sprintf("%s:%d", testEnv.WebhookInstallOptions.LocalServingHost, testEnv.WebhookInstallOptions.LocalServingPort)

	// run server: start manager in goroutine and capture startup error via channel
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunWithConfig(ctx, cfg, options)
	}()

	// ensure manager did not exit immediately with an error
	Consistently(func() error {
		select {
		case e := <-errCh:
			return e
		default:
			return nil
		}
	}, 2*time.Second, 100*time.Millisecond).Should(BeNil())
})

var _ = AfterSuite(func() {
	if cancel != nil {
		cancel()
	}
	if testEnv != nil {
		Expect(testEnv.Stop()).NotTo(HaveOccurred())
	}
})

func ParseYamlToObjects(yamlContent string) ([]client.Object, error) {
	objects := []client.Object{}
	docs := strings.Split(yamlContent, "---")
	for _, doc := range docs {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		obj, err := DecodeYAMLToObject([]byte(doc))
		if err != nil {
			return nil, err
		}
		objects = append(objects, obj)
	}
	return objects, nil
}

func DecodeYAMLToObject(yamlData []byte) (client.Object, error) {
	decoder := serializer.NewCodecFactory(GetScheme()).UniversalDeserializer()
	obj, _, err := decoder.Decode(yamlData, nil, nil)
	if err != nil {
		return nil, err
	}
	clientObj, ok := obj.(client.Object)
	if !ok {
		return nil, fmt.Errorf("decoded object is not a client.Object")
	}
	return clientObj, nil
}
