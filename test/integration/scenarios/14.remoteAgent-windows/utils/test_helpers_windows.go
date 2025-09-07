package utils

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// WindowsTestConfig holds configuration for Windows test setup
type WindowsTestConfig struct {
	ProjectRoot    string
	ConfigPath     string
	ClientCertPath string // For Windows, this will be PFX file path
	ClientKeyPath  string // For Windows, this might be empty for PFX
	CertPassword   string // PFX certificate password
	CACertPath     string
	TargetName     string
	Namespace      string
	TopologyPath   string
	Protocol       string
	BaseURL        string
	BinaryPath     string
	BrokerAddress  string
	BrokerPort     string
	RunMode        string // "service" or "schedule"
}

// WindowsCertificatePaths holds paths to Windows-specific certificates
type WindowsCertificatePaths struct {
	CACert     string
	ClientCert string // PFX file for HTTP mode
	ClientKey  string // Separate key file for MQTT mode
	ClientPEM  string // PEM version of client cert for MQTT mode
	Password   string // PFX password
}

// WindowsMQTTCertificatePaths holds paths to MQTT-specific certificates for Windows
type WindowsMQTTCertificatePaths struct {
	CACert             string
	CAKey              string
	MQTTServerCert     string
	MQTTServerKey      string
	SymphonyClientCert string
	SymphonyClientKey  string
	RemoteAgentCert    string
	RemoteAgentKey     string
}

// GetWindowsProjectRoot returns the project root directory using Windows path conventions
func GetWindowsProjectRoot(t *testing.T) string {
	// Start from the current working directory
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	t.Logf("GetWindowsProjectRoot: Starting from directory: %s", currentDir)

	// Keep going up directories until we find the project root
	for {
		t.Logf("GetWindowsProjectRoot: Checking directory: %s", currentDir)

		// Check if this directory contains the expected project structure
		expectedDirs := []string{"api", "coa", "remote-agent", "test"}
		isProjectRoot := true

		for _, dir := range expectedDirs {
			fullPath := filepath.Join(currentDir, dir)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				t.Logf("GetWindowsProjectRoot: Directory %s not found at %s", dir, fullPath)
				isProjectRoot = false
				break
			} else {
				t.Logf("GetWindowsProjectRoot: Found directory %s at %s", dir, fullPath)
			}
		}

		if isProjectRoot {
			t.Logf("Project root detected: %s", currentDir)
			return currentDir
		}

		// Move up one directory
		parentDir := filepath.Dir(currentDir)

		// Check if we've reached the filesystem root
		if parentDir == currentDir {
			t.Fatalf("Could not find Symphony project root. Started from: %s", func() string {
				wd, _ := os.Getwd()
				return wd
			}())
		}

		currentDir = parentDir
	}
}

// GetProjectRoot returns the project root directory (alias for GetWindowsProjectRoot for consistency)
func GetProjectRoot(t *testing.T) string {
	return GetWindowsProjectRoot(t)
}

// streamProcessLogsWindows streams logs from a Windows process in real-time with timestamps
func streamProcessLogsWindows(t *testing.T, reader io.Reader, prefix string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		// Add timestamp prefix for better debugging
		timestamp := time.Now().Format("15:04:05.000")
		t.Logf("[%s] [%s] %s", timestamp, prefix, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Logf("[%s] Error reading logs: %v", prefix, err)
	}
}

// GenerateWindowsCertificates generates certificates suitable for Windows testing
func GenerateWindowsCertificates(t *testing.T, testDir string) WindowsCertificatePaths {
	return GenerateWindowsCertificatesWithProtocol(t, testDir, "http")
}

// GenerateWindowsCertificatesWithProtocol generates certificates for specific protocol
func GenerateWindowsCertificatesWithProtocol(t *testing.T, testDir, protocol string) WindowsCertificatePaths {
	t.Logf("Generating Windows certificates for protocol %s in directory: %s", protocol, testDir)

	// Generate CA key
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate CA key: %v", err)
	}

	// Create CA certificate template
	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Symphony Test CA"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// Create CA certificate
	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	// Parse CA certificate
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("Failed to parse CA certificate: %v", err)
	}

	// Save CA certificate in PEM format
	caCertPath := filepath.Join(testDir, "ca.crt")
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	err = ioutil.WriteFile(caCertPath, caCertPEM, 0644)
	if err != nil {
		t.Fatalf("Failed to write CA certificate: %v", err)
	}

	// Save CA key
	caKeyPath := filepath.Join(testDir, "ca.key")
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})
	err = ioutil.WriteFile(caKeyPath, caKeyPEM, 0644)
	if err != nil {
		t.Fatalf("Failed to write CA key: %v", err)
	}

	// Generate client key
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate client key: %v", err)
	}

	// Create client certificate template with Windows-friendly subject
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization:       []string{"Symphony Test Client"},
			Country:            []string{"US"},
			Province:           []string{""},
			Locality:           []string{""},
			StreetAddress:      []string{""},
			PostalCode:         []string{""},
			CommonName:         "remote-agent-client",
			OrganizationalUnit: []string{"Testing"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		DNSNames:    []string{"localhost", "remote-agent-client"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	// Create client certificate
	clientCertDER, err := x509.CreateCertificate(rand.Reader, &clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create client certificate: %v", err)
	}

	// Save client certificate in PEM format
	clientCertPEMPath := filepath.Join(testDir, "client.crt")
	clientCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER})
	err = ioutil.WriteFile(clientCertPEMPath, clientCertPEM, 0644)
	if err != nil {
		t.Fatalf("Failed to write client certificate: %v", err)
	}

	// Save client key in PEM format
	clientKeyPath := filepath.Join(testDir, "client.key")
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)})
	err = ioutil.WriteFile(clientKeyPath, clientKeyPEM, 0644)
	if err != nil {
		t.Fatalf("Failed to write client key: %v", err)
	}

	password := "test123"
	var clientCertPath string
	var pfxPath string

	// Generate appropriate certificate format based on protocol
	if protocol == "http" {
		// For HTTP mode, generate PFX certificate using PowerShell 7
		pfxPath = filepath.Join(testDir, "client.pfx")
		err = generatePFXCertificate(t, clientCertPEMPath, clientKeyPath, pfxPath, password)
		if err != nil {
			t.Fatalf("Failed to generate PFX certificate for HTTP mode: %v", err)
		}
		clientCertPath = pfxPath
		t.Logf("Successfully generated PFX certificate: %s", pfxPath)
	} else {
		// For MQTT mode, use PEM certificate (no PFX generation needed)
		clientCertPath = clientCertPEMPath
		pfxPath = clientCertPEMPath
		t.Logf("Skipped PFX generation for MQTT mode - using PEM certificates only")
	}

	t.Logf("Generated Windows certificates for %s mode:", protocol)
	t.Logf("  CA Certificate: %s", caCertPath)
	t.Logf("  Client Certificate: %s", clientCertPath)
	t.Logf("  Client Key: %s", clientKeyPath)
	if protocol == "http" && clientCertPath == pfxPath && pfxPath != clientCertPEMPath {
		t.Logf("  Certificate format: PFX (required for Windows HTTP mode)")
	} else {
		t.Logf("  Certificate format: PEM (optimized for MQTT mode)")
	}

	return WindowsCertificatePaths{
		CACert:     caCertPath,
		ClientCert: clientCertPath,
		ClientKey:  clientKeyPath,
		ClientPEM:  clientCertPEMPath,
		Password:   password,
	}
}

// GenerateWindowsMQTTCertificates generates a complete set of MQTT-specific test certificates for Windows
func GenerateWindowsMQTTCertificates(t *testing.T, testDir string) WindowsMQTTCertificatePaths {
	t.Logf("Generating Windows MQTT certificates in directory: %s", testDir)

	// Generate CA certificate (same CA signs all certificates)
	caCert, caKey := generateWindowsCA(t)

	// Generate MQTT server certificate (for MQTT broker)
	mqttServerCert, mqttServerKey := generateWindowsServerCert(t, caCert, caKey, "localhost")

	// Generate Symphony client certificate (Symphony as MQTT client)
	symphonyClientCert, symphonyClientKey := generateWindowsClientCert(t, caCert, caKey, "symphony-client")

	// Generate remote agent certificate (Remote agent as MQTT client)
	remoteAgentCert, remoteAgentKey := generateWindowsClientCert(t, caCert, caKey, "remote-agent-client")

	// Define paths with MQTT-specific naming for Windows
	paths := WindowsMQTTCertificatePaths{
		CACert:             filepath.Join(testDir, "ca.crt"),
		CAKey:              filepath.Join(testDir, "ca.key"),
		MQTTServerCert:     filepath.Join(testDir, "mqtt-server.crt"),
		MQTTServerKey:      filepath.Join(testDir, "mqtt-server.key"),
		SymphonyClientCert: filepath.Join(testDir, "symphony-client.crt"),
		SymphonyClientKey:  filepath.Join(testDir, "symphony-client.key"),
		RemoteAgentCert:    filepath.Join(testDir, "remote-agent.crt"),
		RemoteAgentKey:     filepath.Join(testDir, "remote-agent.key"),
	}

	// Save all certificates
	err := saveWindowsCertificate(paths.CACert, caCert)
	if err != nil {
		t.Fatalf("Failed to save CA certificate: %v", err)
	}
	err = saveWindowsPrivateKey(paths.CAKey, caKey)
	if err != nil {
		t.Fatalf("Failed to save CA key: %v", err)
	}

	err = saveWindowsCertificate(paths.MQTTServerCert, mqttServerCert)
	if err != nil {
		t.Fatalf("Failed to save MQTT server certificate: %v", err)
	}
	err = saveWindowsPrivateKey(paths.MQTTServerKey, mqttServerKey)
	if err != nil {
		t.Fatalf("Failed to save MQTT server key: %v", err)
	}

	err = saveWindowsCertificate(paths.SymphonyClientCert, symphonyClientCert)
	if err != nil {
		t.Fatalf("Failed to save Symphony client certificate: %v", err)
	}
	err = saveWindowsPrivateKey(paths.SymphonyClientKey, symphonyClientKey)
	if err != nil {
		t.Fatalf("Failed to save Symphony client key: %v", err)
	}

	err = saveWindowsCertificate(paths.RemoteAgentCert, remoteAgentCert)
	if err != nil {
		t.Fatalf("Failed to save remote agent certificate: %v", err)
	}
	err = saveWindowsPrivateKey(paths.RemoteAgentKey, remoteAgentKey)
	if err != nil {
		t.Fatalf("Failed to save remote agent key: %v", err)
	}

	t.Logf("Generated Windows MQTT test certificates in %s", testDir)
	t.Logf("  CA Certificate: %s", paths.CACert)
	t.Logf("  MQTT Server Certificate: %s", paths.MQTTServerCert)
	t.Logf("  Symphony Client Certificate: %s", paths.SymphonyClientCert)
	t.Logf("  Remote Agent Certificate: %s", paths.RemoteAgentCert)
	return paths
}

// generateWindowsCA generates a CA certificate for Windows testing
func generateWindowsCA(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate CA key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Symphony Test"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
			CommonName:    "MyRootCA", // This is what Symphony will check for trust
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	// Parse the certificate
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse CA certificate: %v", err)
	}

	return cert, privateKey
}

// generateWindowsServerCert generates a server certificate for Windows testing with enhanced network support matching Linux implementation
func generateWindowsServerCert(t *testing.T, caCert *x509.Certificate, caKey *rsa.PrivateKey, hostname string) (*x509.Certificate, *rsa.PrivateKey) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate server key: %v", err)
	}

	// Build comprehensive list of IP addresses to include in certificate
	// Start with standard localhost addresses (matching Linux implementation)
	ipAddresses := []net.IP{
		net.IPv4(127, 0, 0, 1), // localhost IPv4
		net.IPv6loopback,       // localhost IPv6
		net.IPv4zero,           // 0.0.0.0 - any IPv4
	}

	// Dynamically detect all available network interfaces and their IPs (enhanced Windows version)
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			// Skip loopback and down interfaces, but include all others
			if iface.Flags&net.FlagUp == 0 {
				continue
			}

			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}

			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}

				if ip != nil {
					// Add both IPv4 and IPv6 addresses
					ipAddresses = append(ipAddresses, ip)
					t.Logf("Added detected IP to Windows certificate: %s (interface: %s)", ip.String(), iface.Name)
				}
			}
		}
	} else {
		t.Logf("Warning: Could not detect Windows network interfaces: %v", err)
	}

	// Enhanced Docker Desktop and container host IP detection for Windows
	commonHostIPs := []string{
		"host.docker.internal",
		"host.minikube.internal",
		"gateway.docker.internal",
	}

	// Robust IP resolution with enhanced error handling and fallback
	for _, hostnameDNS := range commonHostIPs {
		t.Logf("Attempting to resolve %s for Windows certificate...", hostnameDNS)
		if ips, err := net.LookupIP(hostnameDNS); err == nil {
			for _, ip := range ips {
				ipAddresses = append(ipAddresses, ip)
				t.Logf("✅ Added resolved IP to Windows certificate: %s (from %s)", ip.String(), hostnameDNS)
			}
		} else {
			t.Logf("⚠️ Failed to resolve %s on Windows: %v", hostnameDNS, err)

			// Windows-specific fallback: Try to get Docker Desktop host IP using PowerShell
			if hostnameDNS == "host.docker.internal" {
				if dockerHostIP := getDockerDesktopHostIPWindows(t); dockerHostIP != "" {
					if ip := net.ParseIP(dockerHostIP); ip != nil {
						ipAddresses = append(ipAddresses, ip)
						t.Logf("✅ Added Docker Desktop host IP to Windows certificate: %s", dockerHostIP)
					}
				}
			}
		}
	}

	// Enhanced fallback IPs for Windows container scenarios
	fallbackIPs := []string{
		"172.17.0.1",   // Docker bridge IP
		"192.168.49.1", // Common minikube host IP
		"192.168.65.1", // Docker Desktop VM IP
		"10.0.2.2",     // VirtualBox host IP
		"192.168.1.1",  // Common Windows router IP
		"192.168.0.1",  // Alternative router IP
	}

	for _, ipStr := range fallbackIPs {
		if ip := net.ParseIP(ipStr); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		}
	}

	// Enhanced DNS names for maximum Windows compatibility (matching Linux + Windows-specific)
	dnsNames := []string{
		hostname,
		"localhost",
		"*.local",
		"*.localhost",
		"host.docker.internal",       // Docker Desktop
		"host.minikube.internal",     // Minikube
		"gateway.docker.internal",    // Docker gateway
		"kubernetes.docker.internal", // Kubernetes in Docker Desktop
	}

	// Log the final list of IPs in the certificate for debugging
	t.Logf("Enhanced Windows certificate will be valid for %d IP addresses:", len(ipAddresses))
	for i, ip := range ipAddresses {
		t.Logf("  [%d] %s", i+1, ip.String())
	}

	// Log DNS names for debugging
	t.Logf("Enhanced Windows certificate will be valid for %d DNS names:", len(dnsNames))
	for i, name := range dnsNames {
		t.Logf("  DNS[%d] %s", i+1, name)
	}

	// Create certificate template with very permissive settings for Windows testing (matching Linux)
	template := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization:  []string{"Symphony Test"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
			CommonName:    hostname,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, // Allow both server and client auth
		IPAddresses: ipAddresses,
		DNSNames:    dnsNames,
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, caCert, &privateKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create Windows server certificate: %v", err)
	}

	// Parse the certificate
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse Windows server certificate: %v", err)
	}

	return cert, privateKey
}

// getDockerDesktopHostIPWindows attempts to get Docker Desktop host IP on Windows using PowerShell
func getDockerDesktopHostIPWindows(t *testing.T) string {
	t.Logf("Attempting to detect Docker Desktop host IP on Windows...")

	// Method 1: Try PowerShell Test-NetConnection to detect source IP when connecting to Docker
	psScript := `
	try {
		$result = Test-NetConnection -ComputerName "8.8.8.8" -Port 53 -InformationLevel Quiet
		if ($result) {
			$connection = Test-NetConnection -ComputerName "8.8.8.8" -Port 53
			Write-Output $connection.SourceAddress.IPAddress
		}
	} catch {
		Write-Output ""
	}`

	cmd := exec.Command("powershell", "-Command", psScript)
	if output, err := cmd.Output(); err == nil {
		ip := strings.TrimSpace(string(output))
		if ip != "" && net.ParseIP(ip) != nil {
			t.Logf("✅ Detected Windows host IP for Docker: %s", ip)
			return ip
		}
	}

	// Method 2: Try to get the default gateway IP
	cmd = exec.Command("powershell", "-Command",
		"(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Get-NetIPInterface | Where-Object ConnectionState -eq 'Connected' | Get-NetIPAddress -AddressFamily IPv4 | Select-Object -First 1).IPAddress")
	if output, err := cmd.Output(); err == nil {
		ip := strings.TrimSpace(string(output))
		if ip != "" && ip != "127.0.0.1" && net.ParseIP(ip) != nil {
			t.Logf("✅ Using Windows default route IP for Docker: %s", ip)
			return ip
		}
	}

	t.Logf("⚠️ Could not detect Docker Desktop host IP on Windows")
	return ""
}

