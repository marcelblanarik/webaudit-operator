package v1alpha1_test

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	webauditv1alpha1 "github.com/marcelblanarik/webaudit-operator/api/v1alpha1"
)

var k8sClient client.Client

// TestMain starts an envtest API server with the generated CRDs so the
// validation markers are exercised against a real API server.
func TestMain(m *testing.M) {
	if err := webauditv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	// Allows running from an IDE without KUBEBUILDER_ASSETS; `make test` sets it.
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		testEnv.BinaryAssetsDirectory = firstEnvTestBinaryDir()
	}

	cfg, err := testEnv.Start()
	if err != nil {
		panic(err)
	}
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		panic(err)
	}

	code := m.Run()
	_ = testEnv.Stop()
	os.Exit(code)
}

func firstEnvTestBinaryDir() string {
	base := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			return filepath.Join(base, e.Name())
		}
	}
	return ""
}
