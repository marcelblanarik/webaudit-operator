//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/marcelblanarik/webaudit-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "webaudit-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "webaudit-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "webaudit-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "webaudit-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", managerImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				By("getting the name of the controller-manager pod")
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				By("validating the pod's status")
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=webaudit-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			// +kubebuilder:scaffold:e2e-metrics-webhooks-readiness

			By("creating the curl-metrics pod to access the metrics endpoint")
			Expect(createCurlMetricsPod(token, "-v -k")).To(Succeed(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks
	})

	Context("WebAudit", Ordered, func() {
		const auditNS = "webaudit-e2e"
		var token string

		BeforeAll(func() {
			By("creating the audit namespace with the restricted Pod Security level")
			_, err := utils.Run(exec.Command("kubectl", "create", "ns", auditNS))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "label", "--overwrite", "ns", auditNS,
				"pod-security.kubernetes.io/enforce=restricted"))
			Expect(err).NotTo(HaveOccurred())

			By("deploying the nginx audit target")
			_, err = utils.Run(exec.Command("kubectl", "apply", "-n", auditNS, "-f", "test/e2e/testdata/nginx.yaml"))
			Expect(err).NotTo(HaveOccurred())
			_, err = utils.Run(exec.Command("kubectl", "rollout", "status", "deployment/nginx",
				"-n", auditNS, "--timeout=3m"))
			Expect(err).NotTo(HaveOccurred())

			token, err = serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", auditNS, "--ignore-not-found", "--wait=false"))
		})

		AfterEach(func() {
			if !CurrentSpecReport().Failed() {
				return
			}
			for _, args := range [][]string{
				{"get", "webaudits", "-n", auditNS, "-o", "yaml"},
				{"describe", "jobs", "-n", auditNS},
				{"describe", "pods", "-n", auditNS},
				{"logs", "-n", auditNS, "-l", "webaudit.simpleit.sk/name", "--tail=50"},
				{"get", "events", "-n", auditNS, "--sort-by=.lastTimestamp"},
				{"logs", "-n", namespace, "-l", "control-plane=controller-manager", "--tail=200"},
			} {
				out, _ := utils.Run(exec.Command("kubectl", args...))
				_, _ = fmt.Fprintf(GinkgoWriter, "kubectl %s:\n%s\n", strings.Join(args, " "), out)
			}
		})

		applyWebAudit := func(name, lcp string) {
			manifest := fmt.Sprintf(`apiVersion: webaudit.simpleit.sk/v1alpha1
kind: WebAudit
metadata:
  name: %s
  namespace: %s
spec:
  url: http://nginx.%s.svc
  schedule: "* * * * *"
  formFactor: desktop
  image: webaudit-lighthouse:dev
  thresholds:
    lcp: %s
    tbt: 5s
    cls: "1"
`, name, auditNS, auditNS, lcp)
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(manifest)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		}

		field := func(g Gomega, name, jsonpath string) string {
			out, err := utils.Run(exec.Command("kubectl", "get", "webaudit", name, "-n", auditNS,
				"-o", "jsonpath="+jsonpath))
			g.Expect(err).NotTo(HaveOccurred())
			return out
		}

		series := func(metric, name string) string {
			return fmt.Sprintf(`%s{name="%s",namespace="%s"}`, metric, name, auditNS)
		}

		It("records a passing audit", func() {
			applyWebAudit("e2e-pass", "10s")

			By("waiting for a recorded passing run")
			Eventually(func(g Gomega) {
				g.Expect(field(g, "e2e-pass", "{.status.lastRun.passed}")).To(Equal("true"))
				g.Expect(field(g, "e2e-pass", `{.status.conditions[?(@.type=="ThresholdsMet")].status}`)).
					To(Equal("True"))
				g.Expect(field(g, "e2e-pass", "{.status.lastRun.lcp}")).NotTo(BeEmpty())
			}, 8*time.Minute, 10*time.Second).Should(Succeed())

			By("checking the audit Job is owned by the WebAudit and its Pod ran in the restricted namespace")
			out, err := utils.Run(exec.Command("kubectl", "get", "jobs", "-n", auditNS,
				"-l", "webaudit.simpleit.sk/name=e2e-pass", "-o",
				"jsonpath={.items[0].metadata.ownerReferences[0].kind}/{.items[0].metadata.ownerReferences[0].name}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("WebAudit/e2e-pass"))
			out, err = utils.Run(exec.Command("kubectl", "get", "pods", "-n", auditNS,
				"-l", "webaudit.simpleit.sk/name=e2e-pass", "-o", "jsonpath={.items[*].status.phase}"))
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("Succeeded"))

			By("checking the operator metrics")
			Eventually(func(g Gomega) {
				m := scrapeMetrics(g, token)
				g.Expect(m).To(ContainSubstring(series("webaudit_lcp_seconds", "e2e-pass")))
				g.Expect(m).To(ContainSubstring(series("webaudit_threshold_met", "e2e-pass") + " 1"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("records a breach", func() {
			applyWebAudit("e2e-breach", "1ms")

			By("waiting for a recorded breaching run")
			Eventually(func(g Gomega) {
				g.Expect(field(g, "e2e-breach", "{.status.lastRun.passed}")).To(Equal("false"))
				g.Expect(field(g, "e2e-breach",
					`{.status.conditions[?(@.type=="ThresholdsMet")].status}/{.status.conditions[?(@.type=="ThresholdsMet")].reason}`)).
					To(Equal("False/ThresholdsBreached"))
			}, 8*time.Minute, 10*time.Second).Should(Succeed())

			By("checking the operator metrics")
			Eventually(func(g Gomega) {
				g.Expect(scrapeMetrics(g, token)).To(ContainSubstring(series("webaudit_threshold_met", "e2e-breach") + " 0"))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})

		It("forgets the metric series of a deleted WebAudit", func() {
			_, err := utils.Run(exec.Command("kubectl", "delete", "webaudit", "e2e-pass", "-n", auditNS))
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				m := scrapeMetrics(g, token)
				// positive control: e2e-breach (created by the previous ordered spec) is still exported
				g.Expect(m).To(ContainSubstring(series("webaudit_threshold_met", "e2e-breach") + " 0"))
				g.Expect(m).NotTo(ContainSubstring(`name="e2e-pass"`))
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
		})
	})
})

// createCurlMetricsPod starts the curl-metrics pod, which fetches the metrics endpoint once with token,
// retrying for up to a minute; curlFlags are passed to curl.
func createCurlMetricsPod(token, curlFlags string) error {
	cmd := exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
		"--namespace", namespace,
		"--image=curlimages/curl:latest",
		"--overrides",
		fmt.Sprintf(`{
			"spec": {
				"containers": [{
					"name": "curl",
					"image": "curlimages/curl:latest",
					"command": ["/bin/sh", "-c"],
					"args": [
						"for i in $(seq 1 30); do curl %s -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics && exit 0 || sleep 2; done; exit 1"
					],
					"securityContext": {
						"readOnlyRootFilesystem": true,
						"allowPrivilegeEscalation": false,
						"capabilities": {
							"drop": ["ALL"]
						},
						"runAsNonRoot": true,
						"runAsUser": 1000,
						"seccompProfile": {
							"type": "RuntimeDefault"
						}
					}
				}],
				"serviceAccountName": "%s"
			}
		}`, curlFlags, token, metricsServiceName, namespace, serviceAccountName))
	_, err := utils.Run(cmd)
	return err
}

// scrapeMetrics re-runs the curl-metrics pod and returns the metrics body. It fails on any unhealthy
// scrape (HTTP error via curl -f, or a body without the always-present reconcile counter), so absence
// checks cannot pass vacuously.
func scrapeMetrics(g Gomega, token string) string {
	_, err := utils.Run(exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace, "--ignore-not-found"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(createCurlMetricsPod(token, "-sS -f -k")).To(Succeed())
	g.Eventually(func() (string, error) {
		return utils.Run(exec.Command("kubectl", "get", "pod", "curl-metrics", "-n", namespace,
			"-o", "jsonpath={.status.phase}"))
	}, 2*time.Minute, time.Second).Should(BeElementOf("Succeeded", "Failed"))
	out, err := getMetricsOutput()
	g.Expect(err).NotTo(HaveOccurred())
	phase, err := utils.Run(exec.Command("kubectl", "get", "pod", "curl-metrics", "-n", namespace,
		"-o", "jsonpath={.status.phase}"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(phase).To(Equal("Succeeded"), "metrics scrape failed:\n%s", out)
	g.Expect(out).To(ContainSubstring("# TYPE controller_runtime_reconcile_total"), "no metrics body:\n%s", out)
	return out
}

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	By("creating temporary file to store the token request")
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		By("executing kubectl command to create the token")
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		By("parsing the JSON output to extract the token")
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
