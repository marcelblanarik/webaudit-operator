# webaudit-operator

Kubernetes operator that runs scheduled web performance audits.

## Decisions made
- Repo: github.com/marcelblanarik/webaudit-operator, MIT
- Framework: Go + Kubebuilder
- Domain: simpleit.sk, API group: webaudit.simpleit.sk, version v1alpha1
- Kind: WebAudit (resource: webaudits.webaudit.simpleit.sk)
- Init commands:
  kubebuilder init --domain simpleit.sk --repo github.com/marcelblanarik/webaudit-operator --license none
  kubebuilder create api --group webaudit --version v1alpha1 --kind WebAudit --resource --controller

## Design
- WebAudit spec: target URL, schedule, thresholds (e.g. LCP < 2.5s), optional custom image
- Operator runs Lighthouse in a Kubernetes Job (or CronJob) per audit
- Results go into the resource status and Prometheus metrics
- Own container image: pinned Chromium + `lighthouse` npm package on Node >= 22.19
  (Lighthouse 13 requirement), with --headless --no-sandbox --disable-dev-shm-usage
- Users can override the image in the spec

## Later ideas
- Playwright BrowserCheck kind, axe accessibility audits, Lighthouse CI server integration