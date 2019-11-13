MAESH_NAMESPACE ?= maesh

.PHONY: stop-k3d
stop-k3d:
	-k3d delete

.PHONY: start-k3d
start-k3d: stop-k3d
	./script/start-k3d.sh

.PHONY: uninstall-maesh
uninstall-maesh:
	-helm del --purge maesh
	-kubectl delete -n $(MAESH_NAMESPACE) persistentvolumeclaim/metrics-storage

.PHONY: install-maesh
install-maesh: uninstall-maesh push-maesh
	helm install \
		${GOPATH}/src/github.com/containous/maesh/helm/chart/maesh \
		--name maesh \
		--namespace $(MAESH_NAMESPACE) \
		--set controller.image.tag=latest

.PHONY: install-maesh-smi
install-maesh-smi: uninstall-maesh push-maesh
	helm install \
		${GOPATH}/src/github.com/containous/maesh/helm/chart/maesh \
		--name maesh \
		--namespace $(MAESH_NAMESPACE) \
		--set controller.image.tag=latest \
		--set smi=true

.PHONY: watch-maesh
watch-maesh:
	watch kubectl -n $(MAESH_NAMESPACE) get svc,pods

.PHONY: push-maesh
push-maesh:
	k3d import-images containous/maesh:latest

.PHONY: deploy-apps
deploy-apps:
	kubectl apply -f ./k8s/apps

.PHONY: deploy-smi-example
deploy-smi-example:
	kubectl apply -f ./k8s/smi

.PHONY: stop-controller
stop-controller:
	kubectl -n $(MAESH_NAMESPACE) delete pod -l "app=maesh,component=controller"
