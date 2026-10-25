package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadConfigNamesTheMissingVariable(t *testing.T) {
	full := map[string]string{
		"NETMANAGER_URL":   "http://netmanager",
		"NETMANAGER_TOKEN": "t",
		"TLS_CERT_FILE":    "c.pem",
		"TLS_KEY_FILE":     "k.pem",
	}
	for name := range full {
		env := map[string]string{}
		for k, v := range full {
			if k != name {
				env[k] = v
			}
		}
		_, err := loadConfig(envOf(env))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: err = %v", name, err)
		}
	}
	cfg, err := loadConfig(envOf(full))
	if err != nil || cfg.netmanagerToken != "t" {
		t.Errorf("full env: %+v, %v", cfg, err)
	}
}

func writeCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	return certFile, keyFile
}

func TestFailedTLSHandshakeLogsNothing(t *testing.T) {
	var logged bytes.Buffer
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)

	certFile, keyFile := writeCert(t)
	s := newCustomerServer(http.NotFoundHandler())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go s.ServeTLS(ln, certFile, keyFile)
	defer s.Close()

	// A client that rejects the self-signed certificate, as customer apps do.
	_, err = (&http.Client{Timeout: 2 * time.Second}).Get("https://" + ln.Addr().String() + "/")
	if err == nil {
		t.Fatal("handshake should fail on the untrusted certificate")
	}
	time.Sleep(100 * time.Millisecond)
	if logged.Len() != 0 {
		t.Errorf("handshake failure was logged: %q", logged.String())
	}
}

func TestCustomerServerClosesEachConnection(t *testing.T) {
	ts := httptest.NewUnstartedServer(http.NotFoundHandler())
	ts.Config = newCustomerServer(http.NotFoundHandler())
	ts.Start()
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !resp.Close {
		t.Error("response did not announce Connection: close")
	}
}

func TestFetchTransportIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	if fetchTransport().Proxy != nil {
		t.Error("transport has a proxy function")
	}
}
