#!/bin/bash

CLUSTER_NAME="k8s-lab"
KUBECONFIG_PATH="${HOME}/.kube/config"

echo "================================================"
echo "  Starting K3d cluster: ${CLUSTER_NAME}"
echo "================================================"

# Ensure .kube dir exists
mkdir -p ~/.kube

# Check if cluster already exists
if k3d cluster list 2>/dev/null | grep -q "^${CLUSTER_NAME}"; then
  SERVERS_RUNNING=$(k3d cluster list | grep "^${CLUSTER_NAME}" | awk '{print $2}')
  echo "▶ Cluster '${CLUSTER_NAME}' exists (servers: ${SERVERS_RUNNING})"

  if echo "${SERVERS_RUNNING}" | grep -q "^0/"; then
    echo "  Restarting stopped cluster..."
    k3d cluster start ${CLUSTER_NAME}
  else
    echo "  Cluster is already running ✅"
  fi
else
  echo "▶ Creating new cluster '${CLUSTER_NAME}'..."
  k3d cluster create ${CLUSTER_NAME} \
    --servers 1 \
    --agents 1 \
    --port "8080:80@loadbalancer" \
    --port "8443:443@loadbalancer" \
    --wait
  echo "  ✅ Cluster created"
fi

# ── Merge kubeconfig ──────────────────────────────────────────────────────────
echo ""
echo "▶ Merging kubeconfig..."
k3d kubeconfig merge ${CLUSTER_NAME} \
  --kubeconfig-merge-default \
  --kubeconfig-switch-context

# Ensure KUBECONFIG is set in .bashrc for all future terminals
if ! grep -q "export KUBECONFIG" ~/.bashrc 2>/dev/null; then
  echo "export KUBECONFIG=${KUBECONFIG_PATH}" >> ~/.bashrc
fi

# Export for the current shell process too
export KUBECONFIG="${KUBECONFIG_PATH}"

echo "  ✅ kubeconfig written to ${KUBECONFIG_PATH}"

# ── Wait for nodes to be Ready ────────────────────────────────────────────────
echo ""
echo "▶ Waiting for nodes to be Ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=60s

echo ""
echo "▶ Cluster info:"
echo ""
kubectl get nodes -o wide
echo ""
echo "================================================"
echo "  ✅ Cluster ready! Try:"
echo ""
echo "     kubectl get nodes"
echo "     kubectl get pods -A"
echo "     k9s"
echo "================================================"