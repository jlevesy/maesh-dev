package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

type eventType int

func (t eventType) String() string {
	switch t {
	case typeAdded:
		return "added"
	case typeUpdated:
		return "updated"
	case typeRemoved:
		return "removed"
	default:
		return "unknown"
	}
}

const (
	typeUnknown eventType = iota
	typeAdded
	typeUpdated
	typeRemoved
)

type event struct {
	Type      eventType
	Object    *corev1.Service
	OldObject *corev1.Service
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		log.Fatal("unable to get config", err)
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal("unable to create client set", err)
	}

	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "svc-changed")

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

		s := <-sigs
		log.Printf("Received signal %v, exiting", s)
		cancel()
		q.ShutDown()
	}()

	factory := informers.NewSharedInformerFactoryWithOptions(
		clientSet,
		10*time.Second,
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			// Here we can ignore namespaces and objects pased on labels.
			opts.FieldSelector = "metadata.namespace!=kube-system,metadata.name!=kubernetes"
		}),
	)

	svcInformer := factory.Core().V1().Services().Informer()
	svcLister := factory.Core().V1().Services().Lister()

	svcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			q.Add(event{Type: typeAdded, Object: obj.(*corev1.Service)})
		},
		UpdateFunc: func(oldObj, obj interface{}) {
			oldSvc := oldObj.(*corev1.Service)
			svc := obj.(*corev1.Service)

			if oldSvc.GetResourceVersion() == svc.GetResourceVersion() {
				// Periodic resync will send update events for all known Services.
				// Two different versions of the same Services will always have different RVs.
				return
			}

			q.Add(event{Type: typeUpdated, Object: svc, OldObject: oldSvc})
		},
		DeleteFunc: func(obj interface{}) {
			q.Add(event{Type: typeRemoved, Object: obj.(*corev1.Service)})
		},
	})

	log.Println("Starting the informer")
	factory.Start(ctx.Done())

	log.Println("Waiting for the cache to be ready")
	results := factory.WaitForCacheSync(ctx.Done())
	for _, ok := range results {
		if !ok {
			panic("unable to warmup cache")
		}
	}

	// Run the event loop.
	log.Println("Running the event loop")
	for handleEvent(q, svcLister) {
	}

	log.Println("Event loop has exited, bye bye.")
}

func handleEvent(q workqueue.RateLimitingInterface, svcLister listersv1.ServiceLister) bool {
	in, ok := q.Get()
	if ok {
		return false
	}
	defer q.Done(in)

	evt := in.(event)

	log.Printf(
		"Service %q, from namespace %q has been %s",
		evt.Object.GetName(),
		evt.Object.GetNamespace(),
		evt.Type,
	)

	svcs, err := svcLister.List(listNonMaeshSvcs())
	if err != nil {
		log.Fatal("Unable to read services from cache: %v", err)
	}

	for _, svc := range svcs {
		log.Printf("svc %q from Namespace %q can be exposed by maesh", svc.GetName(), svc.GetNamespace())
	}

	maeshSvcs, err := svcLister.List(listMaeshSvcs())
	if err != nil {
		log.Fatal("Unable to read services from cache: %v", err)
	}

	for _, svc := range maeshSvcs {
		log.Printf("svc %q from Namespace %q is a maesh service", svc.GetName(), svc.GetNamespace())
	}

	q.Forget(in)
	return true
}

func listMaeshSvcs() labels.Selector {
	sel := labels.Everything()

	r, err := labels.NewRequirement("app", selection.Equals, []string{"maesh"})
	if err != nil {
		panic(err)
	}
	sel = sel.Add(*r)

	r, err = labels.NewRequirement("component", selection.Equals, []string{"maesh-svc"})
	if err != nil {
		panic(err)
	}
	sel = sel.Add(*r)

	return sel
}

func listNonMaeshSvcs() labels.Selector {
	sel := labels.Everything()

	r, err := labels.NewRequirement("app", selection.NotEquals, []string{"maesh"})
	if err != nil {
		panic(err)
	}
	sel = sel.Add(*r)

	r, err = labels.NewRequirement("app", selection.NotEquals, []string{"jaeger"})
	if err != nil {
		panic(err)
	}
	sel = sel.Add(*r)

	return sel
}