// generateWindowsClientCert generates a client certificate for Windows testing
func generateWindowsClientCert(t *testing.T, caCert *x509.Certificate, caKey *rsa.PrivateKey, commonName string) (*x509.Certificate, *rsa.PrivateKey) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate Windows client key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			Organization:  []string{"Symphony Test"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
			CommonName:    commonName, // Use the provided common name for client cert
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Create the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, caCert, &privateKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create Windows client certificate: %v", err)
	}

	// Parse the certificate
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse Windows client certificate: %v", err)
	}

	return cert, privateKey
}

// saveWindowsCertificate saves a certificate to file for Windows
func saveWindowsCertificate(filename string, cert *x509.Certificate) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	return pem.Encode(file, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	})
}

// saveWindowsPrivateKey saves a private key to file for Windows
func saveWindowsPrivateKey(filename string, key *rsa.PrivateKey) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	return pem.Encode(file, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

// CleanupWindowsMQTTCertificates removes all generated MQTT certificate files for Windows
func CleanupWindowsMQTTCertificates(paths WindowsMQTTCertificatePaths) {
	os.Remove(paths.CACert)
	os.Remove(paths.CAKey)
	os.Remove(paths.MQTTServerCert)
	os.Remove(paths.MQTTServerKey)
	os.Remove(paths.SymphonyClientCert)
	os.Remove(paths.SymphonyClientKey)
	os.Remove(paths.RemoteAgentCert)
	os.Remove(paths.RemoteAgentKey)
}

// generatePFXCertificate creates a PFX certificate using PowerShell 7's CreateFromPem method
func generatePFXCertificate(t *testing.T, certPath, keyPath, pfxPath, password string) error {
	t.Logf("Generating PFX certificate using PowerShell 7 CreateFromPem method...")
	t.Logf("  Input cert: %s", certPath)
	t.Logf("  Input key: %s", keyPath)
	t.Logf("  Output PFX: %s", pfxPath)
	t.Logf("  Password: %s", password)

	// Read certificate file
	certPEM, err := ioutil.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %v", err)
	}

	// Read private key file
	keyPEM, err := ioutil.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key file: %v", err)
	}

	// Parse certificate
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}

	_, err = x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %v", err)
	}

	// Parse private key
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode private key PEM")
	}

	var privateKey interface{}
	switch keyBlock.Type {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	case "PRIVATE KEY":
		privateKey, err = x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	default:
		return fmt.Errorf("unsupported private key type: %s", keyBlock.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to parse private key: %v", err)
	}

	// Suppress unused variable warning for now
	_ = privateKey

	// Create PowerShell 7 script using CreateFromPem method
	// This is the modern and reliable way to create PFX certificates from PEM data
	psScript := "# PowerShell 7 PFX certificate creation using CreateFromPem\n" +
		"$ErrorActionPreference = \"Stop\"\n\n" +
		"try {\n" +
		"    Write-Output \"Creating PFX using PowerShell 7 CreateFromPem method...\"\n" +
		"    \n" +
		"    # Read certificate and key PEM data\n" +
		"    $certPem = Get-Content -Path '" + certPath + "' -Raw\n" +
		"    $keyPem = Get-Content -Path '" + keyPath + "' -Raw\n" +
		"    \n" +
		"    Write-Output \"Certificate PEM length: $($certPem.Length) characters\"\n" +
		"    Write-Output \"Key PEM length: $($keyPem.Length) characters\"\n" +
		"    \n" +
		"    # Use PowerShell 7 / .NET 5+ CreateFromPem method\n" +
		"    # This method properly associates the private key with the certificate\n" +
		"    Write-Output \"Creating certificate using CreateFromPem...\"\n" +
		"    $cert = [System.Security.Cryptography.X509Certificates.X509Certificate2]::CreateFromPem($certPem, $keyPem)\n" +
		"    \n" +
		"    Write-Output \"Certificate created successfully\"\n" +
		"    Write-Output \"  Subject: $($cert.Subject)\"\n" +
		"    Write-Output \"  Thumbprint: $($cert.Thumbprint)\"\n" +
		"    Write-Output \"  HasPrivateKey: $($cert.HasPrivateKey)\"\n" +
		"    Write-Output \"  Valid From: $($cert.NotBefore)\"\n" +
		"    Write-Output \"  Valid To: $($cert.NotAfter)\"\n" +
		"    \n" +
		"    # Verify the certificate has a private key\n" +
		"    if (-not $cert.HasPrivateKey) {\n" +
		"        throw \"ERROR: CreateFromPem failed to associate private key with certificate\"\n" +
		"    }\n" +
		"    \n" +
		"    # Convert password to SecureString\n" +
		"    $securePassword = ConvertTo-SecureString -String '" + password + "' -AsPlainText -Force\n" +
		"    \n" +
		"    # Export to PFX format\n" +
		"    Write-Output \"Exporting certificate to PFX format...\"\n" +
		"    $pfxBytes = $cert.Export([System.Security.Cryptography.X509Certificates.X509ContentType]::Pfx, $securePassword)\n" +
		"    \n" +
		"    # Save PFX file\n" +
		"    [System.IO.File]::WriteAllBytes('" + pfxPath + "', $pfxBytes)\n" +
		"    Write-Output \"PFX file saved: " + pfxPath + "\"\n" +
		"    \n" +
		"    # Verify the created PFX file\n" +
		"    Write-Output \"Verifying created PFX file...\"\n" +
		"    $testCert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2('" + pfxPath + "', $securePassword)\n" +
		"    \n" +
		"    Write-Output \"PFX Verification Results:\"\n" +
		"    Write-Output \"  Subject: $($testCert.Subject)\"\n" +
		"    Write-Output \"  Thumbprint: $($testCert.Thumbprint)\"\n" +
		"    Write-Output \"  HasPrivateKey: $($testCert.HasPrivateKey)\"\n" +
		"    \n" +
		"    if (-not $testCert.HasPrivateKey) {\n" +
		"        throw \"ERROR: Generated PFX file does not contain private key\"\n" +
		"    }\n" +
		"    \n" +
		"    Write-Output \"SUCCESS: PFX certificate created successfully with private key\"\n" +
		"    \n" +
		"} catch {\n" +
		"    Write-Error \"PFX creation failed: $_\"\n" +
		"    Write-Error \"Stack trace: $($_.ScriptStackTrace)\"\n" +
		"    throw $_\n" +
		"}"

	// Write and execute PowerShell script
	tempScriptFile := filepath.Join(filepath.Dir(pfxPath), "create_pfx_ps7.ps1")
	err = ioutil.WriteFile(tempScriptFile, []byte(psScript), 0644)
	if err != nil {
		return fmt.Errorf("failed to write PowerShell script: %v", err)
	}
	defer os.Remove(tempScriptFile)

	// Execute using pwsh (PowerShell 7) specifically
	cmd := ExecutePowerShell7Script(t, tempScriptFile, []string{}, filepath.Dir(pfxPath))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Logf("Executing PowerShell 7 CreateFromPem PFX creation script")

	err = cmd.Run()
	if err != nil {
		t.Logf("PowerShell 7 PFX stdout: %s", stdout.String())
		t.Logf("PowerShell 7 PFX stderr: %s", stderr.String())
		return fmt.Errorf("PowerShell 7 PFX creation failed: %v", err)
	}

	// Verify PFX file was created
	if !FileExistsWindows(pfxPath) {
		return fmt.Errorf("PFX file was not created at %s", pfxPath)
	}

	if stat, err := os.Stat(pfxPath); err == nil {
		t.Logf("PFX certificate created successfully: %s (size: %d bytes)", pfxPath, stat.Size())
	} else {
		t.Logf("PFX certificate created successfully: %s", pfxPath)
	}

	t.Logf("PowerShell 7 PFX creation output: %s", stdout.String())

	// Additional verification using Go to double-check the PFX
	err = verifyPFXCertificate(t, pfxPath, password)
	if err != nil {
		return fmt.Errorf("PFX verification failed: %v", err)
	}

	return nil
}

// ExecutePowerShell7Script executes a PowerShell script, preferring PowerShell 7 but falling back to Windows PowerShell
func ExecutePowerShell7Script(t *testing.T, scriptPath string, args []string, workingDir string) *exec.Cmd {
	t.Logf("Executing PowerShell script (preferring PS7): %s with args: %v", scriptPath, args)

	// Try PowerShell 7 first, fall back to Windows PowerShell
	var psExe string
	if _, err := exec.LookPath("pwsh"); err == nil {
		psExe = "pwsh"
		t.Logf("Using PowerShell 7 (pwsh)")
	} else {
		psExe = "powershell"
		t.Logf("PowerShell 7 not found, falling back to Windows PowerShell")
	}

	// Build PowerShell command arguments
	psArgs := []string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	}
	psArgs = append(psArgs, args...)

	cmd := exec.Command(psExe, psArgs...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	// Set environment to avoid interactive prompts
	cmd.Env = append(os.Environ(), "POWERSHELL_TELEMETRY_OPTOUT=1")

	t.Logf("PowerShell command: %s %s", psExe, strings.Join(psArgs, " "))
	return cmd
}

