package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// The same coordinates your CRD declares: group, version, plural.
var dummysiteGVR = schema.GroupVersionResource{
	Group:    "samuel.jinadu",
	Version:  "v1",
	Resource: "dummysites",
}

func main() {
	// 1. Identity. In-cluster first; kubeconfig only as a local-dev fallback.
	cfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.Getenv("HOME") + "/.kube/config"
		}
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		log.Printf("[watch] using kubeconfig (local dev mode)")
	}

	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("client: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	watchLoop(ctx, client)
}

func watchLoop(ctx context.Context, client dynamic.Interface) {
	backoff := time.Second

	for ctx.Err() == nil {
		err := listAndWatch(ctx, client)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			// clean close: the API server rotated the stream. Normal. Reconnect fast.
			backoff = time.Second
			log.Printf("[watch] stream closed cleanly — reconnecting")
		} else {
			log.Printf("[watch] stream error: %v — reconnecting in %s", err, backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func listAndWatch(ctx context.Context, client dynamic.Interface) error {
	// 2a. LIST — current state + the resourceVersion to resume from.
	list, err := client.Resource(dummysiteGVR).
		Namespace(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	log.Printf("[watch] list: %d existing DummySite(s), rv=%s", len(list.Items), list.GetResourceVersion())
	for i := range list.Items {
		handleEvent("ADDED(list)", &list.Items[i])
	}

	// 2b. WATCH — from exactly where the list ended. Nothing missed, nothing doubled.
	watcher, err := client.Resource(dummysiteGVR).
		Namespace(metav1.NamespaceAll).
		Watch(ctx, metav1.ListOptions{ResourceVersion: list.GetResourceVersion()})
	if err != nil {
		return err
	}
	defer watcher.Stop()
	log.Printf("[watch] watching from rv=%s", list.GetResourceVersion())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-watcher.ResultChan():
			if !ok {
				return nil // channel closed: server ended the stream
			}
			u, ok := ev.Object.(*unstructured.Unstructured)
			if !ok {
				// ERROR events arrive as *metav1.Status, e.g. 410 Gone.
				log.Printf("[watch] non-resource event type=%s (%T) — relisting", ev.Type, ev.Object)
				return nil
			}
			handleEvent(string(ev.Type), u)
		}
	}
}

func handleEvent(eventType string, u *unstructured.Unstructured) {
	url, _, _ := unstructured.NestedString(u.Object, "spec", "website_url")
	log.Printf("[watch] %-12s %s/%s url=%s", eventType, u.GetNamespace(), u.GetName(), url)
}
