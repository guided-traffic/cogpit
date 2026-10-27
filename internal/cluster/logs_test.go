package cluster

import (
	"context"
	"errors"
	"io"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestStreamLogsPassesOptions(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "log" {
			return false, nil, nil
		}
		return true, &runtime.Unknown{Raw: []byte("line 1\nline 2\n")}, nil
	})
	c := &Conn{kube: client}

	stream, err := c.StreamLogs(context.Background(), LogOptions{
		Namespace: "shop", Pod: "web-0", Container: "nginx",
		Follow: true, Previous: true, Timestamps: true, TailLines: 100,
	})
	if err != nil {
		t.Fatalf("StreamLogs: %v", err)
	}
	data, err := io.ReadAll(stream)
	_ = stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "line 1\nline 2\n" {
		t.Errorf("stream = %q", data)
	}

	var got *corev1.PodLogOptions
	for _, a := range client.Actions() {
		if a.GetSubresource() == "log" && a.GetNamespace() == "shop" {
			got, _ = a.(k8stesting.GenericAction).GetValue().(*corev1.PodLogOptions)
		}
	}
	if got == nil {
		t.Fatalf("no log request recorded: %v", client.Actions())
	}
	if got.Container != "nginx" || !got.Follow || !got.Previous || !got.Timestamps || got.TailLines == nil || *got.TailLines != 100 {
		t.Errorf("PodLogOptions = %+v", got)
	}
}

func TestStreamLogsDefaultsTail(t *testing.T) {
	client := fake.NewClientset()
	// The log reactor keeps the request off the object tracker's fallback, which formats the
	// action into an error it then drops, and in doing so calls String on the options' nil
	// SinceTime: a nil-pointer fault that fmt recovers. On Windows hosts with AMX that fault's
	// exception frame overruns the goroutine's stack and corrupts the heap (golang/go#81238).
	client.PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "log" {
			return false, nil, nil
		}
		return true, &runtime.Unknown{Raw: []byte("line 1\n")}, nil
	})
	c := &Conn{kube: client}
	stream, err := c.StreamLogs(context.Background(), LogOptions{Namespace: "shop", Pod: "web-0"})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	for _, a := range client.Actions() {
		if lo, ok := a.(k8stesting.GenericAction).GetValue().(*corev1.PodLogOptions); ok && lo.TailLines != nil {
			t.Errorf("TailLines = %d, want unset (all lines)", *lo.TailLines)
		}
	}
}

func TestStreamLogsErrors(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("pods \"web-0\" is forbidden")
	})
	c := &Conn{kube: client}
	if _, err := c.StreamLogs(context.Background(), LogOptions{Namespace: "shop", Pod: "web-0"}); err == nil {
		t.Error("StreamLogs succeeded although the request failed")
	}
	for name, opts := range map[string]LogOptions{
		"no namespace":  {Pod: "p"},
		"no pod":        {Namespace: "ns"},
		"negative tail": {Namespace: "ns", Pod: "p", TailLines: -1},
	} {
		if _, err := c.StreamLogs(context.Background(), opts); err == nil {
			t.Errorf("%s: StreamLogs succeeded", name)
		}
	}
	var nilConn *Conn
	if _, err := nilConn.StreamLogs(context.Background(), LogOptions{Namespace: "ns", Pod: "p"}); err == nil {
		t.Error("StreamLogs on a nil connection succeeded")
	}
}