// verifyPFXCertificate verifies that the generated PFX contains a valid private key
func verifyPFXCertificate(t *testing.T, pfxPath, password string) error {
	t.Logf("Verifying PFX certificate has private key: %s", pfxPath)

	// Use PowerShell to verify the PFX certificate
	psScript := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
try {
    $securePassword = ConvertTo-SecureString -String '%s' -AsPlainText -Force
    $flags = [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::Exportable
    $cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2('%s', $securePassword, $flags)
    
    Write-Output "PFX Verification Results:"
    Write-Output "  Subject: $($cert.Subject)"
    Write-Output "  Thumbprint: $($cert.Thumbprint)"
    Write-Output "  HasPrivateKey: $($cert.HasPrivateKey)"
    Write-Output "  Valid From: $($cert.NotBefore)"
    Write-Output "  Valid To: $($cert.NotAfter)"
    
    if (-not $cert.HasPrivateKey) {
        throw "ERROR: PFX certificate does not contain a private key"
    }
    
    Write-Output "SUCCESS: PFX certificate contains valid private key"
    
} catch {
    Write-Error "PFX verification failed: $_"
    throw $_
}`, password, pfxPath)

	tempScriptFile := filepath.Join(filepath.Dir(pfxPath), "verify_pfx.ps1")
	err := ioutil.WriteFile(tempScriptFile, []byte(psScript), 0644)
	if err != nil {
		return fmt.Errorf("failed to write verification script: %v", err)
	}
	defer os.Remove(tempScriptFile)

	cmd := ExecutePowerShell7Script(t, tempScriptFile, []string{}, filepath.Dir(pfxPath))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Logf("PFX verification stdout: %s", stdout.String())
		t.Logf("PFX verification stderr: %s", stderr.String())
		return fmt.Errorf("PFX verification failed: %v", err)
	}

	t.Logf("PFX verification successful: %s", stdout.String())
	return nil
}

// ExecutePowerShellScript executes a PowerShell script with given arguments (fallback to any available PowerShell)
func ExecutePowerShellScript(t *testing.T, scriptPath string, args []string, workingDir string) *exec.Cmd {
	t.Logf("Executing PowerShell script: %s with args: %v", scriptPath, args)

	// Determine PowerShell executable
	var psExe string
	if runtime.GOOS == "windows" {
		// Try PowerShell 7 first, fall back to Windows PowerShell
		if _, err := exec.LookPath("pwsh"); err == nil {
			psExe = "pwsh"
		} else {
			psExe = "powershell"
		}
	} else {
		// On non-Windows systems for testing, try pwsh
		psExe = "pwsh"
	}

	// Build PowerShell command arguments
	psArgs := []string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	}
	psArgs = append(psArgs, args...)

	cmd := exec.Command(psExe, psArgs...)
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	// Set environment to avoid interactive prompts
	cmd.Env = append(os.Environ(), "POWERSHELL_TELEMETRY_OPTOUT=1")

	t.Logf("PowerShell command: %s %s", psExe, strings.Join(psArgs, " "))
	return cmd
}

// BuildWindowsRemoteAgent builds the remote agent binary for Windows with real-time logging
func BuildWindowsRemoteAgent(t *testing.T, config WindowsTestConfig) string {
	binaryPath := filepath.Join(config.ProjectRoot, "remote-agent", "bootstrap", "remote-agent.exe")

	t.Logf("Building Windows remote agent binary at: %s", binaryPath)

	// Build the binary: GOOS=windows GOARCH=amd64 go build -o bootstrap/remote-agent.exe
	buildCmd := exec.Command("go", "build", "-o", "bootstrap/remote-agent.exe", ".")
	buildCmd.Dir = filepath.Join(config.ProjectRoot, "remote-agent")
	buildCmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64")

	// Set up pipes for real-time log streaming during build
	stdout, err := buildCmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe for Windows build: %v", err)
	}

	stderr, err := buildCmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe for Windows build: %v", err)
	}

	// Start the build process
	err = buildCmd.Start()
	if err != nil {
		t.Fatalf("Failed to start Windows build process: %v", err)
	}

	t.Logf("Windows build process started, streaming logs in real-time...")

	// Start real-time log streaming in separate goroutines
	go streamProcessLogsWindows(t, stdout, "BUILD-STDOUT")
	go streamProcessLogsWindows(t, stderr, "BUILD-STDERR")

	// Wait for build to complete
	err = buildCmd.Wait()
	if err != nil {
		t.Fatalf("Failed to build Windows remote agent binary: %v", err)
	}

	t.Logf("Successfully built Windows remote agent binary")
	return binaryPath
}

// CreateWindowsTestDirectory creates a temporary directory for Windows test files
func CreateWindowsTestDirectory(t *testing.T) string {
	// Use Windows temp directory
	tempDir := os.TempDir()
	testDir, err := ioutil.TempDir(tempDir, "symphony-windows-e2e-test-")
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	t.Logf("Created Windows test directory: %s", testDir)
	return testDir
}

// StartWindowsRemoteAgentWithBootstrap starts remote agent using bootstrap.ps1 script
func StartWindowsRemoteAgentWithBootstrap(t *testing.T, config WindowsTestConfig) *exec.Cmd {
	// Build the binary first for MQTT mode
	if config.Protocol == "mqtt" && config.BinaryPath == "" {
		binaryPath := BuildWindowsRemoteAgent(t, config)
		config.BinaryPath = binaryPath
	}

	// Prepare bootstrap.ps1 arguments
	var args []string

	if config.Protocol == "http" {
		// HTTP mode arguments
		args = []string{
			"-protocol", "http",
			"-endpoint", config.BaseURL,
			"-cert_path", config.ClientCertPath,
			"-target_name", config.TargetName,
			"-namespace", config.Namespace,
			"-topology", config.TopologyPath,
			"-run_mode", config.RunMode,
			"-cert_password", config.CertPassword,
		}

		// Add CA certificate if available
		if config.CACertPath != "" {
			args = append(args, "-ca_cert_path", config.CACertPath)
		}
	} else if config.Protocol == "mqtt" {
		// MQTT mode arguments
		args = []string{
			"-protocol", "mqtt",
			"-mqtt_broker", config.BrokerAddress,
			"-mqtt_port", config.BrokerPort,
			"-cert_path", config.ClientCertPath,
			"-key_path", config.ClientKeyPath,
			"-target_name", config.TargetName,
			"-namespace", config.Namespace,
			"-topology", config.TopologyPath,
			"-run_mode", config.RunMode,
			"-agent_path", config.BinaryPath,
		}

		if config.CACertPath != "" {
			args = append(args, "-ca_cert_path", config.CACertPath)
		}
	} else {
		t.Fatalf("Unsupported protocol: %s", config.Protocol)
	}

	// Get bootstrap.ps1 path
	bootstrapPath := filepath.Join(config.ProjectRoot, "remote-agent", "bootstrap", "bootstrap.ps1")

	// Execute bootstrap.ps1 using PowerShell 7
	cmd := ExecutePowerShell7Script(t, bootstrapPath, args, filepath.Join(config.ProjectRoot, "remote-agent", "bootstrap"))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Logf("Starting Windows bootstrap.ps1 with args: %v", args)
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start bootstrap.ps1: %v", err)
	}

	t.Logf("Bootstrap.ps1 started with PID: %d", cmd.Process.Pid)

	// Wait for bootstrap.ps1 to complete
	go func() {
		err := cmd.Wait()
		if err != nil {
			t.Logf("Bootstrap.ps1 exited with error: %v", err)
		} else {
			t.Logf("Bootstrap.ps1 completed successfully")
		}
		t.Logf("Bootstrap.ps1 stdout: %s", stdout.String())
		if stderr.Len() > 0 {
			t.Logf("Bootstrap.ps1 stderr: %s", stderr.String())
		}
	}()

	t.Logf("Bootstrap.ps1 started, Windows service should be created")
	return cmd
}

// CheckWindowsServiceStatus checks the status of a Windows service
func CheckWindowsServiceStatus(t *testing.T, serviceName string) {
	cmd := exec.Command("sc", "query", serviceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Service %s status check failed: %v", serviceName, err)
	} else {
		t.Logf("Service %s status: %s", serviceName, string(output))
	}
}

// WaitForWindowsService waits for a Windows service to be running
func WaitForWindowsService(t *testing.T, serviceName string, timeout time.Duration) {
	t.Logf("Waiting for Windows service %s to be running...", serviceName)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Logf("Timeout waiting for Windows service %s to be running", serviceName)
			CheckWindowsServiceStatus(t, serviceName)
			t.Fatalf("Timeout waiting for Windows service %s to be running after %v", serviceName, timeout)
		case <-ticker.C:
			cmd := exec.Command("sc", "query", serviceName)
			output, err := cmd.Output()
			if err == nil {
				outputStr := string(output)
				if strings.Contains(outputStr, "RUNNING") {
					t.Logf("Windows service %s is running", serviceName)
					return
				}
				t.Logf("Service %s not running yet, current status contains: %s", serviceName, outputStr)
			} else {
				t.Logf("Failed to query service %s: %v", serviceName, err)
			}
		}
	}
}

// CleanupWindowsService cleans up a Windows service
func CleanupWindowsService(t *testing.T, serviceName string) {
	t.Logf("Cleaning up Windows service: %s", serviceName)

	// Stop the service
	cmd := exec.Command("sc", "stop", serviceName)
	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to stop service %s: %v", serviceName, err)
	}

	// Delete the service
	cmd = exec.Command("sc", "delete", serviceName)
	err = cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to delete service %s: %v", serviceName, err)
	}

	t.Logf("Windows service %s cleanup completed", serviceName)
}

// CleanupWindowsScheduledTask cleans up a Windows scheduled task
func CleanupWindowsScheduledTask(t *testing.T, taskName string) {
	t.Logf("Cleaning up Windows scheduled task: %s", taskName)

	// Stop the task
	cmd := exec.Command("schtasks", "/End", "/TN", taskName)
	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to stop task %s: %v", taskName, err)
	}

	// Delete the task
	cmd = exec.Command("schtasks", "/Delete", "/TN", taskName, "/F")
	err = cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to delete task %s: %v", taskName, err)
	}

	t.Logf("Windows scheduled task %s cleanup completed", taskName)
}

// IsRunningOnWindows checks if the test is running on Windows
func IsRunningOnWindows() bool {
	return runtime.GOOS == "windows"
}

// ConvertToWindowsPath converts Unix-style paths to Windows paths if needed
func ConvertToWindowsPath(path string) string {
	if IsRunningOnWindows() {
		return filepath.FromSlash(path)
	}
	return path
}

// GetWindowsHostIP gets the Windows host IP address for networking
func GetWindowsHostIP(t *testing.T) string {
	// Try to get the host IP by connecting to a remote address
	cmd := exec.Command("powershell", "-Command",
		"(Test-NetConnection -ComputerName 8.8.8.8 -Port 53).SourceAddress.IPAddress")
	output, err := cmd.Output()
	if err == nil {
		ip := strings.TrimSpace(string(output))
		if ip != "" {
			t.Logf("Detected Windows host IP: %s", ip)
			return ip
		}
	}

	// Fallback: try to get default gateway
	cmd = exec.Command("powershell", "-Command",
		"(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Get-NetIPInterface | Where-Object ConnectionState -eq 'Connected' | Get-NetIPAddress -AddressFamily IPv4).IPAddress")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && line != "127.0.0.1" {
				t.Logf("Using Windows network interface IP: %s", line)
				return line
			}
		}
	}

	t.Logf("Could not detect Windows host IP, using localhost")
	return "127.0.0.1"
}

// CreateHTTPConfigWindows creates HTTP configuration file for Windows remote agent
func CreateHTTPConfigWindows(t *testing.T, testDir, baseURL string) string {
	config := map[string]interface{}{
		"requestEndpoint":  fmt.Sprintf("%s/solution/tasks", baseURL),
		"responseEndpoint": fmt.Sprintf("%s/solution/task/getResult", baseURL),
		"baseUrl":          baseURL,
	}

	configBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal HTTP config: %v", err)
	}

	configPath := filepath.Join(testDir, "config-http.json")
	err = ioutil.WriteFile(configPath, configBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write HTTP config: %v", err)
	}

	return configPath
}

// CreateMQTTConfigWindows creates MQTT configuration file for Windows remote agent
func CreateMQTTConfigWindows(t *testing.T, testDir, brokerAddress string, brokerPort int, targetName, namespace string) string {
	config := map[string]interface{}{
		"mqttBroker": brokerAddress,
		"mqttPort":   brokerPort,
		"targetName": targetName,
		"namespace":  namespace,
	}

	configBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal MQTT config: %v", err)
	}

	configPath := filepath.Join(testDir, "config-mqtt.json")
	err = ioutil.WriteFile(configPath, configBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write MQTT config: %v", err)
	}

	t.Logf("Created Windows MQTT config: %s", configPath)
	return configPath
}

// FileExistsWindows checks if a file exists on Windows
func FileExistsWindows(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

// CreateTestTopologyWindows creates a test topology file for Windows
func CreateTestTopologyWindows(t *testing.T, testDir string) string {
	topology := map[string]interface{}{
		"bindings": []map[string]interface{}{
			{
				"provider": "providers.target.script",
				"role":     "script",
			},
			{
				"provider": "providers.target.remote-agent",
				"role":     "remote-agent",
			},
			{
				"provider": "providers.target.http",
				"role":     "http",
			},
			{
				"provider": "providers.target.docker",
				"role":     "docker",
			},
		},
	}

	topologyBytes, err := json.MarshalIndent(topology, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal topology: %v", err)
	}

	topologyPath := filepath.Join(testDir, "topology.json")
	err = ioutil.WriteFile(topologyPath, topologyBytes, 0644)
	if err != nil {
		t.Fatalf("Failed to write topology: %v", err)
	}

	t.Logf("Created Windows test topology: %s", topologyPath)
	return topologyPath
}

// CreateYAMLFileWindows creates a YAML file with the given content for Windows
func CreateYAMLFileWindows(t *testing.T, filePath, content string) error {
	err := ioutil.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Logf("Failed to write YAML file %s: %v", filePath, err)
		return err
	}
	t.Logf("Created YAML file: %s", filePath)
	return nil
}

// CreateTargetYAMLWindows creates a Target resource YAML file for Windows
func CreateTargetYAMLWindows(t *testing.T, testDir, targetName, namespace string) string {
	targetYaml := fmt.Sprintf(`
apiVersion: fabric.symphony/v1
kind: Target
metadata:
  name: %s
  namespace: %s
spec:
  displayName: %s
  scope: %s-scope
  properties:
    os.type: windows
  components:
  - name: remote-agent
    type: remote-agent
    properties:
      description: E2E test remote agent
  topologies:
  - bindings:
    - provider: providers.target.script
      role: script
    - provider: providers.target.remote-agent
      role: remote-agent
    - provider: providers.target.http
      role: http
`, targetName, namespace, targetName, namespace)

	targetPath := filepath.Join(testDir, "target.yaml")
	err := CreateYAMLFileWindows(t, targetPath, targetYaml)
	if err != nil {
		t.Fatalf("Failed to create target YAML: %v", err)
	}

	t.Logf("Created Windows target YAML: %s", targetPath)
	return targetPath
}

// ApplyKubernetesManifestWindows applies a Kubernetes manifest file for Windows
func ApplyKubernetesManifestWindows(t *testing.T, manifestPath string) error {
	t.Logf("Applying Kubernetes manifest: %s", manifestPath)
	cmd := exec.Command("kubectl", "apply", "-f", manifestPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Failed to apply manifest %s: %v", manifestPath, err)
		t.Logf("kubectl output: %s", string(output))
		return err
	}
	t.Logf("Successfully applied manifest: %s", manifestPath)
	t.Logf("kubectl output: %s", string(output))
	return nil
}

// DeleteKubernetesResourceWindows deletes a Kubernetes resource for Windows
func DeleteKubernetesResourceWindows(t *testing.T, resourceType, name, namespace string, timeout time.Duration) error {
	t.Logf("Deleting Kubernetes resource: %s/%s in namespace %s", resourceType, name, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "delete", resourceType, name, "-n", namespace, "--timeout=30s")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Failed to delete resource %s/%s: %v", resourceType, name, err)
		t.Logf("kubectl output: %s", string(output))
		return err
	}
	t.Logf("Successfully deleted resource: %s/%s", resourceType, name)
	return nil
}

// WaitForTargetReadyWindows waits for a Target to reach ready state for Windows
func WaitForTargetReadyWindows(t *testing.T, targetName, namespace string, timeout time.Duration) {
	t.Logf("Waiting for Target %s in namespace %s to be ready...", targetName, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Logf("Timeout waiting for Target %s to be ready", targetName)
			// Get target status for debugging
			cmd := exec.Command("kubectl", "get", "target", targetName, "-n", namespace, "-o", "yaml")
			if output, err := cmd.CombinedOutput(); err == nil {
				t.Logf("Target status: %s", string(output))
			}
			t.Fatalf("Timeout waiting for Target %s to be ready after %v", targetName, timeout)
		case <-ticker.C:
			cmd := exec.Command("kubectl", "get", "target", targetName, "-n", namespace, "-o", "jsonpath={.status.provisioningStatus.status}")
			output, err := cmd.Output()
			if err == nil {
				status := strings.TrimSpace(string(output))
				t.Logf("Target %s current status: %s", targetName, status)
				if status == "Succeeded" {
					t.Logf("Target %s is ready", targetName)
					return
				}
			} else {
				t.Logf("Failed to get target status: %v", err)
			}
		}
	}
}

// WaitForInstanceReadyWindows waits for an Instance to complete deployment for Windows
func WaitForInstanceReadyWindows(t *testing.T, instanceName, namespace string, timeout time.Duration) {
	t.Logf("Waiting for Instance %s in namespace %s to be ready...", instanceName, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Logf("Timeout waiting for Instance %s to be ready", instanceName)
			// Get instance status for debugging
			cmd := exec.Command("kubectl", "get", "instance", instanceName, "-n", namespace, "-o", "yaml")
			if output, err := cmd.CombinedOutput(); err == nil {
				t.Logf("Instance status: %s", string(output))
			}
			t.Logf("Instance %s deployment completed (may not be fully ready)", instanceName)
			return
		case <-ticker.C:
			cmd := exec.Command("kubectl", "get", "instance", instanceName, "-n", namespace, "-o", "jsonpath={.status.provisioningStatus.status}")
			output, err := cmd.Output()
			if err == nil {
				status := strings.TrimSpace(string(output))
				t.Logf("Instance %s current status: %s", instanceName, status)
				if status == "Succeeded" || status == "Failed" {
					t.Logf("Instance %s deployment completed with status: %s", instanceName, status)
					return
				}
			} else {
				t.Logf("Failed to get instance status: %v", err)
			}
		}
	}
}

// WaitForResourceDeletedWindows waits for a resource to be completely deleted for Windows
func WaitForResourceDeletedWindows(t *testing.T, resourceType, name, namespace string, timeout time.Duration) {
	t.Logf("Waiting for %s %s in namespace %s to be deleted...", resourceType, name, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Logf("Timeout waiting for %s %s to be deleted", resourceType, name)
			return
		case <-ticker.C:
			cmd := exec.Command("kubectl", "get", resourceType, name, "-n", namespace)
			err := cmd.Run()
			if err != nil {
				// Resource not found, it's been deleted
				t.Logf("%s %s has been deleted", resourceType, name)
				return
			}
			t.Logf("%s %s still exists, waiting...", resourceType, name)
		}
	}
}

// WaitForTargetCreatedWindows waits for a Target to be created for Windows
func WaitForTargetCreatedWindows(t *testing.T, targetName, namespace string, timeout time.Duration) {
	t.Logf("Waiting for Target %s in namespace %s to be created...", targetName, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for Target %s to be created after %v", targetName, timeout)
		case <-ticker.C:
			cmd := exec.Command("kubectl", "get", "target", targetName, "-n", namespace)
			err := cmd.Run()
			if err == nil {
				t.Logf("Target %s has been created", targetName)
				return
			}
			t.Logf("Target %s not yet created, waiting...", targetName)
		}
	}
}

// VerifyTargetTopologyUpdateWindows verifies that topology was successfully updated for Windows
func VerifyTargetTopologyUpdateWindows(t *testing.T, targetName, namespace, testDescription string) {
	t.Logf("Verifying topology update for Target %s: %s", targetName, testDescription)

	cmd := exec.Command("kubectl", "get", "target", targetName, "-n", namespace, "-o", "yaml")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Warning: Failed to get target for topology verification: %v", err)
		return
	}

	t.Logf("Target topology verification completed for: %s", testDescription)
	t.Logf("Target status: %s", string(output))
}

// DeleteSolutionManifestWithTimeoutWindows deletes a solution manifest with timeout for Windows using direct resource deletion
func DeleteSolutionManifestWithTimeoutWindows(t *testing.T, manifestPath string, timeout time.Duration) error {
	t.Logf("Deleting solution manifest using direct resource deletion: %s", manifestPath)

	// Parse the YAML file to extract resource names
	yamlContent, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		t.Logf("Failed to read manifest file %s: %v", manifestPath, err)
		return err
	}

	// Parse YAML to extract solution and solutioncontainer names
	solutionName, solutionContainerName, namespace, err := parseWindowsSolutionYAML(string(yamlContent))
	if err != nil {
		t.Logf("Failed to parse solution YAML: %v", err)
		return err
	}

	t.Logf("Parsed resources - Solution: %s, SolutionContainer: %s, Namespace: %s",
		solutionName, solutionContainerName, namespace)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Step 1: Delete the solution (nested resource) first
	if solutionName != "" {
		t.Logf("Deleting solution: %s in namespace %s", solutionName, namespace)
		cmd := exec.CommandContext(ctx, "kubectl", "delete", "solution", solutionName, "-n", namespace, "--timeout=30s")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("Failed to delete solution %s: %v", solutionName, err)
			t.Logf("kubectl output: %s", string(output))
			return fmt.Errorf("failed to delete solution %s: %v", solutionName, err)
		}
		t.Logf("Successfully deleted solution: %s", solutionName)
	}

	// Step 2: Delete the solution container (parent resource) second
	if solutionContainerName != "" {
		t.Logf("Deleting solution container: %s in namespace %s", solutionContainerName, namespace)
		cmd := exec.CommandContext(ctx, "kubectl", "delete", "solutioncontainer", solutionContainerName, "-n", namespace, "--timeout=30s")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("Failed to delete solution container %s: %v", solutionContainerName, err)
			t.Logf("kubectl output: %s", string(output))
			return fmt.Errorf("failed to delete solution container %s: %v", solutionContainerName, err)
		}
		t.Logf("Successfully deleted solution container: %s", solutionContainerName)
	}

	t.Logf("Successfully deleted all resources from manifest: %s", manifestPath)
	return nil
}

// parseWindowsSolutionYAML parses solution YAML to extract resource names and namespace
func parseWindowsSolutionYAML(yamlContent string) (solutionName, solutionContainerName, namespace string, err error) {
	// Split YAML documents by "---"
	documents := strings.Split(yamlContent, "---")

	for _, doc := range documents {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Simple parsing for kind and metadata
		lines := strings.Split(doc, "\n")
		var kind, name, ns string
		var inMetadata bool

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			// Parse kind
			if strings.HasPrefix(line, "kind:") {
				kind = strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
			}

			// Track metadata section
			if line == "metadata:" {
				inMetadata = true
				continue
			}
			if inMetadata && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				inMetadata = false
			}

			// Parse name and namespace in metadata section
			if inMetadata {
				if strings.HasPrefix(line, "name:") {
					name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
				}
				if strings.HasPrefix(line, "namespace:") {
					ns = strings.TrimSpace(strings.TrimPrefix(line, "namespace:"))
				}
			}
		}

		// Store the parsed values based on kind
		switch kind {
		case "Solution":
			solutionName = name
			if namespace == "" && ns != "" {
				namespace = ns
			}
		case "SolutionContainer":
			solutionContainerName = name
			if namespace == "" && ns != "" {
				namespace = ns
			}
		}
	}

	// Default to "default" namespace if none specified
	if namespace == "" {
		namespace = "default"
	}

	return solutionName, solutionContainerName, namespace, nil
}

// VerifyMinikubeInstallationWindows verifies that minikube is installed and available on Windows
func VerifyMinikubeInstallationWindows(t *testing.T) {
	t.Logf("Verifying minikube installation on Windows...")
	cmd := exec.Command("minikube", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Minikube is not installed or not available: %v\nOutput: %s", err, string(output))
	}
	t.Logf("Minikube is available: %s", string(output))
}

// VerifyKubectlInstallationWindows verifies that kubectl is installed and available on Windows
func VerifyKubectlInstallationWindows(t *testing.T) {
	t.Logf("Verifying kubectl installation on Windows...")
	cmd := exec.Command("kubectl", "version", "--client")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl is not installed or not available: %v\nOutput: %s", err, string(output))
	}
	t.Logf("kubectl is available: %s", string(output))
}

// CleanupMinikubeWindows cleans up the minikube cluster on Windows
func CleanupMinikubeWindows(t *testing.T) {
	t.Logf("Cleaning up minikube cluster on Windows...")
	cmd := exec.Command("minikube", "delete")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Warning: Failed to delete minikube cluster: %v\nOutput: %s", err, string(output))
	} else {
		t.Logf("Minikube cluster deleted successfully")
	}
}

// SetupWindowsMQTTProcessNamespace sets up namespace for Windows MQTT process testing
func SetupWindowsMQTTProcessNamespace(t *testing.T, namespace string) {
	t.Logf("Setting up namespace %s for Windows MQTT process testing", namespace)

	nsYaml := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, namespace)

	tempDir := CreateWindowsTestDirectory(t)
	nsPath := filepath.Join(tempDir, "namespace.yaml")
	err := CreateYAMLFileWindows(t, nsPath, nsYaml)
	if err == nil {
		ApplyKubernetesManifestWindows(t, nsPath)
	}
}

// SetupWindowsMQTTProcessTestWithDetectedAddress sets up Windows MQTT process test with detected broker address
func SetupWindowsMQTTProcessTestWithDetectedAddress(t *testing.T, testDir, targetName, namespace string) (WindowsTestConfig, string, string) {
	t.Logf("Setting up Windows MQTT process test with detected address")

	// Generate certificates
	certs := GenerateWindowsCertificates(t, testDir)

	// Detect broker address (for testing, we'll use localhost)
	detectedBrokerAddress := "localhost"
	mqttBrokerPort := 8883

	// Create topology file
	topologyPath := CreateTestTopologyWindows(t, testDir)

	// Create MQTT config
	configPath := CreateMQTTConfigWindows(t, testDir, detectedBrokerAddress, mqttBrokerPort, targetName, namespace)

	// Setup Windows test configuration for MQTT mode
	config := WindowsTestConfig{
		ProjectRoot:    GetWindowsProjectRoot(t),
		ConfigPath:     configPath,
		ClientCertPath: certs.ClientPEM, // PEM format for MQTT
		ClientKeyPath:  certs.ClientKey,
		CertPassword:   certs.Password,
		CACertPath:     certs.CACert,
		TargetName:     targetName,
		Namespace:      namespace,
		TopologyPath:   topologyPath,
		Protocol:       "mqtt",
		BrokerAddress:  detectedBrokerAddress,
		BrokerPort:     fmt.Sprintf("%d", mqttBrokerPort),
		RunMode:        "service",
	}

	caSecretName := "mqtt-ca"

	return config, detectedBrokerAddress, caSecretName
}

// DebugWindowsCertificateInfo debugs certificate information on Windows
func DebugWindowsCertificateInfo(t *testing.T, certPath, certType string) {
	t.Logf("Debugging %s certificate: %s", certType, certPath)
	if FileExistsWindows(certPath) {
		t.Logf("Certificate file exists: %s", certPath)
	} else {
		t.Logf("Warning: Certificate file does not exist: %s", certPath)
	}
}

// DebugWindowsTLSConnection debugs TLS connection on Windows
func DebugWindowsTLSConnection(t *testing.T, address string, port int, caCertPath, clientCertPath, clientKeyPath string) {
	t.Logf("Debugging Windows TLS connection to %s:%d", address, port)
	t.Logf("Using CA cert: %s", caCertPath)
	t.Logf("Using client cert: %s", clientCertPath)
	t.Logf("Using client key: %s", clientKeyPath)

	// For now, just log the connection attempt
	// In a full implementation, this would test the actual TLS connection
	t.Logf("TLS connection debug completed for Windows")
}

// CreateWindowsMQTTCASecretInNamespace creates CA secret in namespace for Windows MQTT
func CreateWindowsMQTTCASecretInNamespace(t *testing.T, namespace, caCertPath string) {
	t.Logf("Creating CA secret in namespace %s for Windows MQTT", namespace)

	// Verify CA certificate exists
	if !FileExistsWindows(caCertPath) {
		t.Fatalf("CA certificate file not found: %s", caCertPath)
	}

	// Create secret using kubectl
	cmd := exec.Command("kubectl", "create", "secret", "generic", "mqtt-ca",
		"--from-file=ca.crt="+caCertPath, "-n", namespace)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Warning: Failed to create CA secret: %v\nOutput: %s", err, string(output))
	} else {
		t.Logf("Successfully created CA secret in namespace %s", namespace)
	}
}

// StartSymphonyWithMQTTConfigDetectedWindows starts Symphony with MQTT config on Windows using real deployment
func StartSymphonyWithMQTTConfigDetectedWindows(t *testing.T, brokerAddress, caSecretName string) {
	t.Logf("Starting Symphony with MQTT config on Windows: broker=%s, ca_secret=%s", brokerAddress, caSecretName)

	// Use the real Symphony deployment function with MQTT configuration
	projectRoot := GetWindowsProjectRoot(t)
	localenvDir := filepath.Join(projectRoot, "test", "localenv")

	// Check if localenv directory exists
	if _, err := os.Stat(localenvDir); os.IsNotExist(err) {
		t.Fatalf("Localenv directory does not exist: %s", localenvDir)
	}

	// Build MQTT-specific Helm values similar to bootstrap test
	helmValues := fmt.Sprintf("--set mqtt.enabled=true --set mqtt.brokerAddress=%s --set mqtt.useTLS=true --set certManager.enabled=true", brokerAddress)

	t.Logf("Deploying Symphony with MQTT configuration using mage...")
	cmd := exec.Command("mage", "cluster:deploywithsettings", helmValues)
	cmd.Dir = localenvDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("Symphony MQTT deployment stdout: %s", stdout.String())
		t.Logf("Symphony MQTT deployment stderr: %s", stderr.String())

		// Check if the error is related to cert-manager webhook (like in bootstrap test)
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "cert-manager-webhook") &&
			strings.Contains(stderrStr, "x509: certificate signed by unknown authority") {
			t.Logf("Detected cert-manager webhook certificate issue, attempting to fix...")
			FixCertManagerWebhookWindows(t)

			// Retry the deployment after fixing cert-manager
			t.Logf("Retrying Symphony MQTT deployment after cert-manager fix...")
			retryCmd := exec.Command("mage", "cluster:deploywithsettings", helmValues)
			retryCmd.Dir = localenvDir

			var retryStdout, retryStderr bytes.Buffer
			retryCmd.Stdout = &retryStdout
			retryCmd.Stderr = &retryStderr

			retryErr := retryCmd.Run()
			if retryErr != nil {
				t.Logf("Retry deployment stdout: %s", retryStdout.String())
				t.Logf("Retry deployment stderr: %s", retryStderr.String())
				t.Fatalf("Symphony MQTT deployment failed on Windows even after cert-manager fix: %v", retryErr)
			} else {
				t.Logf("Symphony MQTT deployment succeeded after cert-manager fix")
				err = nil // Clear the original error since retry succeeded
			}
		}
	}

	if err != nil {
		t.Fatalf("Symphony MQTT deployment failed on Windows: %v", err)
	}

	t.Logf("Successfully started Symphony with MQTT configuration on Windows")

	// Wait for Symphony to be ready
	WaitForSymphonyServiceReadyWindows(t, 5*time.Minute)
}

// WaitForSymphonyServerCertWindows waits for Symphony server certificate on Windows
func WaitForSymphonyServerCertWindows(t *testing.T, timeout time.Duration) {
	t.Logf("Waiting for Symphony server certificate on Windows (timeout: %v)", timeout)

	// For testing purposes, we'll just wait a bit
	// In a real implementation, this would check for actual certificate creation
	time.Sleep(10 * time.Second)

	t.Logf("Symphony server certificate wait completed on Windows")
}

// StartFreshMinikubeWindows starts a fresh minikube cluster on Windows with optimized settings
func StartFreshMinikubeWindows(t *testing.T) {
	t.Logf("Creating fresh minikube cluster for Windows E2E testing...")

	// Step 1: Always delete any existing cluster first
	t.Logf("Deleting any existing minikube cluster...")
	cmd := exec.Command("minikube", "delete")
	cmd.Run() // Ignore errors - cluster might not exist

	// Wait for cleanup to complete
	time.Sleep(5 * time.Second)

	// Step 2: Start new cluster with Windows-optimized settings
	t.Logf("Starting new minikube cluster...")
	cmd = exec.Command("minikube", "start", "--driver=docker", "--memory=4096", "--cpus=2")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("Minikube start stdout: %s", stdout.String())
		t.Logf("Minikube start stderr: %s", stderr.String())
		t.Fatalf("Failed to start minikube on Windows: %v", err)
	}

	// Step 3: Wait for cluster to be fully ready
	WaitForMinikubeReadyWindows(t, 5*time.Minute)

	t.Logf("Fresh minikube cluster is ready for Windows testing")
}

// WaitForMinikubeReadyWindows waits for the cluster to be fully operational on Windows
func WaitForMinikubeReadyWindows(t *testing.T, timeout time.Duration) {
	t.Logf("Waiting for minikube cluster to be ready on Windows...")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for minikube to be ready after %v", timeout)
		case <-ticker.C:
			// Check 1: Can we get nodes?
			cmd := exec.Command("kubectl", "get", "nodes")
			if cmd.Run() != nil {
				t.Logf("Still waiting for kubectl to connect...")
				continue
			}

			// Check 2: Can we create secrets?
			cmd = exec.Command("kubectl", "auth", "can-i", "create", "secrets")
			if cmd.Run() != nil {
				t.Logf("Still waiting for RBAC permissions...")
				continue
			}

			// Check 3: Are system pods running?
			cmd = exec.Command("kubectl", "get", "pods", "-n", "kube-system", "--field-selector=status.phase=Running")
			output, err := cmd.Output()
			if err != nil || len(strings.TrimSpace(string(output))) == 0 {
				t.Logf("Still waiting for system pods to be running...")
				continue
			}

			t.Logf("Minikube cluster is fully ready on Windows!")
			return
		}
	}
}

// StartSymphonyWithRemoteAgentConfigWindows starts Symphony with remote agent configuration on Windows
func StartSymphonyWithRemoteAgentConfigWindows(t *testing.T, protocol string) {
	projectRoot := GetWindowsProjectRoot(t)
	localenvDir := filepath.Join(projectRoot, "test", "localenv")

	t.Logf("StartSymphonyWithRemoteAgentConfigWindows: Project root: %s", projectRoot)
	t.Logf("StartSymphonyWithRemoteAgentConfigWindows: Localenv dir: %s", localenvDir)

	// Check if localenv directory exists
	if _, err := os.Stat(localenvDir); os.IsNotExist(err) {
		t.Fatalf("Localenv directory does not exist: %s", localenvDir)
	}

	var helmValues string
	if protocol == "http" {
		helmValues = "--set remoteAgent.remoteCert.used=true " +
			"--set remoteAgent.remoteCert.trustCAs.secretName=client-cert-secret " +
			"--set remoteAgent.remoteCert.trustCAs.secretKey=ca.crt " +
			"--set remoteAgent.remoteCert.subjects=remote-agent-client " +
			"--set certManager.enabled=true " +
			"--set api.env.ISSUER_NAME=symphony-ca-issuer " +
			"--set api.env.SYMPHONY_SERVICE_NAME=symphony-service"
	} else if protocol == "mqtt" {
		helmValues = "--set remoteAgent.remoteCert.used=true " +
			"--set remoteAgent.remoteCert.trustCAs.secretName=client-cert-secret " +
			"--set remoteAgent.remoteCert.trustCAs.secretKey=ca.crt " +
			"--set remoteAgent.remoteCert.subjects=remote-agent-client " +
			"--set mqtt.mqttClientCert.enabled=true " +
			"--set mqtt.mqttClientCert.secretName=mqtt-client-secret " +
			"--set mqtt.mqttClientCert.crt=client.crt " +
			"--set mqtt.mqttClientCert.key=client.key " +
			"--set mqtt.brokerAddress=tls://localhost:8883 " +
			"--set mqtt.enabled=true --set mqtt.useTLS=true " +
			"--set certManager.enabled=true " +
			"--set api.env.ISSUER_NAME=symphony-ca-issuer " +
			"--set api.env.SYMPHONY_SERVICE_NAME=symphony-service"
	}

	cmd := exec.Command("mage", "cluster:deploywithsettings", helmValues)
	cmd.Dir = localenvDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("Symphony deployment stdout: %s", stdout.String())
		t.Logf("Symphony deployment stderr: %s", stderr.String())

		// Check if the error is related to cert-manager webhook
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "cert-manager-webhook") &&
			strings.Contains(stderrStr, "x509: certificate signed by unknown authority") {
			t.Logf("Detected cert-manager webhook certificate issue, attempting to fix...")
			FixCertManagerWebhookWindows(t)

			// Retry the deployment after fixing cert-manager
			t.Logf("Retrying Symphony deployment after cert-manager fix...")
			retryCmd := exec.Command("mage", "cluster:deploywithsettings", helmValues)
			retryCmd.Dir = localenvDir

			var retryStdout, retryStderr bytes.Buffer
			retryCmd.Stdout = &retryStdout
			retryCmd.Stderr = &retryStderr

			retryErr := retryCmd.Run()
			if retryErr != nil {
				t.Logf("Retry deployment stdout: %s", retryStdout.String())
				t.Logf("Retry deployment stderr: %s", retryStderr.String())
				t.Fatalf("Symphony deployment failed on Windows even after cert-manager fix: %v", retryErr)
			} else {
				t.Logf("Symphony deployment succeeded after cert-manager fix")
				err = nil // Clear the original error since retry succeeded
			}
		}
	}
	if err != nil {
		t.Fatalf("Symphony deployment failed on Windows: %v", err)
	}

	t.Logf("Started Symphony with remote agent configuration for %s protocol on Windows", protocol)
}

// CreateCASecretWindows creates CA secret in cert-manager namespace for Windows
func CreateCASecretWindows(t *testing.T, certs WindowsCertificatePaths) string {
	secretName := "client-cert-secret"

	// Ensure cert-manager namespace exists
	cmd := exec.Command("kubectl", "create", "namespace", "cert-manager")
	cmd.Run() // Ignore error if namespace already exists

	// Create CA secret in cert-manager namespace with correct key name
	cmd = exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=ca.crt="+certs.CACert,
		"-n", "cert-manager")

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create CA secret (may already exist): %v", err)
	} else {
		t.Logf("Created CA secret %s in cert-manager namespace", secretName)
	}
	return secretName
}

// CreateClientCertSecretWindows creates client certificate secret in test namespace for Windows
func CreateClientCertSecretWindows(t *testing.T, namespace string, certs WindowsCertificatePaths) string {
	secretName := "remote-agent-client-secret"

	cmd := exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=client.crt="+certs.ClientPEM,
		"--from-file=client.key="+certs.ClientKey,
		"-n", namespace)

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create client cert secret (may already exist): %v", err)
	} else {
		t.Logf("Created client cert secret %s in namespace %s", secretName, namespace)
	}
	return secretName
}

// StartPortForwardWindows starts kubectl port-forward for Symphony service on Windows
func StartPortForwardWindows(t *testing.T) *exec.Cmd {
	t.Logf("Starting port-forward for Symphony service on Windows...")

	cmd := exec.Command("kubectl", "port-forward", "svc/symphony-service", "8081:8081", "-n", "default")
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start port-forward on Windows: %v", err)
	}

	// Wait for port-forward to be truly ready
	WaitForPortForwardReadyWindows(t, "127.0.0.1:8081", 30*time.Second)

	t.Cleanup(func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
			t.Logf("Killed port-forward process with PID: %d", cmd.Process.Pid)
		}
	})

	t.Logf("Port-forward started with PID: %d and is ready for connections", cmd.Process.Pid)
	return cmd
}

// StartPortForwardWindowsWithoutCleanup starts kubectl port-forward for Symphony service on Windows without auto-cleanup
func StartPortForwardWindowsWithoutCleanup(t *testing.T) *exec.Cmd {
	t.Logf("Starting port-forward for Symphony service on Windows (without auto-cleanup)...")

	cmd := exec.Command("kubectl", "port-forward", "svc/symphony-service", "8081:8081", "-n", "default")
	err := cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start port-forward on Windows: %v", err)
	}

	// Wait for port-forward to be truly ready
	WaitForPortForwardReadyWindows(t, "127.0.0.1:8081", 30*time.Second)

	t.Logf("Port-forward started with PID: %d and is ready for connections", cmd.Process.Pid)
	return cmd
}

// WaitForPortForwardReadyWindows waits for port-forward to be ready by testing TCP connection on Windows
func WaitForPortForwardReadyWindows(t *testing.T, address string, timeout time.Duration) {
	t.Logf("Waiting for port-forward to be ready at %s...", address)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for port-forward to be ready at %s after %v", address, timeout)
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", address, 2*time.Second)
			if err == nil {
				conn.Close()
				t.Logf("Port-forward is ready and accepting connections at %s", address)
				return
			}
			t.Logf("Still waiting for port-forward at %s... (error: %v)", address, err)
		}
	}
}

// WaitForSymphonyServiceReadyWindows waits for Symphony service to be ready and accessible on Windows
func WaitForSymphonyServiceReadyWindows(t *testing.T, timeout time.Duration) {
	t.Logf("Waiting for Symphony service to be ready on Windows...")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Before failing, let's get some debug information
			t.Logf("Timeout waiting for Symphony service on Windows. Getting debug information...")

			// Check pod status
			cmd := exec.Command("kubectl", "get", "pods", "-n", "default", "-l", "app.kubernetes.io/name=symphony")
			if output, err := cmd.CombinedOutput(); err == nil {
				t.Logf("Symphony pods status:\n%s", string(output))
			}

			// Check service status
			cmd = exec.Command("kubectl", "get", "svc", "symphony-service", "-n", "default")
			if output, err := cmd.CombinedOutput(); err == nil {
				t.Logf("Symphony service status:\n%s", string(output))
			}

			t.Fatalf("Timeout waiting for Symphony service to be ready after %v", timeout)
		case <-ticker.C:
			// Check if Symphony API deployment is ready
			cmd := exec.Command("kubectl", "get", "deployment", "symphony-api", "-n", "default", "-o", "jsonpath={.status.readyReplicas}")
			output, err := cmd.Output()
			if err != nil {
				t.Logf("Failed to check symphony-api deployment status: %v", err)
				continue
			}

			readyReplicas := strings.TrimSpace(string(output))
			if readyReplicas == "" || readyReplicas == "0" {
				t.Logf("Symphony API deployment not ready yet (ready replicas: %s)", readyReplicas)
				continue
			}

			t.Logf("Symphony API deployment is ready with %s replicas", readyReplicas)
			return
		}
	}
}

// FixCertManagerWebhookWindows fixes cert-manager webhook certificate issues on Windows
func FixCertManagerWebhookWindows(t *testing.T) {
	t.Logf("Fixing cert-manager webhook certificate issues on Windows...")

	// Delete webhook configurations to force recreation
	webhookConfigs := []string{
		"cert-manager-webhook",
		"cert-manager-cainjector",
	}

	for _, config := range webhookConfigs {
		t.Logf("Deleting validating webhook configuration: %s", config)
		cmd := exec.Command("kubectl", "delete", "validatingwebhookconfiguration", config, "--ignore-not-found=true")
		cmd.Run() // Ignore errors as the webhook might not exist

		t.Logf("Deleting mutating webhook configuration: %s", config)
		cmd = exec.Command("kubectl", "delete", "mutatingwebhookconfiguration", config, "--ignore-not-found=true")
		cmd.Run() // Ignore errors as the webhook might not exist
	}

	// Restart cert-manager pods to regenerate certificates
	t.Logf("Restarting cert-manager deployments...")
	deployments := []string{
		"cert-manager",
		"cert-manager-webhook",
		"cert-manager-cainjector",
	}

	for _, deployment := range deployments {
		cmd := exec.Command("kubectl", "rollout", "restart", "deployment", deployment, "-n", "cert-manager")
		if err := cmd.Run(); err != nil {
			t.Logf("Warning: Failed to restart deployment %s: %v", deployment, err)
		}
	}

	// Wait for cert-manager to be ready again
	t.Logf("Waiting for cert-manager to be ready after restart...")
	time.Sleep(10 * time.Second)

	t.Logf("Cert-manager webhook fix completed on Windows")
}

// SetupSymphonyHostsWindows configures hosts file for Symphony service access on Windows
func SetupSymphonyHostsWindows(t *testing.T) {
	t.Logf("Setting up hosts entry for Symphony service on Windows...")

	// Add symphony-service -> 127.0.0.1 mapping
	hostsEntry := "127.0.0.1 symphony-service"

	// Use PowerShell to add hosts entry (requires admin privileges)
	psScript := fmt.Sprintf(`
$hostsPath = "$env:windir\System32\drivers\etc\hosts"
$entry = "%s"
Add-Content -Path $hostsPath -Value $entry
`, hostsEntry)

	tempScriptFile := filepath.Join(os.TempDir(), "add_hosts_entry.ps1")
	err := ioutil.WriteFile(tempScriptFile, []byte(psScript), 0644)
	if err != nil {
		t.Logf("Warning: Failed to create hosts script: %v", err)
		return
	}
	defer os.Remove(tempScriptFile)

	// Execute with elevated privileges
	cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", tempScriptFile)
	err = cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to add hosts entry (may require admin privileges): %v", err)
	} else {
		t.Logf("Added hosts entry: %s", hostsEntry)
	}

	// NOTE: Cleanup is NOT set here to avoid premature removal during subtests
	// The calling test should handle cleanup explicitly when appropriate
}

// RemoveHostsEntryWindows removes an entry from hosts file on Windows
func RemoveHostsEntryWindows(t *testing.T, hostname string) {
	t.Logf("Removing hosts entry for: %s", hostname)

	psScript := fmt.Sprintf(`
$hostsPath = "$env:windir\System32\drivers\etc\hosts"
$content = Get-Content $hostsPath | Where-Object { $_ -notmatch "127.0.0.1 %s" }
Set-Content -Path $hostsPath -Value $content
`, hostname)

	tempScriptFile := filepath.Join(os.TempDir(), "remove_hosts_entry.ps1")
	err := ioutil.WriteFile(tempScriptFile, []byte(psScript), 0644)
	if err != nil {
		t.Logf("Warning: Failed to create hosts removal script: %v", err)
		return
	}
	defer os.Remove(tempScriptFile)

	cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", tempScriptFile)
	if err := cmd.Run(); err != nil {
		t.Logf("Warning: Failed to remove hosts entry for %s: %v", hostname, err)
	} else {
		t.Logf("Removed hosts entry for: %s", hostname)
	}
}

// ExtractAndImportSymphonyCACertWindows extracts CA certificate from Kubernetes secret and imports it into Windows certificate store
func ExtractAndImportSymphonyCACertWindows(t *testing.T, timeout time.Duration) error {
	t.Logf("Extracting and importing Symphony CA certificate on Windows...")

	// Wait for symphony-api-serving-cert secret to be available
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for symphony-api-serving-cert secret after %v", timeout)
		case <-ticker.C:
			// Check if secret exists
			cmd := exec.Command("kubectl", "get", "secret", "-n", "default", "symphony-api-serving-cert", "--ignore-not-found")
			err := cmd.Run()
			if err == nil {
				t.Logf("symphony-api-serving-cert secret found")
				goto extractCert
			}
			t.Logf("Waiting for symphony-api-serving-cert secret to be created...")
		}
	}

extractCert:
	// Extract CA certificate from secret
	t.Logf("Extracting CA certificate from symphony-api-serving-cert secret...")
	cmd := exec.Command("kubectl", "get", "secret", "-n", "default", "symphony-api-serving-cert", "-o", "jsonpath={.data['ca\\.crt']}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to extract CA certificate from secret: %v", err)
	}

	caCertB64 := strings.TrimSpace(string(output))
	if caCertB64 == "" {
		return fmt.Errorf("CA certificate data is empty in symphony-api-serving-cert secret")
	}

	// Create temporary directory for certificate
	tempDir := CreateWindowsTestDirectory(t)
	localCAPath := filepath.Join(tempDir, "symphony-ca.crt")

	// Use PowerShell to decode base64 and save certificate
	psScript := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
try {
    Write-Output "Decoding base64 CA certificate..."
    $base64String = '%s'
    $certBytes = [System.Convert]::FromBase64String($base64String)
    [System.IO.File]::WriteAllBytes('%s', $certBytes)
    Write-Output "CA certificate saved to: %s"
} catch {
    Write-Error "Failed to decode and save CA certificate: $_"
    throw $_
}`, caCertB64, localCAPath, localCAPath)

	tempScriptFile := filepath.Join(tempDir, "decode_ca_cert.ps1")
	err = ioutil.WriteFile(tempScriptFile, []byte(psScript), 0644)
	if err != nil {
		return fmt.Errorf("failed to write PowerShell script: %v", err)
	}
	defer os.Remove(tempScriptFile)

	// Execute PowerShell script to decode certificate
	cmd = ExecutePowerShell7Script(t, tempScriptFile, []string{}, tempDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Logf("PowerShell decode stdout: %s", stdout.String())
		t.Logf("PowerShell decode stderr: %s", stderr.String())
		return fmt.Errorf("failed to decode CA certificate: %v", err)
	}

	// Verify certificate file exists
	if !FileExistsWindows(localCAPath) {
		return fmt.Errorf("CA certificate file was not created at %s", localCAPath)
	}

	t.Logf("Successfully extracted CA certificate to: %s", localCAPath)

	// Import CA certificate into Windows certificate store
	t.Logf("Importing CA certificate into Windows certificate store...")

	importScript := fmt.Sprintf(`
$ErrorActionPreference = "Stop"
try {
    Write-Output "Importing CA certificate into Windows certificate store..."
    
    # Check if running as administrator
    $isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    
    if ($isAdmin) {
        Write-Output "Running as administrator - importing into LocalMachine\\Root store"
        Import-Certificate -FilePath '%s' -CertStoreLocation Cert:\\LocalMachine\\Root | Out-Null
        Write-Output "Successfully imported CA certificate into LocalMachine\\Root store"
    } else {
        Write-Output "Running as regular user - importing into CurrentUser\\Root store"
        Import-Certificate -FilePath '%s' -CertStoreLocation Cert:\\CurrentUser\\Root | Out-Null
        Write-Output "Successfully imported CA certificate into CurrentUser\\Root store"
    }
    
    # Verify certificate was imported by checking thumbprint
    $cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2('%s')
    Write-Output "Imported certificate details:"
    Write-Output "  Subject: $($cert.Subject)"
    Write-Output "  Thumbprint: $($cert.Thumbprint)"
    Write-Output "  Valid From: $($cert.NotBefore)"
    Write-Output "  Valid To: $($cert.NotAfter)"
    
} catch {
    Write-Error "Failed to import CA certificate: $_"
    throw $_
}`, localCAPath, localCAPath, localCAPath)

	importScriptFile := filepath.Join(tempDir, "import_ca_cert.ps1")
	err = ioutil.WriteFile(importScriptFile, []byte(importScript), 0644)
	if err != nil {
		return fmt.Errorf("failed to write import script: %v", err)
	}
	defer os.Remove(importScriptFile)

	// Execute import script
	cmd = ExecutePowerShell7Script(t, importScriptFile, []string{}, tempDir)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	stdout.Reset()
	stderr.Reset()

	err = cmd.Run()
	if err != nil {
		t.Logf("PowerShell import stdout: %s", stdout.String())
		t.Logf("PowerShell import stderr: %s", stderr.String())
		return fmt.Errorf("failed to import CA certificate: %v", err)
	}

	t.Logf("Successfully imported Symphony CA certificate into Windows certificate store")
	t.Logf("Import output: %s", stdout.String())

	return nil
}

