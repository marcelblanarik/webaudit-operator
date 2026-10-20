# webaudit-operator

A Kubernetes operator that runs scheduled [Lighthouse](https://github.com/GoogleChrome/lighthouse) audits against a URL, compares the lab Core Web Vitals (LCP, CLS, TBT) to thresholds you set, and reports the result in the resource status, Events and Prometheus metrics.

## Example

```yaml
apiVersion: webaudit.simpleit.sk/v1alpha1
kind: WebAudit
metadata:
  name: webaudit-sample
spec:
  url: https://example.com
  schedule: "0 * * * *"      # 5-field cron, UTC
  formFactor: mobile         # mobile (default) | desktop
  thresholds:                # each optional; omitted = not enforced
    lcp: 2500ms
    cls: "0.1"
    tbt: 200ms
  # image: my/lighthouse:tag # optional override of the audit image
```

## How it works

- For each fire time of `spec.schedule` the operator creates one Kubernetes Job (no CronJob), named `<name>-<unix fire time>`. The Job runs the audit image and is owned by the WebAudit.
- The Job writes `{lcpMs, tbtMs, cls}` to its termination message. The operator reads it from the Pod.
- The operator, not the Job, evaluates the thresholds. The Job only fails if the audit itself failed.
- No new Job starts while one is active. Jobs run with `backoffLimit: 0` and `activeDeadlineSeconds: 300`.
- The result goes to `status.lastRun`, the conditions, an Event and the metrics below. The 3 newest finished Jobs are kept.

## Install

```sh
helm install webaudit-operator oci://ghcr.io/marcelblanarik/charts/webaudit-operator \
  --version <version> --namespace webaudit-operator-system --create-namespace
kubectl apply -f config/samples/webaudit_v1alpha1_webaudit.yaml
```

The images and chart are published by the release workflow when a `v*` tag is pushed, so none of these exist until a release has been cut. The released chart pins the audit image of the same version. Kubernetes 1.31+ is required.

| Chart value | Meaning |
|---|---|
| `defaultAuditImage` | Audit image for WebAudits without `spec.image` (passed as `--default-audit-image`). |
| `prometheus.enabled` | Render a `ServiceMonitor` (default `false`; needs the prometheus-operator CRDs). |
| `manager.image.repository`, `manager.image.tag` | Operator image. The tag defaults to the chart `appVersion`. |

Caveat: setting `manager.args` replaces the whole list. The defaults are `--leader-elect` and `--default-audit-image={{ .Values.defaultAuditImage }}`, so repeat both if you override it.

## Status and metrics

`status` holds `observedGeneration`, `nextRunTime`, `lastRun` (`jobName`, `startTime`, `completionTime`, `lcp`, `cls`, `tbt`, `passed`, `message`) and two conditions:

| Condition | Meaning |
|---|---|
| `Ready` | `True` (reason `Scheduled`), or `False` (reason `InvalidSchedule`). |
| `ThresholdsMet` | `True` (`ThresholdsMet`), `False` (`ThresholdsBreached`), or `Unknown` (`AuditFailed`, the audit produced no result). |

Metrics are served on the manager's metrics endpoint. All carry the labels `namespace` and `name`; the series of a deleted WebAudit are removed.

| Metric | Notes |
|---|---|
| `webaudit_lcp_seconds`, `webaudit_cls`, `webaudit_tbt_seconds` | Values of the last audit that produced a result. |
| `webaudit_threshold_met` | `1` met, `0` breached. Not updated by failed audits. |
| `webaudit_last_run_timestamp_seconds` | Unix time of the last audit that produced a result. |
| `webaudit_runs_total` | Counter with an extra `result` label: `success`, `failed` or `breach`. |

## Behaviour notes

- Schedules are 5-field cron in UTC only. `TZ=` and `CRON_TZ=` prefixes are rejected.
- After downtime at most one run happens, never a catch-up burst. Missed fires older than 60 days are dropped.
- The WebAudit name may be at most 52 characters.
- A single Lighthouse run in a container is noisy. Give thresholds headroom.

## Operating notes and limits

- **No resource requests or limits.** Audit Pods run as BestEffort, so results are noisier under CPU contention. A ResourceQuota that requires requests rejects the Pod, and a LimitRange default limit can OOM-kill Chromium; either way the run ends as `AuditFailed` after the 300s deadline. Create WebAudits in a namespace without such constraints, or with a LimitRange that suits Chromium.
- **SSRF is out of scope.** Anyone who can create a WebAudit can make the cluster fetch any URL reachable from that namespace, including in-cluster Services. No NetworkPolicy ships. Restrict WebAudit creation with RBAC and consider an egress NetworkPolicy for the audit Pods (label `webaudit.simpleit.sk/name`).
- **Metrics are in memory.** After an operator restart or leader change the `webaudit_*` series are absent until each WebAudit's next run, so give `absent()` alerts a grace period. `status.lastRun` still holds the last result.
- **Failure diagnosis.** An audit that cannot start (for example `ImagePullBackOff` or unschedulable) only shows up as `AuditFailed` after the 300s deadline. Inspect `kubectl describe job/pod` and the logs of the retained Jobs (up to 3).
- **Helm `rbac.namespaced=true` is not supported** (the manager caches cluster-wide). Leave it `false`.
- **Maintainers:** GHCR packages are private by default. After the first release, make the packages public (the `org.opencontainers.image.source` label links them to this repository) before the anonymous `helm install` and image pulls above work.

## Development

Needs Go (see `go.mod`), Docker, kind, kubectl, helm and Node 22 for the audit image tests.

```sh
make test        # unit and envtest tests
make lint
node --test 'images/lighthouse/*.test.mjs'   # audit image unit tests
docker build -t webaudit-lighthouse:dev images/lighthouse
helm lint dist/chart
make docker-buildx audit-image-buildx        # multi-arch build and push, set IMG and AUDIT_IMG first
```

`make test-e2e` creates a kind cluster, builds both images and runs real Lighthouse audits (about 4 minutes). **Warning:** kind switches the current kubectl context and drops it again on cleanup. Run it with an isolated kubeconfig so your own is untouched:

```sh
KUBECONFIG=$(mktemp) make test-e2e
```

It also rewrites `config/manager/kustomization.yaml` (`make deploy` does); restore it with `git checkout`.

Maintenance notes:

- The audit image installs an exact Chromium build (`ARG CHROMIUM_VERSION` in `images/lighthouse/Dockerfile`). Debian drops old builds from its archive, so the build fails once the pin ages out; bump it periodically. The `Audit Image` workflow builds weekly to catch this.
- `dist/chart` was generated with `kubebuilder edit --plugins=helm/v2-alpha`. Running it again with `--force` can overwrite hand-edited chart values; re-apply `defaultAuditImage` and `prometheus.enabled: false` afterwards.

## License

MIT, see [LICENSE](LICENSE).
