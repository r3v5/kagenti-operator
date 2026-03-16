/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package agentcard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testAgentCardJSON = `{"name":"test-agent","version":"1.0","url":"http://example.com"}`

func TestDefaultFetcher_SuccessfulA2ACardFetch(t *testing.T) {
	g := NewGomegaWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.Expect(r.URL.Path).To(Equal(A2AAgentCardPath))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(testAgentCardJSON))
	}))
	defer server.Close()

	result, err := NewFetcher().Fetch(context.Background(), A2AProtocol, server.URL, "", "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.Name).To(Equal("test-agent"))
	g.Expect(result.Version).To(Equal("1.0"))
}

func TestFetchA2ACard_LegacyFallback(t *testing.T) {
	g := NewGomegaWithT(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == A2ALegacyAgentCardPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(testAgentCardJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	card, err := NewFetcher().Fetch(context.Background(), A2AProtocol, srv.URL, "", "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(card.Name).To(Equal("test-agent"))
}

func TestFetchA2ACard_BothNotFound(t *testing.T) {
	g := NewGomegaWithT(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := NewFetcher().Fetch(context.Background(), A2AProtocol, srv.URL, "", "")
	g.Expect(err).To(HaveOccurred())
}

func TestDefaultFetcher_UnsupportedProtocol(t *testing.T) {
	g := NewGomegaWithT(t)
	_, err := NewFetcher().Fetch(context.Background(), "unsupported", "http://example.com", "", "")
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("unsupported protocol"))
}

func TestDefaultFetcher_HTTPError500(t *testing.T) {
	g := NewGomegaWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error body"))
	}))
	defer server.Close()

	_, err := NewFetcher().Fetch(context.Background(), A2AProtocol, server.URL, "", "")
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("unexpected status code"))
}

func TestDefaultFetcher_InvalidJSON(t *testing.T) {
	g := NewGomegaWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	_, err := NewFetcher().Fetch(context.Background(), A2AProtocol, server.URL, "", "")
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to parse agent card JSON"))
}

func TestGetServiceURL(t *testing.T) {
	g := NewGomegaWithT(t)
	g.Expect(GetServiceURL("my-agent", "default", 8080)).To(Equal("http://my-agent.default.svc.cluster.local:8080"))
}

func newFakeScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

func TestConfigMapFetcher_ConfigMapFound(t *testing.T) {
	g := NewGomegaWithT(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent" + SignedCardConfigMapSuffix,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			SignedCardConfigMapKey: testAgentCardJSON,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(newFakeScheme()).
		WithObjects(cm).
		Build()
	fetcher := NewConfigMapFetcher(fakeClient)

	card, err := fetcher.Fetch(context.Background(), A2AProtocol, "", "my-agent", "test-ns")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(card.Name).To(Equal("test-agent"))
	g.Expect(card.Version).To(Equal("1.0"))
}

func TestConfigMapFetcher_ConfigMapNotFound(t *testing.T) {
	g := NewGomegaWithT(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == A2AAgentCardPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(testAgentCardJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newFakeScheme()).
		Build()
	fetcher := NewConfigMapFetcher(fakeClient)

	card, err := fetcher.Fetch(context.Background(), A2AProtocol, srv.URL, "no-such-agent", "test-ns")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(card.Name).To(Equal("test-agent"))
}

func TestConfigMapFetcher_MissingKey(t *testing.T) {
	g := NewGomegaWithT(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "empty-agent" + SignedCardConfigMapSuffix,
			Namespace: "test-ns",
		},
		Data: map[string]string{},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == A2AAgentCardPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(testAgentCardJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newFakeScheme()).
		WithObjects(cm).
		Build()
	fetcher := NewConfigMapFetcher(fakeClient)

	card, err := fetcher.Fetch(context.Background(), A2AProtocol, srv.URL, "empty-agent", "test-ns")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(card.Name).To(Equal("test-agent"))
}

func TestConfigMapFetcher_InvalidJSON(t *testing.T) {
	g := NewGomegaWithT(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bad-agent" + SignedCardConfigMapSuffix,
			Namespace: "test-ns",
		},
		Data: map[string]string{
			SignedCardConfigMapKey: "not valid json{{{",
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == A2AAgentCardPath {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(testAgentCardJSON))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	fakeClient := fake.NewClientBuilder().
		WithScheme(newFakeScheme()).
		WithObjects(cm).
		Build()
	fetcher := NewConfigMapFetcher(fakeClient)

	card, err := fetcher.Fetch(context.Background(), A2AProtocol, srv.URL, "bad-agent", "test-ns")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(card.Name).To(Equal("test-agent"))
}