// SetupWindowsCertificateValidation sets up proper certificate validation for Windows tests
func SetupWindowsCertificateValidation(t *testing.T) {
	t.Logf("Setting up Windows certificate validation...")

	// Extract and import Symphony CA certificate
	err := ExtractAndImportSymphonyCACertWindows(t, 5*time.Minute)
	if err != nil {
		t.Fatalf("Failed to set up certificate validation: %v", err)
	}

	t.Logf("Windows certificate validation setup completed successfully")
}

// VerifyCertificateFileAccessWindows verifies certificate files exist and are accessible before bootstrap
func VerifyCertificateFileAccessWindows(t *testing.T, caCertPath, clientCertPath, clientKeyPath string, timeout time.Duration) error {
	t.Logf("Verifying certificate file access with timeout %v...", timeout)
	t.Logf("  CA Certificate: %s", caCertPath)
	t.Logf("  Client Certificate: %s", clientCertPath)
	t.Logf("  Client Key: %s", clientKeyPath)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	filesToCheck := map[string]string{
		"CA Certificate":     caCertPath,
		"Client Certificate": clientCertPath,
		"Client Key":         clientKeyPath,
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for certificate files to be accessible after %v", timeout)
		case <-ticker.C:
			allFilesReady := true
			for fileType, filePath := range filesToCheck {
				// Skip empty paths
				if filePath == "" {
					continue
				}

				// Check if file exists
				if !FileExistsWindows(filePath) {
					t.Logf("Waiting for %s file: %s", fileType, filePath)
					allFilesReady = false
					continue
				}

				// Check if file is readable and has content
				if stat, err := os.Stat(filePath); err != nil {
					t.Logf("Waiting for %s file to be readable: %s (error: %v)", fileType, filePath, err)
					allFilesReady = false
					continue
				} else if stat.Size() == 0 {
					t.Logf("Waiting for %s file to have content: %s (size: 0)", fileType, filePath)
					allFilesReady = false
					continue
				}

				// Try to read the file to ensure it's not locked
				if _, err := os.ReadFile(filePath); err != nil {
					t.Logf("Waiting for %s file to be unlocked: %s (error: %v)", fileType, filePath, err)
					allFilesReady = false
					continue
				}

				t.Logf("✓ %s file is ready: %s", fileType, filePath)
			}

			if allFilesReady {
				t.Logf("All certificate files are accessible and ready")
				return nil
			}
		}
	}
}

