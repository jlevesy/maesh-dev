package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	Object    v1.Object
	OldObject v1.Object
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

	factory := informers.NewSharedInformerFactory(clientSet, 10*time.Second)

	svcInformer := factory.Core().V1().Services().Informer()
	svcLister := factory.Core().V1().Services().Lister()

	svcInformer.AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			q.Add(event{Type: typeAdded, Object: obj.(v1.Object)})
		},
		UpdateFunc: func(oldObj, obj interface{}) {
			v1old := oldObj.(v1.Object)
			v1obj := obj.(v1.Object)

			if v1obj.GetResourceVersion() == v1old.GetResourceVersion() {
				// Periodic resync will send update events for all known Deployments.
				// Two different versions of the same Deployment will always have different RVs.
				return
			}

			q.Add(event{Type: typeUpdated, Object: v1obj, OldObject: v1old})
		},
		DeleteFunc: func(obj interface{}) {
			q.Add(event{Type: typeRemoved, Object: obj.(v1.Object)})
		},
	}, 30*time.Second)

	log.Println("Starting the informer")
	factory.Start(ctx.Done())

	log.Println("Waiting for the cache to be warmed up")
	if ok := cache.WaitForCacheSync(ctx.Done(), svcInformer.HasSynced); !ok {
		log.Fatal("unable to wait for cache sync")
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

	if evt.Type == typeUpdated && evt.Object.GetResourceVersion() == evt.OldObject.GetResourceVersion() {
		log.Println("skipping event due to refresh")
		return true
	}

	log.Printf(
		"Service %q, from namespace %q has been %s",
		evt.Object.GetName(),
		evt.Object.GetNamespace(),
		evt.Type,
	)

	svcs, err := svcLister.List(labels.Everything())
	if err != nil {
		log.Fatal("Unable to read services from cache: %v", err)
	}

	for _, svc := range svcs {
		log.Printf("svc %q from Namespace %q", svc.GetName(), svc.GetNamespace())
	}

	q.Forget(in)
	return true
}
