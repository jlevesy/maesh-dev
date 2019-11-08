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

.PHONY: watch-maesh
watch-maesh:
	watch kubectl -n $(MAESH_NAMESPACE) get all,pv,pvc

.PHONY: push-maesh
push-maesh:
	k3d import-images containous/maesh:latest

.PHONY: deploy-apps
deploy-apps: push-toolbox
	kubectl apply -f ./k8s/apps

.PHONY: stop-controller
stop-controller:
	kubectl -n $(MAESH_NAMESPACE) delete pod -l "app=maesh,component=controller"

.PHONY: build-toolbox
build-toolbox:
	docker build -t jlevesy/maesh-dev-debug:latest -f toolbox.Dockerfile .

.PHONY: push-toolbox
push-toolbox:
	k3d import-images jlevesy/maesh-dev-debug:latest