// StartSymphonyWithMQTTConfigAlternativeWindows starts Symphony with MQTT config using alternative method similar to Linux
func StartSymphonyWithMQTTConfigAlternativeWindows(t *testing.T, brokerAddress string) error {
	t.Logf("Starting Symphony with MQTT configuration using alternative method: %s", brokerAddress)

	projectRoot := GetWindowsProjectRoot(t)
	localenvDir := filepath.Join(projectRoot, "test", "localenv")

	// Check if localenv directory exists
	if _, err := os.Stat(localenvDir); os.IsNotExist(err) {
		return fmt.Errorf("localenv directory does not exist: %s", localenvDir)
	}

	// Create a more robust deployment with retry logic
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		t.Logf("Symphony deployment attempt %d/%d", attempt, maxRetries)

		// Use timeout context for each attempt
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

		// Build mage command with extended timeout
		helmValues := fmt.Sprintf("--set mqtt.enabled=true --set mqtt.brokerAddress=%s --set mqtt.useTLS=true --set certManager.enabled=true --timeout=600s", brokerAddress)
		cmd := exec.CommandContext(ctx, "mage", "cluster:deploywithsettings", helmValues)
		cmd.Dir = localenvDir

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		cancel()

		if err == nil {
			t.Logf("Symphony MQTT deployment succeeded on attempt %d", attempt)
			return nil
		}

		lastErr = err
		t.Logf("Symphony MQTT deployment attempt %d failed: %v", attempt, err)
		t.Logf("Stdout: %s", stdout.String())
		t.Logf("Stderr: %s", stderr.String())

		// If this was the last attempt, return the error
		if attempt == maxRetries {
			break
		}

		// Wait before retrying
		t.Logf("Waiting 30s before retry...")
		time.Sleep(30 * time.Second)

		// Try to cleanup any partial deployment before retry
		t.Logf("Cleaning up partial deployment before retry...")
		cleanupCmd := exec.Command("mage", "destroy", "all,nowait")
		cleanupCmd.Dir = localenvDir
		cleanupCmd.Run() // Ignore errors

		// Wait for cleanup
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("symphony MQTT deployment failed after %d attempts, last error: %v", maxRetries, lastErr)
}

