// Copyright 2020 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/prometheus/common/expfmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport"
	"k8s.io/client-go/transport/spdy"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

const localPort = 4443

func TestMetricsServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "[MetricsServer]")
}

var _ = Describe("MetricsServer", func() {
	restConfig, err := getRestConfig()
	if err != nil {
		panic(err)
	}
	client, err := clientset.NewForConfig(restConfig)
	if err != nil {
		panic(err)
	}
	mclient, err := metricsclientset.NewForConfig(restConfig)
	if err != nil {
		panic(err)
	}
	It("exposes metrics from at least one pod in cluster", func() {
		podMetrics, err := mclient.MetricsV1beta1().PodMetricses(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "Failed to list pod metrics")
		Expect(podMetrics.Items).NotTo(BeEmpty(), "Need at least one pod to verify if MetricsServer works")
	})
	It("exposes metrics about all nodes in cluster", func() {
		nodeList, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			panic(err)
		}
		Expect(nodeList.Items).NotTo(BeEmpty(), "Need at least one node to verify if MetricsServer works")
		for _, node := range nodeList.Items {
			_, err := mclient.MetricsV1beta1().NodeMetricses().Get(context.Background(), node.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "Metrics for node %s are not available", node.Name)
		}
	})
	It("passes readyz probe", func() {
		msPod := mustGetMetricsServerPod(client)
		Expect(msPod.Spec.Containers).To(HaveLen(1), "Expected only one container in Metrics Server pod")
		resp := mustProxyContainerProbe(restConfig, msPod.Namespace, msPod.Name, msPod.Spec.Containers[0], msPod.Spec.Containers[0].ReadinessProbe)
		diff := cmp.Diff(string(resp), `[+]ping ok
[+]log ok
[+]poststarthook/generic-apiserver-start-informers ok
[+]informer-sync ok
[+]poststarthook/max-in-flight-filter ok
[+]metric-collection-successful ok
[+]metadata-informer-sync ok
[+]shutdown ok
readyz check passed
`)
		Expect(diff == "").To(BeTrue(), "Unexpected response %s", diff)
	})
	It("passes livez probe", func() {
		msPod := mustGetMetricsServerPod(client)
		Expect(msPod.Spec.Containers).To(HaveLen(1), "Expected only one container in Metrics Server pod")
		resp := mustProxyContainerProbe(restConfig, msPod.Namespace, msPod.Name, msPod.Spec.Containers[0], msPod.Spec.Containers[0].LivenessProbe)
		diff := cmp.Diff(string(resp), `[+]ping ok
[+]log ok
[+]poststarthook/generic-apiserver-start-informers ok
[+]poststarthook/max-in-flight-filter ok
[+]metric-collection-timely ok
[+]metadata-informer-sync ok
livez check passed
`)
		Expect(diff == "").To(BeTrue(), "Unexpected response %s", diff)
	})
	It("exposes prometheus metrics", func() {
		msPod := mustGetMetricsServerPod(client)
		resp, err := proxyRequestToPod(restConfig, msPod.Namespace, msPod.Name, "https", 443, "/metrics")
		Expect(err).NotTo(HaveOccurred(), "Failed to get Metrics Server /metrics endpoint")
		metrics, err := parseMetricNames(resp)
		Expect(err).NotTo(HaveOccurred(), "Failed to parse Metrics Server metrics")
		sort.Strings(metrics)

		Expect(metrics).To(Equal([]string{
			"apiserver_audit_event_total",
			"apiserver_audit_requests_rejected_total",
			"apiserver_client_certificate_expiration_seconds",
			"apiserver_current_inflight_requests",
			"apiserver_envelope_encryption_dek_cache_fill_percent",
			"apiserver_request_duration_seconds",
			"apiserver_request_filter_duration_seconds",
			"apiserver_request_total",
			"apiserver_response_sizes",
			"apiserver_storage_data_key_generation_duration_seconds",
			"apiserver_storage_data_key_generation_failures_total",
			"apiserver_storage_envelope_transformation_cache_misses_total",
			"apiserver_tls_handshake_errors_total",
			"authenticated_user_requests",
			"authentication_attempts",
			"authentication_duration_seconds",
			"go_gc_duration_seconds",
			"go_goroutines",
			"go_info",
			"go_memstats_alloc_bytes",
			"go_memstats_alloc_bytes_total",
			"go_memstats_buck_hash_sys_bytes",
			"go_memstats_frees_total",
			"go_memstats_gc_cpu_fraction",
			"go_memstats_gc_sys_bytes",
			"go_memstats_heap_alloc_bytes",
			"go_memstats_heap_idle_bytes",
			"go_memstats_heap_inuse_bytes",
			"go_memstats_heap_objects",
			"go_memstats_heap_released_bytes",
			"go_memstats_heap_sys_bytes",
			"go_memstats_last_gc_time_seconds",
			"go_memstats_lookups_total",
			"go_memstats_mallocs_total",
			"go_memstats_mcache_inuse_bytes",
			"go_memstats_mcache_sys_bytes",
			"go_memstats_mspan_inuse_bytes",
			"go_memstats_mspan_sys_bytes",
			"go_memstats_next_gc_bytes",
			"go_memstats_other_sys_bytes",
			"go_memstats_stack_inuse_bytes",
			"go_memstats_stack_sys_bytes",
			"go_memstats_sys_bytes",
			"go_threads",
			"metrics_server_api_metric_freshness_seconds",
			"metrics_server_kubelet_last_request_time_seconds",
			"metrics_server_kubelet_request_duration_seconds",
			"metrics_server_kubelet_request_total",
			"metrics_server_manager_tick_duration_seconds",
			"metrics_server_storage_points",
			"process_cpu_seconds_total",
			"process_max_fds",
			"process_open_fds",
			"process_resident_memory_bytes",
			"process_start_time_seconds",
			"process_virtual_memory_bytes",
			"process_virtual_memory_max_bytes",
			"rest_client_exec_plugin_certificate_rotation_age",
			"rest_client_exec_plugin_ttl_seconds",
			"rest_client_request_duration_seconds",
			"rest_client_requests_total",
			"workqueue_adds_total",
			"workqueue_depth",
			"workqueue_longest_running_processor_seconds",
			"workqueue_queue_duration_seconds",
			"workqueue_retries_total",
			"workqueue_unfinished_work_seconds",
			"workqueue_work_duration_seconds",
		}), "Unexpected metrics")
	})
})

