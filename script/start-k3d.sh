#/bin/sh

set -e

k3d create --workers=2 --wait=10
sleep 2
export KUBECONFIG="$(k3d get-kubeconfig --name='k3s-default')"
kubectl get nodes
kubectl apply -f ./k8s/rbac/tiller.yaml
kubectl apply -f https://raw.githubusercontent.com/rancher/local-path-provisioner/master/deploy/local-path-storage.yaml
helm init --upgrade  --service-account tiller --history-max 200