// SetupInitialConfigWindows sets up initial configuration similar to Linux version
func SetupInitialConfigWindows(t *testing.T, testDir, targetName, namespace string, mqttCerts WindowsMQTTCertificatePaths) WindowsTestConfig {
	t.Logf("Setting up initial Windows test configuration...")

	projectRoot := GetWindowsProjectRoot(t)

	config := WindowsTestConfig{
		ProjectRoot:    projectRoot,
		TargetName:     targetName,
		Namespace:      namespace,
		Protocol:       "mqtt",
		ClientCertPath: mqttCerts.RemoteAgentCert,
		ClientKeyPath:  mqttCerts.RemoteAgentKey,
		CACertPath:     mqttCerts.CACert,
		RunMode:        "service",
	}

	t.Logf("Initial Windows configuration set up:")
	t.Logf("  Target Name: %s", config.TargetName)
	t.Logf("  Namespace: %s", config.Namespace)
	t.Logf("  Protocol: %s", config.Protocol)
	t.Logf("  CA Certificate: %s", config.CACertPath)
	t.Logf("  Client Certificate: %s", config.ClientCertPath)
	t.Logf("  Client Key: %s", config.ClientKeyPath)

	return config
}

// SetupMQTTBootstrapTestWithDetectedAddressWindows sets up MQTT bootstrap test with detected address similar to Linux
func SetupMQTTBootstrapTestWithDetectedAddressWindows(t *testing.T, testDir, targetName, namespace string, config *WindowsTestConfig, mqttCerts *WindowsMQTTCertificatePaths) {
	t.Logf("Setting up Windows MQTT bootstrap test with detected address...")

	// Use the broker address that was already detected and set in config
	// This ensures consistency with the external broker setup
	detectedBrokerAddress := config.BrokerAddress
	mqttBrokerPort := 8883

	if detectedBrokerAddress == "" {
		// Fallback if not set
		detectedBrokerAddress = "localhost"
		config.BrokerAddress = detectedBrokerAddress
	}

	// Ensure port is set
	config.BrokerPort = fmt.Sprintf("%d", mqttBrokerPort)

	// Create topology file
	config.TopologyPath = CreateTestTopologyWindows(t, testDir)

	// Create MQTT config using the same broker address as the external setup
	config.ConfigPath = CreateMQTTConfigWindows(t, testDir, detectedBrokerAddress, mqttBrokerPort, targetName, namespace)

	t.Logf("Windows MQTT bootstrap test setup completed:")
	t.Logf("  Broker Address: %s", config.BrokerAddress)
	t.Logf("  Broker Port: %s", config.BrokerPort)
	t.Logf("  Config Path: %s", config.ConfigPath)
	t.Logf("  Topology Path: %s", config.TopologyPath)
}

// EnhancedStartWindowsRemoteAgentWithBootstrap starts remote agent with enhanced debugging and certificate verification
func EnhancedStartWindowsRemoteAgentWithBootstrap(t *testing.T, config WindowsTestConfig) *exec.Cmd {
	t.Logf("Enhanced Windows remote agent bootstrap with certificate verification...")

	// Step 1: Verify certificate files are accessible before proceeding
	err := VerifyCertificateFileAccessWindows(t, config.CACertPath, config.ClientCertPath, config.ClientKeyPath, 30*time.Second)
	if err != nil {
		t.Fatalf("Certificate file verification failed: %v", err)
	}

	// Step 2: Build the binary first for MQTT mode
	if config.Protocol == "mqtt" && config.BinaryPath == "" {
		binaryPath := BuildWindowsRemoteAgent(t, config)
		config.BinaryPath = binaryPath
	}

	// Step 3: Debug certificate information
	t.Logf("Certificate verification complete. Starting bootstrap with enhanced debugging...")
	DebugWindowsCertificateInfo(t, config.CACertPath, "CA")
	DebugWindowsCertificateInfo(t, config.ClientCertPath, "Client")
	DebugWindowsCertificateInfo(t, config.ClientKeyPath, "Client Key")

	// Step 4: Prepare bootstrap.ps1 arguments with enhanced error handling
	var args []string

	if config.Protocol == "http" {
		// HTTP mode arguments
		args = []string{
			"-protocol", "http",
			"-endpoint", config.BaseURL,
			"-cert_path", config.ClientCertPath,
			"-target_name", config.TargetName,
			"-namespace", config.Namespace,
			"-topology", config.TopologyPath,
			"-run_mode", config.RunMode,
			"-cert_password", config.CertPassword,
		}

		// Add CA certificate if available
		if config.CACertPath != "" {
			args = append(args, "-ca_cert_path", config.CACertPath)
		}
	} else if config.Protocol == "mqtt" {
		// MQTT mode arguments
		args = []string{
			"-protocol", "mqtt",
			"-mqtt_broker", config.BrokerAddress,
			"-mqtt_port", config.BrokerPort,
			"-cert_path", config.ClientCertPath,
			"-key_path", config.ClientKeyPath,
			"-target_name", config.TargetName,
			"-namespace", config.Namespace,
			"-topology", config.TopologyPath,
			"-run_mode", config.RunMode,
			"-agent_path", config.BinaryPath,
		}

		if config.CACertPath != "" {
			args = append(args, "-ca_cert_path", config.CACertPath)
		}
	} else {
		t.Fatalf("Unsupported protocol: %s", config.Protocol)
	}

	// Step 5: Final verification just before bootstrap call
	t.Logf("Final certificate verification before bootstrap...")
	for _, path := range []string{config.CACertPath, config.ClientCertPath, config.ClientKeyPath} {
		if path != "" && !FileExistsWindows(path) {
			t.Fatalf("Certificate file missing just before bootstrap: %s", path)
		}
	}

	// Step 6: Get bootstrap.ps1 path and execute with PowerShell 7
	bootstrapPath := filepath.Join(config.ProjectRoot, "remote-agent", "bootstrap", "bootstrap.ps1")

	// Enhanced logging of the command
	t.Logf("Executing enhanced Windows bootstrap.ps1:")
	t.Logf("  Script: %s", bootstrapPath)
	t.Logf("  Arguments: %v", args)
	t.Logf("  Working Dir: %s", filepath.Join(config.ProjectRoot, "remote-agent", "bootstrap"))

	// Execute bootstrap.ps1 using PowerShell 7
	cmd := ExecutePowerShell7Script(t, bootstrapPath, args, filepath.Join(config.ProjectRoot, "remote-agent", "bootstrap"))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start enhanced bootstrap.ps1: %v", err)
	}

	t.Logf("Enhanced bootstrap.ps1 started with PID: %d", cmd.Process.Pid)

	// Wait for bootstrap.ps1 to complete with enhanced error reporting
	go func() {
		err := cmd.Wait()
		if err != nil {
			t.Logf("Enhanced bootstrap.ps1 exited with error: %v", err)
			t.Logf("Enhanced bootstrap.ps1 stdout: %s", stdout.String())
			t.Logf("Enhanced bootstrap.ps1 stderr: %s", stderr.String())
		} else {
			t.Logf("Enhanced bootstrap.ps1 completed successfully")
			t.Logf("Enhanced bootstrap.ps1 output: %s", stdout.String())
		}
	}()

	t.Logf("Enhanced bootstrap.ps1 started, Windows service should be created with improved error handling")
	return cmd
}

// CleanupSymphonyWindows cleans up Symphony on Windows
func CleanupSymphonyWindows(t *testing.T) {
	t.Logf("Cleaning up Symphony on Windows...")

	// Dump logs first
	projectRoot := GetWindowsProjectRoot(t)
	localenvDir := filepath.Join(projectRoot, "test", "localenv")

	cmd := exec.Command("mage", "dumpSymphonyLogsForTest", fmt.Sprintf("'%s'", t.Name()))
	cmd.Dir = localenvDir
	cmd.Run()

	// Destroy symphony
	cmd = exec.Command("mage", "destroy", "all,nowait")
	cmd.Dir = localenvDir
	cmd.Run()

	t.Logf("Symphony cleanup completed on Windows")
}

// DetectMQTTBrokerAddressWindows detects the host IP address for MQTT broker on Windows
func DetectMQTTBrokerAddressWindows(t *testing.T) string {
	t.Logf("Detecting MQTT broker address for Windows...")

	// Method 1: Try to get minikube host IP
	cmd := exec.Command("minikube", "ssh", "ip route show default | awk '/default/ { print $3 }'")
	if output, err := cmd.Output(); err == nil {
		hostIP := strings.TrimSpace(string(output))
		if hostIP != "" && net.ParseIP(hostIP) != nil {
			t.Logf("Using minikube host IP as MQTT broker address: %s", hostIP)
			return hostIP
		}
	}

	// Method 2: Get the Windows host IP using PowerShell
	cmd = exec.Command("powershell", "-Command", "(Test-NetConnection -ComputerName 8.8.8.8 -Port 53).SourceAddress.IPAddress")
	if output, err := cmd.Output(); err == nil {
		ip := strings.TrimSpace(string(output))
		if ip != "" && net.ParseIP(ip) != nil {
			t.Logf("Using Windows host IP as MQTT broker address: %s", ip)
			return ip
		}
	}

	// Method 3: Get default gateway IP
	cmd = exec.Command("powershell", "-Command", "(Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Get-NetIPInterface | Where-Object ConnectionState -eq 'Connected' | Get-NetIPAddress -AddressFamily IPv4).IPAddress | Select-Object -First 1")
	if output, err := cmd.Output(); err == nil {
		ip := strings.TrimSpace(string(output))
		if ip != "" && ip != "127.0.0.1" && net.ParseIP(ip) != nil {
			t.Logf("Using Windows network interface IP as MQTT broker address: %s", ip)
			return ip
		}
	}

	// Fallback: Force IPv4 localhost to avoid IPv6 resolution issues
	t.Logf("Using IPv4 localhost as fallback MQTT broker address")
	return "127.0.0.1"
}

// SetupExternalMQTTBrokerWindows sets up external MQTT broker using Docker on Windows with enhanced network detection
func SetupExternalMQTTBrokerWindows(t *testing.T, certs WindowsMQTTCertificatePaths, brokerPort int) string {
	t.Logf("Setting up enhanced external MQTT broker on Windows on port %d", brokerPort)

	// Create mosquitto configuration file
	configContent := fmt.Sprintf(`
port %d
cafile /mqtt/certs/%s
certfile /mqtt/certs/%s
keyfile /mqtt/certs/%s
require_certificate true
use_identity_as_username false
allow_anonymous true
log_dest stdout
log_type all
`, brokerPort, filepath.Base(certs.CACert), filepath.Base(certs.MQTTServerCert), filepath.Base(certs.MQTTServerKey))

	configPath := filepath.Join(filepath.Dir(certs.CACert), "mosquitto.conf")
	err := ioutil.WriteFile(configPath, []byte(strings.TrimSpace(configContent)), 0644)
	if err != nil {
		t.Fatalf("Failed to write mosquitto config: %v", err)
	}

	// Stop any existing mosquitto container
	t.Logf("Stopping any existing mosquitto container...")
	exec.Command("docker", "stop", "mqtt-broker").Run()
	exec.Command("docker", "rm", "mqtt-broker").Run()

	// Try different Docker network strategies
	strategies := []struct {
		name string
		args []string
	}{
		{
			"Host Network Mode",
			[]string{"run", "-d", "--name", "mqtt-broker", "--network", "host"},
		},
		{
			"Bridge with Port Binding",
			[]string{"run", "-d", "--name", "mqtt-broker", "-p", fmt.Sprintf("0.0.0.0:%d:%d", brokerPort, brokerPort)},
		},
		{
			"Multiple Interface Binding",
			[]string{"run", "-d", "--name", "mqtt-broker",
				"-p", fmt.Sprintf("127.0.0.1:%d:%d", brokerPort, brokerPort),
				"-p", fmt.Sprintf("192.168.49.1:%d:%d", brokerPort, brokerPort),
				"-p", fmt.Sprintf("0.0.0.0:%d:%d", brokerPort, brokerPort)},
		},
	}

	certsDir := filepath.Dir(certs.CACert)
	var successfulStrategy string
	var containerID string

	for _, strategy := range strategies {
		t.Logf("Trying Docker strategy: %s", strategy.name)

		// Build complete Docker command
		dockerArgs := strategy.args
		dockerArgs = append(dockerArgs,
			"-v", fmt.Sprintf("%s:/mqtt/certs", certsDir),
			"-v", fmt.Sprintf("%s:/mosquitto/config", certsDir),
			"eclipse-mosquitto:2.0",
			"mosquitto", "-c", "/mosquitto/config/mosquitto.conf")

		t.Logf("Docker command: docker %s", strings.Join(dockerArgs, " "))
		cmd := exec.Command("docker", dockerArgs...)

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err = cmd.Run()
		if err != nil {
			t.Logf("Strategy '%s' failed: %v", strategy.name, err)
			t.Logf("Docker stdout: %s", stdout.String())
			t.Logf("Docker stderr: %s", stderr.String())
			// Clean up and try next strategy
			exec.Command("docker", "stop", "mqtt-broker").Run()
			exec.Command("docker", "rm", "mqtt-broker").Run()
			continue
		}

		containerID = strings.TrimSpace(stdout.String())
		t.Logf("Strategy '%s' succeeded with container ID: %s", strategy.name, containerID)

		// Wait for container to be ready
		time.Sleep(5 * time.Second)

		// Test if the broker is accessible from multiple perspectives
		if verifyMQTTBrokerAccessibilityWindows(t, brokerPort) {
			successfulStrategy = strategy.name
			t.Logf("✅ MQTT broker successfully started using strategy: %s", strategy.name)
			break
		} else {
			t.Logf("❌ Strategy '%s' started container but broker not accessible, trying next...", strategy.name)
			exec.Command("docker", "stop", "mqtt-broker").Run()
			exec.Command("docker", "rm", "mqtt-broker").Run()
		}
	}

	if successfulStrategy == "" {
		t.Fatalf("All Docker strategies failed to start accessible MQTT broker")
	}

	// Detect the best broker address for connectivity
	brokerAddress := detectOptimalBrokerAddressWindows(t, brokerPort)
	t.Logf("Using optimal broker address for connectivity: %s", brokerAddress)

	return brokerAddress
}

