.PHONY: stop-k3d
stop-k3d:
	-k3d delete

.PHONY: start-k3d
start-k3d: stop-k3d
	./script/start-k3d.sh

.PHONY: uninstall-maesh
uninstall-maesh:
	-kubectl delete -n maesh persistentvolumeclaim/metrics-storage
	-helm del --purge maesh

.PHONY: install-maesh
install-maesh: uninstall-maesh
	helm install \
		${GOPATH}/src/github.com/containous/maesh/helm/chart/maesh \
		--name maesh \
		--namespace maesh \
		--set controller.image.tag=latest

.PHONY: watch-maesh
watch-maesh:
	watch kubectl -n maesh get all,pv,pvc

.PHONY: push-maesh
push-maesh:
	k3d import-images containous/maesh:latest

.PHONY: deploy-apps
deploy-apps:
	kubectl apply -f ./k8s/apps
