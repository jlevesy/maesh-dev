package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
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
	svcLister := NewServiceLister(factory.Core().V1().Services().Informer().GetIndexer())

	svcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			k, err := cache.MetaNamespaceKeyFunc(obj)
			if err != nil {
				panic(err)
			}
			q.Add(k)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldMeta, _ := meta.Accessor(oldObj)
			newMeta, _ := meta.Accessor(newObj)

			if oldMeta.GetResourceVersion() == newMeta.GetResourceVersion() {
				return
			}

			k, err := cache.MetaNamespaceKeyFunc(newObj)
			if err != nil {
				panic(err)
			}
			q.Add(k)
		},
		DeleteFunc: func(obj interface{}) {
			k, err := cache.MetaNamespaceKeyFunc(obj)
			if err != nil {
				panic(err)
			}
			q.Add(k)
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

func handleEvent(q workqueue.RateLimitingInterface, svcLister *ServiceLister) bool {
	evt, ok := q.Get()
	if ok {
		return false
	}
	defer q.Done(evt)

	k, ok := evt.(string)
	if !ok {
		q.Forget(evt)
	}

	name, namespace, err := cache.SplitMetaNamespaceKey(k)
	if err != nil {
		log.Println("ERROR when spliting key", err)
		q.Forget(evt)
	}

	log.Printf(
		"Service %q, from namespace %q is updated",
		name,
		namespace,
	)

	svcs, err := svcLister.List(listNonMaeshSvcs())
	if err != nil {
		log.Fatalf("Unable to read services from cache: %v", err)
	}

	for _, svc := range svcs {
		log.Printf("svc %q from Namespace %q can be exposed by maesh", svc.GetName(), svc.GetNamespace())
	}

	maeshSvcs, err := svcLister.List(listMaeshSvcs())
	if err != nil {
		log.Fatalf("Unable to read services from cache: %v", err)
	}

	for _, svc := range maeshSvcs {
		log.Printf("svc %q from Namespace %q is a maesh service", svc.GetName(), svc.GetNamespace())
	}

	nonKubesystemSvcs, err := svcLister.ListExcludingNamespaces(labels.Everything(), map[string]struct{}{"kube-system": struct{}{}})
	if err != nil {
		log.Fatalf("Unable to read services from cache: %v", err)
	}
	for _, svc := range nonKubesystemSvcs {
		log.Printf("svc %q from Namespace %q is a not in the kube-system namespace", svc.GetName(), svc.GetNamespace())
	}

	q.Forget(evt)
	return true
}

// ServiceLister lists services in an extended way.
type ServiceLister struct {
	listersv1.ServiceLister

	indexer cache.Indexer
}

// NewServiceLister builds an extended service lister.
func NewServiceLister(indexer cache.Indexer) *ServiceLister {
	return &ServiceLister{
		ServiceLister: listersv1.NewServiceLister(indexer),
		indexer:       indexer,
	}
}

// ListExcludingNamespaces lists all services not in the given namepsace set.
func (e ServiceLister) ListExcludingNamespaces(sel labels.Selector, namespaces map[string]struct{}) (svcs []*corev1.Service, err error) {
	// Slow and naïve implementation not relying on the indexer,
	// we might do better if we're able to list all present namespaces.
	err = cache.ListAll(e.indexer, sel, func(m interface{}) {
		svc := m.(*corev1.Service)

		// If it's excluded in excluded namespace set, then don't use it.
		if _, ok := namespaces[svc.GetNamespace()]; ok {
			return
		}

		svcs = append(svcs, svc)
	})
	return svcs, err

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
