package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// The same coordinates your CRD declares: group, version, plural.
var dummysiteGVR = schema.GroupVersionResource{
	Group:    "samuel.jinadu",
	Version:  "v1",
	Resource: "dummysites",
}

type clients struct {
	dynamic dynamic.Interface
	k8s     kubernetes.Interface
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
		log.Printf("[ctrl] using kubeconfig (local dev mode)")
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("dynamic client: %v", err)
	}
	k8s, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("clientset: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	watchLoop(ctx, &clients{dynamic: dyn, k8s: k8s})
}

func watchLoop(ctx context.Context, c *clients) {
	backoff := time.Second

	for ctx.Err() == nil {
		err := listAndWatch(ctx, c)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			backoff = time.Second
			log.Printf("[ctrl] stream closed cleanly — reconnecting")
		} else {
			log.Printf("[ctrl] stream error: %v — reconnecting in %s", err, backoff)
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

func listAndWatch(ctx context.Context, c *clients) error {
	list, err := c.dynamic.Resource(dummysiteGVR).
		Namespace(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	log.Printf("[ctrl] list: %d existing DummySite(s), rv=%s", len(list.Items), list.GetResourceVersion())
	for i := range list.Items {
		onAdd(ctx, c, &list.Items[i])
	}

	watcher, err := c.dynamic.Resource(dummysiteGVR).
		Namespace(metav1.NamespaceAll).
		Watch(ctx, metav1.ListOptions{ResourceVersion: list.GetResourceVersion()})
	if err != nil {
		return err
	}
	defer watcher.Stop()
	log.Printf("[ctrl] watching from rv=%s", list.GetResourceVersion())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}
			u, ok := ev.Object.(*unstructured.Unstructured)
			if !ok {
				log.Printf("[ctrl] non-resource event type=%s (%T) — relisting", ev.Type, ev.Object)
				return nil
			}
			if ev.Type == "ADDED" {
				onAdd(ctx, c, u)
			}
			// MODIFIED and DELETED are deliberately ignored in Milestone 3.
		}
	}
}

func onAdd(ctx context.Context, c *clients, u *unstructured.Unstructured) {
	name := u.GetName()
	ns := u.GetNamespace()
	url, _, _ := unstructured.NestedString(u.Object, "spec", "website_url")
	if url == "" {
		log.Printf("[ctrl] %s/%s: missing website_url, skipping", ns, name)
		return
	}

	log.Printf("[ctrl] ADDED %s/%s url=%s", ns, name, url)

	body, err := fetchHTML(ctx, url)
	if err != nil {
		log.Printf("[ctrl] %s/%s: fetch failed: %v", ns, name, err)
		return
	}
	log.Printf("[ctrl] %s/%s: fetched %d bytes", ns, name, len(body))

	childName := "dummysite-" + name
	labels := map[string]string{"app": "dummysite", "dummysite": name}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: childName, Namespace: ns, Labels: labels},
		Data:       map[string]string{"index.html": body},
	}
	if _, err := c.k8s.CoreV1().ConfigMaps(ns).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		log.Printf("[ctrl] %s/%s: ConfigMap create: %v", ns, childName, err)
		// Fall through: if the ConfigMap already existed (restart), the Pod
		// can still mount it. Milestone 5 turns this into a proper check.
	} else {
		log.Printf("[ctrl] %s/%s: ConfigMap created", ns, childName)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: childName, Namespace: ns, Labels: labels},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "nginx",
				Image: "nginx:alpine",
				Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 80}},
				VolumeMounts: []corev1.VolumeMount{{
					Name:      "html",
					MountPath: "/usr/share/nginx/html",
					ReadOnly:  true,
				}},
			}},
			Volumes: []corev1.Volume{{
				Name: "html",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: childName},
					},
				},
			}},
		},
	}
	if _, err := c.k8s.CoreV1().Pods(ns).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		log.Printf("[ctrl] %s/%s: Pod create: %v", ns, childName, err)
	} else {
		log.Printf("[ctrl] %s/%s: Pod created", ns, childName)
	}
}

func fetchHTML(ctx context.Context, url string) (string, error) {
	var lastErr error
	const attempts = 4
	for i := 1; i <= attempts; i++ {
		body, err := fetchOnce(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		log.Printf("[ctrl] fetch %s attempt %d/%d failed: %v", url, i, attempts, err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(i) * time.Second):
		}
	}
	return "", lastErr
}

func fetchOnce(ctx context.Context, url string) (string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "dummysite-controller/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