func getRestConfig() (*rest.Config, error) {
	config, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return nil, err
	}
	return clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
}

func mustGetMetricsServerPod(client clientset.Interface) corev1.Pod {
	podList, err := client.CoreV1().Pods(metav1.NamespaceSystem).List(context.Background(), metav1.ListOptions{LabelSelector: "k8s-app=metrics-server"})
	Expect(err).NotTo(HaveOccurred(), "Failed to find Metrics Server pod")
	Expect(podList.Items).NotTo(BeEmpty(), "Metrics Server pod was not found")
	Expect(podList.Items).To(HaveLen(1), "Expect to only have one Metrics Server pod")
	return podList.Items[0]
}

func parseMetricNames(data []byte) ([]string, error) {
	var parser expfmt.TextParser
	mfs, err := parser.TextToMetricFamilies(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	Expect(err).NotTo(HaveOccurred(), "Failed to parse mfs")
	var ms []string
	for key := range mfs {
		ms = append(ms, key)
	}
	return ms, nil
}

func mustProxyContainerProbe(config *rest.Config, namespace, name string, container corev1.Container, probe *corev1.Probe) []byte {
	Expect(probe).NotTo(BeNil(), "Probe should not be empty")
	Expect(probe.HTTPGet).NotTo(BeNil(), "Probe should be http")
	port := getContainerPort(container, probe.HTTPGet.Port)
	Expect(port).NotTo(Equal(0), "Probe port should not be empty")
	resp, err := proxyRequestToPod(config, namespace, name, string(probe.HTTPGet.Scheme), port, mustGetVerboseProbePath(probe.HTTPGet))
	Expect(err).NotTo(HaveOccurred(), "Failed to get Metrics Server probe endpoint")
	return resp
}

func mustGetVerboseProbePath(probe *corev1.HTTPGetAction) string {
	path, err := url.Parse(probe.Path)
	Expect(err).NotTo(HaveOccurred(), "Failed to parse probe path")
	newPath := url.URL{
		Path:     path.Path,
		RawQuery: "verbose&" + path.RawQuery,
	}
	return newPath.String()
}

func getContainerPort(c corev1.Container, port intstr.IntOrString) int {
	switch port.Type {
	case intstr.Int:
		return int(port.IntVal)
	case intstr.String:
		for _, cp := range c.Ports {
			if cp.Name == port.StrVal {
				return int(cp.ContainerPort)
			}
		}
	}
	return 0
}

func proxyRequestToPod(config *rest.Config, namespace, podname, scheme string, port int, path string) ([]byte, error) {
	cancel, err := setupForwarding(config, namespace, podname, port)
	defer cancel()
	if err != nil {
		return nil, err
	}
	var query string
	if strings.Contains(path, "?") {
		elm := strings.SplitN(path, "?", 2)
		path = elm[0]
		query = elm[1]
	}
	reqUrl := url.URL{Scheme: scheme, Path: path, RawQuery: query, Host: fmt.Sprintf("127.0.0.1:%d", localPort)}
	resp, err := sendRequest(config, reqUrl.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func setupForwarding(config *rest.Config, namespace, podname string, port int) (cancel func(), err error) {
	hostIP := strings.TrimLeft(config.Host, "https://")
	mappings := []string{fmt.Sprintf("%d:%d", localPort, port)}

	trans, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return noop, err
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: trans}, http.MethodPost, &url.URL{Scheme: "https", Path: fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podname), Host: hostIP})

	var berr, bout bytes.Buffer
	buffErr := bufio.NewWriter(&berr)
	buffOut := bufio.NewWriter(&bout)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	fw, err := portforward.New(dialer, mappings, stopCh, readyCh, buffOut, buffErr)
	if err != nil {
		return noop, err
	}
	go func() {
		fmt.Print(fw.ForwardPorts())
	}()
	<-readyCh
	return func() {
		stopCh <- struct{}{}
	}, nil
}

func sendRequest(config *rest.Config, url string) (*http.Response, error) {
	tsConfig, err := config.TransportConfig()
	if err != nil {
		return nil, err
	}
	tsConfig.TLS.Insecure = true
	tsConfig.TLS.CAData = []byte{}
	tsConfig.TLS.CAFile = ""

	ts, err := transport.New(tsConfig)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: ts}
	return client.Get(url)
}

func noop() {}
