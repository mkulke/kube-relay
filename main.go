package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/urfave/cli/v2"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const POD_NAME = "kube-relay"
const POD_IMAGE = "alpine/socat:1.8.0.0"

func forward(namespace string, config *rest.Config, localPort uint) error {
	roundTripper, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, POD_NAME)
	hostIP := strings.TrimLeft(config.Host, "htps:/")
	serverURL := url.URL{Scheme: "https", Path: path, Host: hostIP}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, &serverURL)

	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)

	ports := fmt.Sprintf("%d:9000", localPort)
	forwarder, err := portforward.New(dialer, []string{ports}, stopChan, readyChan, out, errOut)
	if err != nil {
		panic(err)
	}

	go func() {
		for range readyChan { // Kubernetes will close this channel when it has something to tell us.
		}
		if len(errOut.String()) != 0 {
			panic(errOut.String())
		} else if len(out.String()) != 0 {
			print(out.String())
		}
	}()

	return forwarder.ForwardPorts()
}

func spawn(client kubernetes.Interface, namespace string, host string, port uint, image string) (string, error) {
	manifest := &apiv1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: POD_NAME,
		},
		Spec: apiv1.PodSpec{
			Containers: []apiv1.Container{
				{
					Name:  "socat",
					Image: image,
					Args: []string{
						"TCP-LISTEN:9000,fork",
						fmt.Sprintf("TCP:%s:%d", host, port),
					},
				},
			},
		},
	}
	result, err := client.CoreV1().Pods(namespace).Create(context.TODO(), manifest, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	name := result.GetObjectMeta().GetName()
	fmt.Printf("Created pod %q\n", name)
	return name, nil
}

func cleanup(client kubernetes.Interface, namespace string) {
	fmt.Printf("Delete pod %q\n", POD_NAME)
	client.CoreV1().Pods(namespace).Delete(context.TODO(), POD_NAME, metav1.DeleteOptions{})
}

func wait(client kubernetes.Interface, namespace string, name string) error {
	selector := fmt.Sprintf("metadata.name=%s", name)
	podWatch, err := client.CoreV1().Pods(namespace).Watch(context.TODO(), metav1.ListOptions{FieldSelector: selector})
	if err != nil {
		return err
	}

	for event := range podWatch.ResultChan() {
		p, ok := event.Object.(*v1.Pod)
		if !ok {
			return fmt.Errorf("unexpected type")
		}
		if p.Status.Phase == "Running" {
			fmt.Printf("Pod %q is running\n", p.Name)
			break
		}

	}
	return nil
}

func run(localPort uint, clusterHost string, clusterPort uint, podImage string) error {
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	)

	namespace, _, err := kubeconfig.Namespace()
	if err != nil {
		return err
	}

	// use the current context in kubeconfig
	config, err := kubeconfig.ClientConfig()
	if err != nil {
		return err
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	ctrlc := make(chan os.Signal, 1)
	signal.Notify(ctrlc, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctrlc
		println("received sigterm, triggering cleanup...")
		cleanup(clientset, namespace)
		os.Exit(1)
	}()

	name, err := spawn(clientset, namespace, clusterHost, clusterPort, podImage)
	defer cleanup(clientset, namespace)
	if err != nil {
		return err
	}
	err = wait(clientset, namespace, name)
	if err != nil {
		return err
	}
	err = forward(namespace, config, localPort)
	if err != nil {
		return err
	}
	return nil
}

func main() {
	var localPort uint
	var clusterPort uint
	var clusterHost string
	var podImage string

	app := &cli.App{
		Flags: []cli.Flag{
			&cli.UintFlag{
				Name:        "local-port",
				Aliases:     []string{"l"},
				Value:       1999,
				Usage:       "local tcp port",
				Destination: &localPort,
			},
			&cli.StringFlag{
				Name:        "cluster-host",
				Aliases:     []string{"ch"},
				Usage:       "cluster host",
				Destination: &clusterHost,
				Required:    true,
			},
			&cli.UintFlag{
				Name:        "cluster-port",
				Aliases:     []string{"cp"},
				Value:       80,
				Usage:       "cluster tcp port",
				Destination: &clusterPort,
			},
			&cli.StringFlag{
				Name:        "pod-image",
				Aliases:     []string{"p"},
				Value:       POD_IMAGE,
				Usage:       "socat oci image",
				Destination: &podImage,
			},
		},
		Name:  "kube-relay",
		Usage: "access tcp ports in a kubernetes cluster via a pod relay (locally)",
		Action: func(c *cli.Context) error {
			err := run(localPort, clusterHost, clusterPort, podImage)
			return err
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		panic(err)
	}
}