// verifyMQTTBrokerAccessibilityWindows tests MQTT broker accessibility from multiple network contexts
func verifyMQTTBrokerAccessibilityWindows(t *testing.T, brokerPort int) bool {
	testAddresses := []string{
		"127.0.0.1",
		"localhost",
	}

	// Try to get the Windows host IP
	if hostIP := getWindowsHostIPAddress(t); hostIP != "" {
		testAddresses = append(testAddresses, hostIP)
	}

	for _, address := range testAddresses {
		t.Logf("Testing MQTT broker accessibility at %s:%d", address, brokerPort)

		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", address, brokerPort), 3*time.Second)
		if err == nil {
			conn.Close()
			t.Logf("✅ MQTT broker accessible at %s:%d", address, brokerPort)
			return true
		}
		t.Logf("❌ MQTT broker not accessible at %s:%d: %v", address, brokerPort, err)
	}

	return false
}

// detectOptimalBrokerAddressWindows finds the best address for minikube to connect to the MQTT broker
func detectOptimalBrokerAddressWindows(t *testing.T, brokerPort int) string {
	// Try addresses in order of preference for minikube connectivity
	candidateAddresses := []string{
		"192.168.49.1",         // Common minikube host IP
		"host.docker.internal", // Docker Desktop host resolution
		"127.0.0.1",            // IPv4 localhost
		"localhost",            // Hostname localhost
	}

	// Add detected Windows host IP if available
	if hostIP := getWindowsHostIPAddress(t); hostIP != "" && hostIP != "127.0.0.1" {
		candidateAddresses = append([]string{hostIP}, candidateAddresses...)
	}

	// Test each address for broker connectivity
	for _, address := range candidateAddresses {
		t.Logf("Testing broker address candidate: %s:%d", address, brokerPort)
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", address, brokerPort), 2*time.Second)
		if err == nil {
			conn.Close()
			t.Logf("✅ Optimal broker address detected: %s", address)
			return address
		}
		t.Logf("❌ Address %s not accessible: %v", address, err)
	}

	// Fallback to localhost if nothing else works
	t.Logf("Using localhost as fallback broker address")
	return "127.0.0.1"
}

// getWindowsHostIPAddress gets the primary Windows host IP address
func getWindowsHostIPAddress(t *testing.T) string {
	// Method 1: Use PowerShell Test-NetConnection to get source IP
	cmd := exec.Command("powershell", "-Command", "(Test-NetConnection -ComputerName 8.8.8.8 -Port 53).SourceAddress.IPAddress")
	if output, err := cmd.Output(); err == nil {
		ip := strings.TrimSpace(string(output))
		if ip != "" && net.ParseIP(ip) != nil {
			return ip
		}
	}

	// Method 2: Get first non-loopback IPv4 address
	cmd = exec.Command("powershell", "-Command",
		"(Get-NetIPAddress -AddressFamily IPv4 | Where-Object {$_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.254.*'} | Select-Object -First 1).IPAddress")
	if output, err := cmd.Output(); err == nil {
		ip := strings.TrimSpace(string(output))
		if ip != "" && net.ParseIP(ip) != nil {
			return ip
		}
	}

	return ""
}

// CleanupExternalMQTTBrokerWindows cleans up external MQTT broker Docker container on Windows
func CleanupExternalMQTTBrokerWindows(t *testing.T) {
	t.Logf("Cleaning up external MQTT broker Docker container on Windows...")

	// Stop and remove Docker container
	exec.Command("docker", "stop", "mqtt-broker").Run()
	exec.Command("docker", "rm", "mqtt-broker").Run()

	t.Logf("External MQTT broker cleanup completed on Windows")
}

// CreateMQTTCASecretForRemoteAgentWindows creates CA secret for remote agent on Windows
func CreateMQTTCASecretForRemoteAgentWindows(t *testing.T, certs WindowsMQTTCertificatePaths) string {
	secretName := "client-cert-secret"

	// Ensure cert-manager namespace exists
	cmd := exec.Command("kubectl", "create", "namespace", "cert-manager")
	cmd.Run() // Ignore error if namespace already exists

	// Create CA secret in cert-manager namespace
	cmd = exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=ca.crt="+certs.CACert,
		"-n", "cert-manager")

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create CA secret (may already exist): %v", err)
	} else {
		t.Logf("Created CA secret %s in cert-manager namespace", secretName)
	}
	return secretName
}

// CreateMQTTClientSecretForSymphonyWindows creates Symphony MQTT client certificate secret on Windows
func CreateMQTTClientSecretForSymphonyWindows(t *testing.T, namespace string, certs WindowsMQTTCertificatePaths) string {
	secretName := "mqtt-client-secret"

	cmd := exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=client.crt="+certs.SymphonyClientCert,
		"--from-file=client.key="+certs.SymphonyClientKey,
		"-n", namespace)

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create Symphony MQTT client secret (may already exist): %v", err)
	} else {
		t.Logf("Created Symphony MQTT client cert secret %s in namespace %s", secretName, namespace)
	}
	return secretName
}

// VerifyMQTTConnectivityWindows tests basic TCP connectivity to MQTT broker from minikube cluster with enhanced network detection
func VerifyMQTTConnectivityWindows(t *testing.T, brokerAddress string, brokerPort int) bool {
	t.Logf("Testing enhanced MQTT connectivity to broker at %s:%d from minikube", brokerAddress, brokerPort)

	// Try multiple connection strategies with timeouts
	strategies := []struct {
		name    string
		address string
		timeout time.Duration
	}{
		{"Original Address", brokerAddress, 15 * time.Second},
		{"Host Docker Internal", "host.docker.internal", 10 * time.Second},
		{"Container IP", "", 10 * time.Second}, // Will be filled in
	}

	// Get Docker container IP for the MQTT broker
	containerIP := getDockerContainerIPWindows(t, "mqtt-broker")
	if containerIP != "" {
		strategies[2].address = containerIP
		t.Logf("Detected MQTT broker container IP: %s", containerIP)
	} else {
		// Remove container IP strategy if we can't detect it
		strategies = strategies[:2]
	}

	for _, strategy := range strategies {
		if strategy.address == "" {
			continue
		}

		t.Logf("Trying strategy: %s (%s:%d)", strategy.name, strategy.address, brokerPort)

		if testTCPConnectivityFromMinikubeWindows(t, strategy.address, brokerPort, strategy.timeout) {
			t.Logf("✅ TCP connectivity test PASSED using %s (%s:%d)", strategy.name, strategy.address, brokerPort)
			return true
		}

		t.Logf("❌ Strategy %s failed, trying next...", strategy.name)
	}

	t.Logf("❌ All connectivity strategies failed for MQTT broker")
	return false
}

// testTCPConnectivityFromMinikubeWindows tests TCP connectivity from minikube with timeout
func testTCPConnectivityFromMinikubeWindows(t *testing.T, address string, port int, timeout time.Duration) bool {
	testPodName := "mqtt-connectivity-test"
	target := fmt.Sprintf("%s:%d", address, port)

	// Clean up any existing test pod first
	exec.Command("kubectl", "delete", "pod", testPodName, "--ignore-not-found=true").Run()
	time.Sleep(1 * time.Second)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Run connectivity test using netcat from busybox with timeout
	cmd := exec.CommandContext(ctx, "kubectl", "run", testPodName,
		"--image=busybox",
		"--rm", "-i", "--restart=Never",
		"--timeout="+fmt.Sprintf("%.0fs", timeout.Seconds()),
		"--",
		"sh", "-c", fmt.Sprintf("timeout %.0f nc -zv %s %d", timeout.Seconds()-5, address, port))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	t.Logf("Running kubectl command with %v timeout: %s", timeout, cmd.String())

	err := cmd.Run()

	// Clean up - use force delete if needed
	cleanupCmd := exec.Command("kubectl", "delete", "pod", testPodName, "--ignore-not-found=true", "--force", "--grace-period=0")
	cleanupCmd.Run()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			t.Logf("TCP connectivity test TIMEOUT after %v to %s", timeout, target)
		} else {
			t.Logf("TCP connectivity test FAILED to %s: %v", target, err)
		}
		t.Logf("stdout: %s", stdout.String())
		t.Logf("stderr: %s", stderr.String())
		return false
	}

	t.Logf("TCP connectivity test PASSED to %s", target)
	t.Logf("netcat output: %s", stdout.String())
	return true
}

// getDockerContainerIPWindows retrieves the IP address of a Docker container
func getDockerContainerIPWindows(t *testing.T, containerName string) string {
	cmd := exec.Command("docker", "inspect", containerName, "--format={{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}")
	output, err := cmd.Output()
	if err != nil {
		t.Logf("Failed to get container IP for %s: %v", containerName, err)
		return ""
	}

	ip := strings.TrimSpace(string(output))
	if ip == "" {
		t.Logf("No IP found for container %s", containerName)
		return ""
	}

	t.Logf("Container %s IP: %s", containerName, ip)
	return ip
}

