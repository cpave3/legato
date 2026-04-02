package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Paths holds the file paths for generated TLS certificates.
type Paths struct {
	CACert     string // CA certificate (install on devices to trust)
	CAKey      string
	ServerCert string
	ServerKey  string
}

// EnsureCerts generates a self-signed CA and server certificate if they don't
// already exist or are expired. Returns the paths to the cert and key files.
// The CA cert can be installed on mobile devices to trust the server.
func EnsureCerts(dataDir string) (*Paths, error) {
	certsDir := filepath.Join(dataDir, "certs")
	if err := os.MkdirAll(certsDir, 0700); err != nil {
		return nil, fmt.Errorf("creating certs directory: %w", err)
	}

	paths := &Paths{
		CACert:     filepath.Join(certsDir, "ca.pem"),
		CAKey:      filepath.Join(certsDir, "ca-key.pem"),
		ServerCert: filepath.Join(certsDir, "server.pem"),
		ServerKey:  filepath.Join(certsDir, "server-key.pem"),
	}

	// Check if existing certs are valid.
	if certsValid(paths) {
		return paths, nil
	}

	// Generate CA.
	caKey, caCert, caCertPEM, err := generateCA()
	if err != nil {
		return nil, fmt.Errorf("generating CA: %w", err)
	}
	if err := writeKeyFile(paths.CAKey, caKey); err != nil {
		return nil, err
	}
	if err := writePEMFile(paths.CACert, "CERTIFICATE", caCertPEM); err != nil {
		return nil, err
	}

	// Generate server cert signed by CA.
	if err := generateServerCert(paths, caKey, caCert); err != nil {
		return nil, fmt.Errorf("generating server cert: %w", err)
	}

	return paths, nil
}

func certsValid(paths *Paths) bool {
	certData, err := os.ReadFile(paths.ServerCert)
	if err != nil {
		return false
	}
	block, _ := pem.Decode(certData)
	if block == nil {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	// Valid if not expiring within the next 30 days.
	return time.Now().Add(30 * 24 * time.Hour).Before(cert.NotAfter)
}

func generateCA() (*ecdsa.PrivateKey, *x509.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Legato"},
			CommonName:   "Legato Local CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, nil, err
	}

	return key, cert, certDER, nil
}

func generateServerCert(paths *Paths, caKey *ecdsa.PrivateKey, caCert *x509.Certificate) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	serial, err := randomSerial()
	if err != nil {
		return err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Legato"},
			CommonName:   "Legato Web Server",
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		DNSNames:    []string{"localhost"},
		IPAddresses: localIPs(),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}

	if err := writeKeyFile(paths.ServerKey, key); err != nil {
		return err
	}
	return writePEMFile(paths.ServerCert, "CERTIFICATE", certDER)
}

func localIPs() []net.IP {
	ips := []net.IP{
		net.IPv4(127, 0, 0, 1),
		net.IPv6loopback,
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			ips = append(ips, ipNet.IP)
		}
	}
	return ips
}

func writeKeyFile(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshaling key: %w", err)
	}
	return writePEMFile(path, "EC PRIVATE KEY", der)
}

func writePEMFile(path, pemType string, der []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: pemType, Bytes: der})
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}
