//go:build kind

package integration_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/guided-traffic/cogpit/internal/cluster"
	"github.com/guided-traffic/cogpit/internal/engine"
	"github.com/guided-traffic/cogpit/internal/model"
	"github.com/guided-traffic/cogpit/internal/testkit"
)

// certsNamespace is the namespace of TestKindSecretCertificates; it is deleted afterwards.
const certsNamespace = "cogpit-certs"

// TestKindSecretCertificates checks D-30 against a real API server: a kubernetes.io/tls Secret
// with a chain in tls.crt, its root in ca.crt and the leaf's key in tls.key, created through the
// API (which validates the type's keys), reaches an agent's object_get with every certificate
// decoded and its times in UTC, and nothing of the PEM text, the values or the key; the audit log
// carries none of it either.
func TestKindSecretCertificates(t *testing.T) {
	kubeconfig, contextName := kindEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := cluster.Connect(ctx, cluster.Options{KubeconfigPaths: []string{kubeconfig}, Context: contextName})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	kindNamespace(ctx, t, conn.Kube(), certsNamespace)

	now := time.Now()
	root := testkit.NewCert(t, testkit.CertSpec{CommonName: "Cogpit Kind Root CA", CA: true, NotBefore: now.AddDate(0, 0, -1), NotAfter: now.AddDate(1, 0, 0)})
	leaf := testkit.NewCert(t, testkit.CertSpec{
		CommonName: "web.cogpit-certs.svc", DNSNames: []string{"web.cogpit-certs.svc", "web"},
		NotBefore: now.AddDate(0, 0, -1), NotAfter: now.Add(20*24*time.Hour + time.Hour), Parent: root,
	})
	data := map[string][]byte{"tls.crt": testkit.Chain(leaf, root), "tls.key": leaf.KeyPEM(t), "ca.crt": root.PEM()}
	if _, err := conn.Kube().CoreV1().Secrets(certsNamespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "web-tls", Namespace: certsNamespace},
		Type:       corev1.SecretTypeTLS, Data: data,
	}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	k := newKindSession(ctx, t, kubeconfig, contextName, engine.NewMemoryHelmJournal())
	defer k.s.Close()
	ref := model.ObjectRef{Context: contextName, Kind: "secrets", Namespace: certsNamespace, Name: "web-tls"}
	var m *engine.DetailModel
	if !testkit.WaitFor(ctx, func() bool {
		m = k.detail(ctx, t, engine.OriginExternalAgent, ref)
		return len(m.Certificates) == 2
	}) {
		t.Fatalf("object_get never decoded the certificates: %+v", m.Certificates)
	}
	if m.Certificates[0].Key != "tls.crt" || m.Certificates[1].Key != "ca.crt" || len(m.Certificates[0].Certificates) != 2 {
		t.Fatalf("certificates %+v", m.Certificates)
	}
	c := m.Certificates[0].Certificates[0]
	if c.SubjectCN != "web.cogpit-certs.svc" || c.IssuerCN != "Cogpit Kind Root CA" || c.ExpiresInDays != 20 ||
		c.Expired || c.Status != model.StatusWarning || c.NotAfter.Location() != time.UTC || strings.Join(c.DNSNames, ",") != "web.cogpit-certs.svc,web" {
		t.Errorf("the leaf %+v", c)
	}
	if r := m.Certificates[0].Certificates[1]; !r.SelfSigned || !r.IsCA {
		t.Errorf("the root %+v", r)
	}
	// The owner's answers: tls.key compared with the leaf, the chain checked.
	if ck := m.Certificates[0]; ck.PairedKey != "tls.key" || ck.KeyMatches == nil || !ck.KeyMatches.Matches ||
		ck.ChainInOrder == nil || !*ck.ChainInOrder {
		t.Errorf("the checks %+v", ck)
	}

	b, _ := json.Marshal(m)
	forbidden := []string{"PRIVATE KEY", "BEGIN CERTIFICATE"}
	for _, v := range data {
		forbidden = append(forbidden, base64.StdEncoding.EncodeToString(v))
		for _, line := range strings.Split(string(v), "\n") {
			if len(line) >= 16 && !strings.HasPrefix(line, "-----") {
				forbidden = append(forbidden, line)
			}
		}
	}
	audit := k.audit.String()
	for _, f := range forbidden {
		if strings.Contains(string(b), f) || strings.Contains(audit, f) {
			t.Errorf("object_get or the audit log carries %q", f[:min(len(f), 40)])
		}
	}
}