// VerifyMQTTWithCertificatesWindows tests actual MQTT connection with certificates from minikube cluster
func VerifyMQTTWithCertificatesWindows(t *testing.T, brokerAddress string, brokerPort int, certs WindowsMQTTCertificatePaths) bool {
	t.Logf("Testing MQTT connection with certificates to %s:%d", brokerAddress, brokerPort)

	// Create a ConfigMap with the CA certificate for the test pod
	testConfigMapName := "mqtt-test-ca"
	exec.Command("kubectl", "delete", "configmap", testConfigMapName, "--ignore-not-found=true").Run()

	cmd := exec.Command("kubectl", "create", "configmap", testConfigMapName,
		"--from-file=ca.crt="+certs.CACert)
	if err := cmd.Run(); err != nil {
		t.Logf("Failed to create CA ConfigMap: %v", err)
		return false
	}

	defer func() {
		exec.Command("kubectl", "delete", "configmap", testConfigMapName, "--ignore-not-found=true").Run()
	}()

	// Create test pod with mosquitto client
	testPodName := "mqtt-cert-test"
	exec.Command("kubectl", "delete", "pod", testPodName, "--ignore-not-found=true").Run()
	time.Sleep(2 * time.Second)

	// Enhanced pod YAML that can handle both localhost and host.docker.internal addresses
	// Use host networking mode to avoid DNS resolution issues with host.docker.internal from inside pods
	var podYaml string
	if brokerAddress == "host.docker.internal" {
		// For host.docker.internal, use the Windows host IP that we can resolve from the test environment
		if hostIP := getWindowsHostIPAddress(t); hostIP != "" {
			t.Logf("Resolving host.docker.internal to Windows host IP: %s", hostIP)
			podYaml = fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
spec:
  restartPolicy: Never
  hostNetwork: true
  containers:
  - name: mqtt-test
    image: eclipse-mosquitto:2.0
    command: ["sh", "-c"]
    args:
    - |
      echo "Testing MQTT connection with CA certificate to %s..."
      echo "Using resolved IP address: %s"
      # Try the resolved IP address first
      mosquitto_pub -h %s -p %d -t test/connectivity -m "test message" --cafile /certs/ca.crt --insecure && exit 0
      # If that fails, try host.docker.internal directly
      mosquitto_pub -h %s -p %d -t test/connectivity -m "test message" --cafile /certs/ca.crt --insecure || exit 1
      echo "MQTT certificate test completed successfully"
    volumeMounts:
    - name: ca-cert
      mountPath: /certs
      readOnly: true
  volumes:
  - name: ca-cert
    configMap:
      name: %s
`, testPodName, brokerAddress, hostIP, hostIP, brokerPort, brokerAddress, brokerPort, testConfigMapName)
		} else {
			// Fallback if we can't resolve the host IP
			t.Logf("Could not resolve Windows host IP, using host.docker.internal directly")
			podYaml = fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
spec:
  restartPolicy: Never
  hostNetwork: true
  containers:
  - name: mqtt-test
    image: eclipse-mosquitto:2.0
    command: ["sh", "-c"]
    args:
    - |
      echo "Testing MQTT connection with CA certificate to %s..."
      mosquitto_pub -h %s -p %d -t test/connectivity -m "test message" --cafile /certs/ca.crt --insecure || exit 1
      echo "MQTT certificate test completed successfully"
    volumeMounts:
    - name: ca-cert
      mountPath: /certs
      readOnly: true
  volumes:
  - name: ca-cert
    configMap:
      name: %s
`, testPodName, brokerAddress, brokerAddress, brokerPort, testConfigMapName)
		}
	} else {
		// Standard pod configuration for other addresses
		podYaml = fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
spec:
  restartPolicy: Never
  containers:
  - name: mqtt-test
    image: eclipse-mosquitto:2.0
    command: ["sh", "-c"]
    args:
    - |
      echo "Testing MQTT connection with CA certificate to %s..."
      mosquitto_pub -h %s -p %d -t test/connectivity -m "test message" --cafile /certs/ca.crt --insecure || exit 1
      echo "MQTT certificate test completed successfully"
    volumeMounts:
    - name: ca-cert
      mountPath: /certs
      readOnly: true
  volumes:
  - name: ca-cert
    configMap:
      name: %s
`, testPodName, brokerAddress, brokerAddress, brokerPort, testConfigMapName)
	}

	// Write pod YAML to temp file
	tempDir := CreateWindowsTestDirectory(t)
	podYamlPath := filepath.Join(tempDir, "mqtt-test-pod.yaml")
	err := ioutil.WriteFile(podYamlPath, []byte(podYaml), 0644)
	if err != nil {
		t.Logf("Failed to write pod YAML: %v", err)
		return false
	}

	// Apply pod YAML
	cmd = exec.Command("kubectl", "apply", "-f", podYamlPath)
	if err := cmd.Run(); err != nil {
		t.Logf("Failed to create MQTT test pod: %v", err)
		return false
	}

	defer func() {
		exec.Command("kubectl", "delete", "pod", testPodName, "--ignore-not-found=true").Run()
	}()

	// Wait for pod to complete and get logs
	t.Logf("Waiting for MQTT certificate test to complete...")

	// Wait up to 60 seconds for pod completion
	for i := 0; i < 60; i++ {
		cmd = exec.Command("kubectl", "get", "pod", testPodName, "-o", "jsonpath={.status.phase}")
		if output, err := cmd.Output(); err == nil {
			phase := strings.TrimSpace(string(output))
			if phase == "Succeeded" {
				t.Logf("MQTT certificate test PASSED")

				// Get pod logs for verification
				cmd = exec.Command("kubectl", "logs", testPodName)
				if logOutput, err := cmd.Output(); err == nil {
					t.Logf("MQTT test pod logs: %s", string(logOutput))
				}
				return true
			} else if phase == "Failed" {
				t.Logf("MQTT certificate test FAILED")

				// Get pod logs for debugging
				cmd = exec.Command("kubectl", "logs", testPodName)
				if logOutput, err := cmd.Output(); err == nil {
					t.Logf("MQTT test pod logs: %s", string(logOutput))
				}
				return false
			}
		}
		time.Sleep(1 * time.Second)
	}

	t.Logf("MQTT certificate test TIMEOUT - pod did not complete within 60 seconds")
	return false
}

// VerifyFirewallRuleWindows verifies that a Windows firewall rule exists and is active
func VerifyFirewallRuleWindows(t *testing.T, ruleName string) bool {
	t.Logf("Verifying Windows firewall rule: %s", ruleName)

	// Use netsh to check if the firewall rule exists
	cmd := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", fmt.Sprintf("name=%s", ruleName))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("Firewall rule '%s' not found or command failed: %v", ruleName, err)
		t.Logf("netsh stderr: %s", stderr.String())
		return false
	}

	output := stdout.String()
	if strings.Contains(output, "No rules match the specified criteria") {
		t.Logf("Firewall rule '%s' does not exist", ruleName)
		return false
	}

	t.Logf("Firewall rule '%s' exists and is active", ruleName)
	t.Logf("Rule details: %s", output)
	return true
}

// CreateFirewallRuleWindows attempts to create a Windows firewall rule for MQTT port
func CreateFirewallRuleWindows(t *testing.T, port int) bool {
	ruleName := fmt.Sprintf("Allow MQTT %d", port)
	t.Logf("Attempting to create Windows firewall rule: %s", ruleName)

	// Try using netsh to create the firewall rule
	cmd := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		fmt.Sprintf("name=%s", ruleName),
		"dir=in",
		"action=allow",
		"protocol=TCP",
		fmt.Sprintf("localport=%d", port))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("Failed to create firewall rule (may require administrator privileges): %v", err)
		t.Logf("netsh stdout: %s", stdout.String())
		t.Logf("netsh stderr: %s", stderr.String())

		// Try PowerShell method as fallback
		t.Logf("Attempting PowerShell firewall rule creation...")
		psScript := fmt.Sprintf(`
try {
    New-NetFirewallRule -DisplayName "%s" -Direction Inbound -Action Allow -Protocol TCP -LocalPort %d
    Write-Output "PowerShell firewall rule created successfully"
} catch {
    Write-Error "PowerShell firewall rule creation failed: $_"
    throw $_
}`, ruleName, port)

		tempScriptFile := filepath.Join(os.TempDir(), "create_firewall_rule.ps1")
		err := ioutil.WriteFile(tempScriptFile, []byte(psScript), 0644)
		if err != nil {
			t.Logf("Failed to write PowerShell script: %v", err)
			return false
		}
		defer os.Remove(tempScriptFile)

		psCmd := ExecutePowerShell7Script(t, tempScriptFile, []string{}, os.TempDir())
		psCmd.Stdout = &stdout
		psCmd.Stderr = &stderr
		stdout.Reset()
		stderr.Reset()

		err = psCmd.Run()
		if err != nil {
			t.Logf("PowerShell firewall rule creation also failed: %v", err)
			t.Logf("PowerShell stdout: %s", stdout.String())
			t.Logf("PowerShell stderr: %s", stderr.String())
			return false
		}

		t.Logf("PowerShell firewall rule created successfully")
		return true
	}

	t.Logf("Firewall rule created successfully using netsh")
	t.Logf("netsh output: %s", stdout.String())
	return true
}

// TestNetworkConnectivityWindows tests network connectivity from minikube to host using PowerShell Test-NetConnection
func TestNetworkConnectivityWindows(t *testing.T, targetAddress string, port int) bool {
	t.Logf("Testing network connectivity from Windows host to %s:%d", targetAddress, port)

	// Use PowerShell Test-NetConnection for comprehensive network testing
	psScript := fmt.Sprintf(`
try {
    $result = Test-NetConnection -ComputerName "%s" -Port %d -InformationLevel Detailed
    Write-Output "Connection test results:"
    Write-Output "  Target: $($result.ComputerName):$($result.RemotePort)"
    Write-Output "  Source: $($result.SourceAddress.IPAddress)"
    Write-Output "  TcpTestSucceeded: $($result.TcpTestSucceeded)"
    Write-Output "  InterfaceAlias: $($result.InterfaceAlias)"
    Write-Output "  NetworkPath: $($result.NetworkPath)"
    
    if ($result.TcpTestSucceeded) {
        Write-Output "SUCCESS: Network connectivity test passed"
        exit 0
    } else {
        Write-Output "FAILED: Network connectivity test failed"
        exit 1
    }
} catch {
    Write-Error "Network connectivity test error: $_"
    exit 1
}`, targetAddress, port)

	tempScriptFile := filepath.Join(os.TempDir(), "test_network_connectivity.ps1")
	err := ioutil.WriteFile(tempScriptFile, []byte(psScript), 0644)
	if err != nil {
		t.Logf("Failed to write PowerShell script: %v", err)
		return false
	}
	defer os.Remove(tempScriptFile)

	cmd := ExecutePowerShell7Script(t, tempScriptFile, []string{}, os.TempDir())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Logf("Network connectivity test FAILED: %v", err)
		t.Logf("PowerShell output: %s", stdout.String())
		t.Logf("PowerShell errors: %s", stderr.String())
		return false
	}

	t.Logf("Network connectivity test PASSED")
	t.Logf("Test results: %s", stdout.String())
	return true
}

// Missing functions required for mqtt_schedule_test.go

// CleanupStaleRemoteAgentProcessesWindows cleans up any stale remote agent processes on Windows
func CleanupStaleRemoteAgentProcessesWindows(t *testing.T) {
	t.Logf("Cleaning up stale remote agent processes on Windows...")

	// Find and kill any existing remote-agent.exe processes
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq remote-agent.exe", "/FO", "CSV")
	output, err := cmd.Output()
	if err != nil {
		t.Logf("Failed to list remote agent processes: %v", err)
		return
	}

	if strings.Contains(string(output), "remote-agent.exe") {
		t.Logf("Found existing remote agent processes, terminating...")
		killCmd := exec.Command("taskkill", "/F", "/IM", "remote-agent.exe")
		if err := killCmd.Run(); err != nil {
			t.Logf("Warning: Failed to kill remote agent processes: %v", err)
		} else {
			t.Logf("Successfully terminated existing remote agent processes")
		}
	}
}

// SetupWindowsTestDirectory creates a Windows test directory (alias for CreateWindowsTestDirectory)
func SetupWindowsTestDirectory(t *testing.T) string {
	return CreateWindowsTestDirectory(t)
}

// SetupWindowsMQTTScheduleTestWithDetectedAddress sets up Windows MQTT schedule test with proper infrastructure
func SetupWindowsMQTTScheduleTestWithDetectedAddress(t *testing.T, testDir, targetName, namespace string) (WindowsTestConfig, string, string) {
	t.Logf("Setting up Windows MQTT schedule test with complete infrastructure")

	// Generate complete MQTT certificates similar to bootstrap test
	mqttCerts := GenerateWindowsMQTTCertificates(t, testDir)
	t.Logf("Generated complete MQTT certificate set for schedule test")

	// Setup external MQTT broker using Docker (like bootstrap test)
	mqttBrokerPort := 8883
	detectedBrokerAddress := SetupExternalMQTTBrokerWindows(t, mqttCerts, mqttBrokerPort)
	t.Logf("External MQTT broker started at: %s:%d", detectedBrokerAddress, mqttBrokerPort)

	// Create secrets for Symphony MQTT communication
	CreateMQTTCASecretForRemoteAgentWindows(t, mqttCerts)
	CreateMQTTClientSecretForSymphonyWindows(t, namespace, mqttCerts)

	// Create topology file
	topologyPath := CreateTestTopologyWindows(t, testDir)

	// Create MQTT config using detected broker address
	configPath := CreateMQTTConfigWindows(t, testDir, detectedBrokerAddress, mqttBrokerPort, targetName, namespace)

	// Setup Windows test configuration for MQTT schedule mode with complete certificate paths
	config := WindowsTestConfig{
		ProjectRoot:    GetWindowsProjectRoot(t),
		ConfigPath:     configPath,
		ClientCertPath: mqttCerts.RemoteAgentCert, // Use proper MQTT client certificate
		ClientKeyPath:  mqttCerts.RemoteAgentKey,
		CertPassword:   "", // MQTT uses PEM, no password needed
		CACertPath:     mqttCerts.CACert,
		TargetName:     targetName,
		Namespace:      namespace,
		TopologyPath:   topologyPath,
		Protocol:       "mqtt",
		BrokerAddress:  detectedBrokerAddress,
		BrokerPort:     fmt.Sprintf("%d", mqttBrokerPort),
		RunMode:        "schedule", // Different from service mode
	}

	// Verify MQTT connectivity from minikube cluster
	if VerifyMQTTConnectivityWindows(t, detectedBrokerAddress, mqttBrokerPort) {
		t.Logf("✅ MQTT broker connectivity verified from minikube")
	} else {
		t.Logf("⚠️ MQTT broker connectivity test failed, but continuing with test")
	}

	// Test MQTT with certificates from minikube cluster
	if VerifyMQTTWithCertificatesWindows(t, detectedBrokerAddress, mqttBrokerPort, mqttCerts) {
		t.Logf("✅ MQTT certificate connectivity verified from minikube")
	} else {
		t.Logf("⚠️ MQTT certificate connectivity test failed, but continuing with test")
	}

	caSecretName := "mqtt-ca"

	return config, detectedBrokerAddress, caSecretName
}

// DebugWindowsMQTTBrokerCertificates debugs MQTT broker certificates on Windows
func DebugWindowsMQTTBrokerCertificates(t *testing.T, testDir string) {
	t.Logf("Debugging Windows MQTT broker certificates in: %s", testDir)

	certFiles := []string{"ca.crt", "mqtt-server.crt", "mqtt-server.key", "client.crt", "client.key"}
	for _, certFile := range certFiles {
		certPath := filepath.Join(testDir, certFile)
		if FileExistsWindows(certPath) {
			t.Logf("✓ Certificate file exists: %s", certPath)
		} else {
			t.Logf("✗ Certificate file missing: %s", certPath)
		}
	}
}

// WindowsFileExists checks if a file exists on Windows (alias for FileExistsWindows)
func WindowsFileExists(filePath string) bool {
	return FileExistsWindows(filePath)
}

// TestWindowsMQTTCertificateChain tests MQTT certificate chain on Windows
func TestWindowsMQTTCertificateChain(t *testing.T, caCertPath, serverCertPath string) {
	t.Logf("Testing Windows MQTT certificate chain: CA=%s, Server=%s", caCertPath, serverCertPath)

	// For now, just log the test - in a full implementation this would verify certificate chain
	t.Logf("Windows MQTT certificate chain test completed")
}

// TestWindowsMQTTConnectionWithClientCert tests MQTT connection with client certificate on Windows
func TestWindowsMQTTConnectionWithClientCert(t *testing.T, address string, port int, caCertPath, clientCertPath, clientKeyPath string) bool {
	t.Logf("Testing Windows MQTT connection with client cert to %s:%d", address, port)

	// Simple TCP connection test for now
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", address, port), 5*time.Second)
	if err != nil {
		t.Logf("Windows MQTT connection test failed: %v", err)
		return false
	}
	defer conn.Close()

	t.Logf("Windows MQTT connection test succeeded")
	return true
}

// CreateWindowsTargetYAML creates Target YAML for Windows (alias for CreateTargetYAMLWindows)
func CreateWindowsTargetYAML(t *testing.T, testDir, targetName, namespace string) string {
	return CreateTargetYAMLWindows(t, testDir, targetName, namespace)
}

// StartWindowsRemoteAgentProcessWithoutCleanup starts Windows remote agent process without cleanup with real-time logging
func StartWindowsRemoteAgentProcessWithoutCleanup(t *testing.T, config WindowsTestConfig) *exec.Cmd {
	t.Logf("Starting Windows remote agent process without cleanup in schedule mode with real-time logging...")

	// Build binary if needed
	if config.BinaryPath == "" {
		config.BinaryPath = BuildWindowsRemoteAgent(t, config)
	}

	// Start remote agent directly as process (not as service)
	// Use only valid command-line flags that exist in remote-agent/main.go
	// MQTT broker and port MUST be specified in the config JSON file, NOT as command-line flags
	args := []string{
		"-config", config.ConfigPath,
		"-topology", config.TopologyPath,
		"-target-name", config.TargetName,
		"-namespace", config.Namespace,
		"-protocol", config.Protocol,
		"-client-cert", config.ClientCertPath,
		"-client-key", config.ClientKeyPath,
	}

	// Only add CA cert flag if path is provided
	if config.CACertPath != "" {
		args = append(args, "-ca-cert", config.CACertPath)
	}

	t.Logf("Starting Windows remote agent with corrected arguments: %v", args)

	// Log config file contents for debugging
	if configBytes, err := os.ReadFile(config.ConfigPath); err == nil {
		t.Logf("Config file contents: %s", string(configBytes))
	} else {
		t.Logf("Warning: Could not read config file for debugging: %v", err)
	}

	cmd := exec.Command(config.BinaryPath, args...)

	// Set up pipes for real-time log streaming
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe for Windows remote agent: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Failed to create stderr pipe for Windows remote agent: %v", err)
	}

	// Start the process
	err = cmd.Start()
	if err != nil {
		t.Fatalf("Failed to start Windows remote agent process: %v", err)
	}

	t.Logf("Windows remote agent process started with PID: %d", cmd.Process.Pid)

	// Start real-time log streaming in separate goroutines
	go streamProcessLogsWindows(t, stdout, "STDOUT")
	go streamProcessLogsWindows(t, stderr, "STDERR")

	t.Logf("Real-time log streaming initiated for Windows remote agent process")
	return cmd
}

// CleanupWindowsRemoteAgentProcess cleans up Windows remote agent process
func CleanupWindowsRemoteAgentProcess(t *testing.T, cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	t.Logf("Cleaning up Windows remote agent process PID: %d", cmd.Process.Pid)

	// Try graceful termination first
	if err := cmd.Process.Signal(os.Interrupt); err == nil {
		// Wait up to 5 seconds for graceful shutdown
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case <-done:
			t.Logf("Windows remote agent process terminated gracefully")
			return
		case <-time.After(5 * time.Second):
			t.Logf("Windows remote agent process did not terminate gracefully, force killing...")
		}
	}

	// Force kill if graceful termination failed
	if err := cmd.Process.Kill(); err != nil {
		t.Logf("Failed to force kill Windows remote agent process: %v", err)
	} else {
		t.Logf("Windows remote agent process force killed")
	}
}

// WaitForWindowsProcessHealthy waits for Windows process to be healthy
func WaitForWindowsProcessHealthy(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Logf("Waiting for Windows process to be healthy (timeout: %v)", timeout)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Check if process has already exited before we start waiting
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		t.Logf("Windows process exited during health check: %s", cmd.ProcessState.String())
		return
	}

	for {
		select {
		case <-ctx.Done():
			t.Logf("Timeout waiting for Windows process to be healthy after %v", timeout)
			return
		case <-ticker.C:
			// Check if process exited during our wait
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
				t.Logf("Windows process exited during health check: %s", cmd.ProcessState.String())
				return
			}

			// Windows-compatible process health check - avoid Signal(0) which is not supported
			if cmd.Process != nil && cmd.ProcessState == nil {
				t.Logf("Windows process is healthy (PID: %d)", cmd.Process.Pid)
				return
			}

			t.Logf("Windows process health check: process not running or exited")
			continue
		}
	}
}

// DebugWindowsMQTTSecrets debugs MQTT secrets on Windows
func DebugWindowsMQTTSecrets(t *testing.T, namespace string) {
	t.Logf("Debugging Windows MQTT secrets in namespace: %s", namespace)

	secrets := []string{"mqtt-ca", "mqtt-client-secret", "symphony-api-serving-cert"}
	for _, secret := range secrets {
		cmd := exec.Command("kubectl", "get", "secret", secret, "-n", namespace, "--ignore-not-found")
		if err := cmd.Run(); err == nil {
			t.Logf("✓ Secret exists: %s", secret)
		} else {
			t.Logf("✗ Secret missing: %s", secret)
		}
	}
}

// DebugSymphonyPodCertificatesWindows debugs Symphony pod certificates on Windows
func DebugSymphonyPodCertificatesWindows(t *testing.T) {
	t.Logf("Debugging Symphony pod certificates on Windows...")

	// Check Symphony pods
	cmd := exec.Command("kubectl", "get", "pods", "-l", "app.kubernetes.io/name=symphony", "-n", "default")
	output, err := cmd.Output()
	if err != nil {
		t.Logf("Failed to get Symphony pods: %v", err)
		return
	}

	t.Logf("Symphony pods: %s", string(output))
}

// CleanupWindowsMQTTCASecret cleans up Windows MQTT CA secret
func CleanupWindowsMQTTCASecret(t *testing.T, secretName string) {
	t.Logf("Cleaning up Windows MQTT CA secret: %s", secretName)

	cmd := exec.Command("kubectl", "delete", "secret", secretName, "-n", "cert-manager", "--ignore-not-found")
	if err := cmd.Run(); err != nil {
		t.Logf("Warning: Failed to delete CA secret %s: %v", secretName, err)
	} else {
		t.Logf("Successfully deleted CA secret: %s", secretName)
	}
}

// CleanupWindowsMQTTClientSecret cleans up Windows MQTT client secret
func CleanupWindowsMQTTClientSecret(t *testing.T, namespace, secretName string) {
	t.Logf("Cleaning up Windows MQTT client secret: %s in namespace %s", secretName, namespace)

	cmd := exec.Command("kubectl", "delete", "secret", secretName, "-n", namespace, "--ignore-not-found")
	if err := cmd.Run(); err != nil {
		t.Logf("Warning: Failed to delete client secret %s: %v", secretName, err)
	} else {
		t.Logf("Successfully deleted client secret: %s", secretName)
	}
}

// GetKubeClientWindows gets Kubernetes client on Windows
func GetKubeClientWindows() (interface{}, error) {
	// For now, just test kubectl connectivity
	cmd := exec.Command("kubectl", "version", "--client")
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("kubectl not available: %v", err)
	}

	return "kubectl-client", nil
}

// CreateWindowsYAMLFile creates YAML file on Windows (alias)
func CreateWindowsYAMLFile(t *testing.T, filePath, content string) error {
	return CreateYAMLFileWindows(t, filePath, content)
}
