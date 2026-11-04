#!/bin/bash

echo "================================================"
echo "  Installing K3d Kubernetes Learning Environment"
echo "================================================"

# ── k3d ──────────────────────────────────────────────────────────────────────
if command -v k3d &>/dev/null; then
  echo "▶ k3d already installed ($(k3d version | head -1 | awk '{print $3}')) — skipping"
else
  echo ""
  echo "▶ Installing k3d..."
  curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
  echo "  ✅ k3d $(k3d version | head -1 | awk '{print $3}') installed"
fi

# ── kubectx + kubens ─────────────────────────────────────────────────────────
if command -v kubectx &>/dev/null; then
  echo "▶ kubectx/kubens already installed — skipping"
else
  echo ""
  echo "▶ Installing kubectx and kubens..."
  sudo git clone https://github.com/ahmetb/kubectx /opt/kubectx --quiet
  sudo ln -sf /opt/kubectx/kubectx /usr/local/bin/kubectx
  sudo ln -sf /opt/kubectx/kubens /usr/local/bin/kubens
  echo "  ✅ kubectx and kubens installed"
fi

# ── k9s ──────────────────────────────────────────────────────────────────────
if command -v k9s &>/dev/null; then
  echo "▶ k9s already installed — skipping"
else
  echo ""
  echo "▶ Installing k9s..."
  K9S_VERSION=$(curl -s https://api.github.com/repos/derailed/k9s/releases/latest | grep tag_name | cut -d'"' -f4)
  if [ -z "$K9S_VERSION" ]; then
    echo "  ⚠️  Could not fetch k9s version — skipping"
  else
    curl -sL "https://github.com/derailed/k9s/releases/download/${K9S_VERSION}/k9s_Linux_amd64.tar.gz" | \
      sudo tar xz -C /usr/local/bin k9s
    echo "  ✅ k9s ${K9S_VERSION} installed"
  fi
fi

# ── Stern ────────────────────────────────────────────────────────────────────
if command -v stern &>/dev/null; then
  echo "▶ stern already installed — skipping"
else
  echo ""
  echo "▶ Installing stern..."
  STERN_VERSION=$(curl -s https://api.github.com/repos/stern/stern/releases/latest | grep tag_name | cut -d'"' -f4)
  if [ -z "$STERN_VERSION" ]; then
    echo "  ⚠️  Could not fetch stern version — skipping"
  else
    curl -sL "https://github.com/stern/stern/releases/download/${STERN_VERSION}/stern_linux_amd64.tar.gz" | \
      sudo tar xz -C /usr/local/bin stern
    echo "  ✅ stern ${STERN_VERSION} installed"
  fi
fi

# ── kubectl aliases (idempotent) ──────────────────────────────────────────────
if grep -q "# ── Kubernetes shortcuts" ~/.bashrc 2>/dev/null; then
  echo "▶ kubectl aliases already configured — skipping"
else
  echo ""
  echo "▶ Setting up kubectl aliases and shell completion..."
  cat >> ~/.bashrc << 'BASHRC'

# ── Kubernetes shortcuts ──────────────────────────────────────────────────────
alias k="kubectl"
alias kgp="kubectl get pods"
alias kgs="kubectl get svc"
alias kgd="kubectl get deployments"
alias kgn="kubectl get nodes"
alias kga="kubectl get all"
alias kdp="kubectl describe pod"
alias kds="kubectl describe svc"
alias kl="kubectl logs"
alias kaf="kubectl apply -f"
alias kdf="kubectl delete -f"
alias kns="kubens"
alias kctx="kubectx"

# kubectl autocomplete
source <(kubectl completion bash)
complete -F __start_kubectl k
BASHRC
  echo "  ✅ Aliases and completion configured"
fi

# ── ensure .kube dir exists ───────────────────────────────────────────────────
mkdir -p ~/.kube

echo ""
echo "================================================"
echo "  ✅ Installation complete!"
echo "================================================"