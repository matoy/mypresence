package mailer

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

// selfSignedCert generates a self-signed TLS certificate for testing.
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// startFakeTLSSMTPServer starts a minimal TLS listener that immediately closes
// connections after accept — enough to let tls.Dial + smtp.NewClient succeed
// but then fail on the SMTP handshake.
func startFakeTLSSMTPServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	cert := selfSignedCert(t)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	l, err := tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			// Write a fake SMTP greeting then close
			conn.Write([]byte("220 fake.smtp.server ESMTP\r\n")) //nolint:errcheck
			conn.Close()
		}
	}()
	return l.Addr().String(), func() { l.Close() }
}

func TestSendTLS_NewClientFails(t *testing.T) {
	addr, stop := startFakeTLSSMTPServer(t)
	defer stop()

	host, _, _ := net.SplitHostPort(addr)

	// Use InsecureSkipVerify via a custom sendTLS call won't work since sendTLS
	// doesn't accept tls.Config. We call sendTLS directly — it will fail because
	// the server cert is self-signed and not trusted.
	// This still covers the tls.Dial path + the error return.
	err := sendTLS(addr, host, nil, "from@test.com", "to@test.com", []byte("test"))
	// Expected: either TLS handshake failure or SMTP client error
	if err == nil {
		t.Fatal("expected error from sendTLS with self-signed cert")
	}
}
