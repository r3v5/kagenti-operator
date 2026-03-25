# E2E Tests

End-to-end tests for the kagenti-operator. The suite runs 8 specs:

- **Manager tests** (2 specs) — controller pod readiness and Prometheus metrics
- **AgentCard tests** (6 specs) — webhook validation, auto-discovery, duplicate prevention, audit mode, and SPIRE signature verification

## Prerequisites

- [Kind](https://kind.sigs.k8s.io/) — `go install sigs.k8s.io/kind@latest`
- [Helm](https://helm.sh/) — `brew install helm`
- [kubectl](https://kubernetes.io/docs/tasks/tools/) — `brew install kubectl`
- Container runtime: **Docker** or **Podman**

The test suite auto-detects Docker vs Podman. No env vars needed.

## Run

```bash
# Create a fresh Kind cluster
kind delete cluster 2>/dev/null; kind create cluster

# Run all 8 specs (~7 min)
make test-e2e
```

The suite automatically builds/loads images, installs Prometheus, CertManager, SPIRE, deploys the controller, runs tests, and tears everything down.

## Skip pre-installed components

If Prometheus, CertManager, or SPIRE are already on your cluster:

```bash
PROMETHEUS_INSTALL_SKIP=true \
CERT_MANAGER_INSTALL_SKIP=true \
SPIRE_INSTALL_SKIP=true \
make test-e2e
```

## Run specific scenarios

```bash
# Webhook + auto-discovery tests only (~4 min)
go test ./test/e2e/ -v -ginkgo.v -ginkgo.focus="should reject AgentCard|should not create|should auto-create|should reject duplicate"

# Signature verification tests only (~5 min)
go test ./test/e2e/ -v -ginkgo.v -ginkgo.focus="SignatureInvalidAudit|should verify signed"

# Manager tests only
go test ./test/e2e/ -v -ginkgo.v -ginkgo.focus="Manager"
```

## Cleanup

```bash
kind delete cluster
```

## Test scenarios

| Scenario | Context | What it tests |
|----------|---------|---------------|
| Reject missing targetRef | Without signature | Webhook rejects AgentCard with no `spec.targetRef` |
| No protocol label | Without signature | Workload with `kagenti.io/type=agent` but no `protocol.kagenti.io/*` label gets no auto-created card |
| Auto-discovery | Without signature | Properly labeled workload gets an auto-created AgentCard with correct targetRef, protocol, and Synced=True |
| Duplicate prevention | Without signature | Webhook rejects a second AgentCard targeting the same workload |
| Audit mode | With signature | Unsigned card syncs (Synced=True) but reports SignatureVerified=False with reason SignatureInvalidAudit |
| Signed agent | With signature | SPIRE-signed card gets SignatureVerified=True, correct SPIFFE ID, Synced=True, and Bound=True |

## Troubleshooting

**Stale cluster state** — if you see errors about namespaces being terminated or cert-manager TLS failures, delete and recreate the cluster:

```bash
kind delete cluster && kind create cluster
```

**Podman socket errors** — ensure your Podman machine is running:

```bash
podman machine start
```

**Override container tool** — if auto-detection picks the wrong runtime:

```bash
CONTAINER_TOOL=podman make test-e2e
```
