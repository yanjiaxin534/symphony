//go:build windows || !linux

package verify

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
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
	ClientCertPath string
	ClientKeyPath  string
	CertPassword   string
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
	ClientCert string
	ClientKey  string
	ClientPEM  string
	Password   string
}

// GetWindowsProjectRoot returns the project root directory using Windows path conventions
func GetWindowsProjectRoot(t *testing.T) string {
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	for {
		expectedDirs := []string{"api", "coa", "remote-agent", "test"}
		isProjectRoot := true

		for _, dir := range expectedDirs {
			fullPath := filepath.Join(currentDir, dir)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				isProjectRoot = false
				break
			}
		}

		if isProjectRoot {
			t.Logf("Project root detected: %s", currentDir)
			return currentDir
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			t.Fatalf("Could not find Symphony project root")
		}
		currentDir = parentDir
	}
}

// GenerateWindowsCertificates generates certificates suitable for Windows testing
func GenerateWindowsCertificates(t *testing.T, testDir string) WindowsCertificatePaths {
	t.Logf("Generating Windows certificates in directory: %s", testDir)

	// Generate CA key
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate CA key: %v", err)
	}

	// Create CA certificate template
	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Symphony Test CA"},
			Country:      []string{"US"},
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

	// Save CA certificate
	caCertPath := filepath.Join(testDir, "ca.crt")
	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
	err = ioutil.WriteFile(caCertPath, caCertPEM, 0644)
	if err != nil {
		t.Fatalf("Failed to write CA certificate: %v", err)
	}

	// Generate client key
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate client key: %v", err)
	}

	// Create client certificate
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Symphony Test Client"},
			CommonName:   "remote-agent-client",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		DNSNames:    []string{"localhost", "remote-agent-client"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	clientCertDER, err := x509.CreateCertificate(rand.Reader, &clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create client certificate: %v", err)
	}

	// Save client certificate
	clientCertPEMPath := filepath.Join(testDir, "client.crt")
	clientCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCertDER})
	err = ioutil.WriteFile(clientCertPEMPath, clientCertPEM, 0644)
	if err != nil {
		t.Fatalf("Failed to write client certificate: %v", err)
	}

	// Save client key
	clientKeyPath := filepath.Join(testDir, "client.key")
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientKey)})
	err = ioutil.WriteFile(clientKeyPath, clientKeyPEM, 0644)
	if err != nil {
		t.Fatalf("Failed to write client key: %v", err)
	}

	return WindowsCertificatePaths{
		CACert:     caCertPath,
		ClientCert: clientCertPEMPath,
		ClientKey:  clientKeyPath,
		ClientPEM:  clientCertPEMPath,
		Password:   "test123",
	}
}

// CreateWindowsTestDirectory creates a temporary directory for Windows test files
func CreateWindowsTestDirectory(t *testing.T) string {
	tempDir := os.TempDir()
	testDir, err := ioutil.TempDir(tempDir, "symphony-windows-e2e-test-")
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	t.Logf("Created Windows test directory: %s", testDir)
	return testDir
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

// FileExistsWindows checks if a file exists on Windows
func FileExistsWindows(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
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

	t.Logf("Could not detect Windows host IP, using localhost")
	return "127.0.0.1"
}

// ExecutePowerShellScript executes a PowerShell script with given arguments
func ExecutePowerShellScript(t *testing.T, scriptPath string, args []string, workingDir string) *exec.Cmd {
	t.Logf("Executing PowerShell script: %s with args: %v", scriptPath, args)

	var psExe string
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err == nil {
			psExe = "pwsh"
		} else {
			psExe = "powershell"
		}
	} else {
		psExe = "pwsh"
	}

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

	cmd.Env = append(os.Environ(), "POWERSHELL_TELEMETRY_OPTOUT=1")
	t.Logf("PowerShell command: %s %s", psExe, strings.Join(psArgs, " "))
	return cmd
}

// CleanupWindowsService cleans up a Windows service
func CleanupWindowsService(t *testing.T, serviceName string) {
	t.Logf("Cleaning up Windows service: %s", serviceName)

	cmd := exec.Command("sc", "stop", serviceName)
	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to stop service %s: %v", serviceName, err)
	}

	cmd = exec.Command("sc", "delete", serviceName)
	err = cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to delete service %s: %v", serviceName, err)
	}

	t.Logf("Windows service %s cleanup completed", serviceName)
}

// Test function
func TestSimpleWindowsRemoteAgentSetup(t *testing.T) {
	t.Logf("Starting simple Windows remote agent setup test")

	// Get project root
	projectRoot := GetWindowsProjectRoot(t)
	t.Logf("Project root: %s", projectRoot)

	// Create test directory
	testDir := CreateWindowsTestDirectory(t)
	defer func() {
		if err := os.RemoveAll(testDir); err != nil {
			t.Logf("Warning: Failed to clean up test directory %s: %v", testDir, err)
		}
	}()

	// Generate certificates
	certs := GenerateWindowsCertificates(t, testDir)
	t.Logf("Generated certificates:")
	t.Logf("  CA Cert: %s", certs.CACert)
	t.Logf("  Client Cert: %s", certs.ClientCert)
	t.Logf("  Client Key: %s", certs.ClientKey)

	// Verify certificates exist
	if !FileExistsWindows(certs.ClientCert) {
		t.Errorf("Client certificate should exist: %s", certs.ClientCert)
	}
	if !FileExistsWindows(certs.ClientKey) {
		t.Errorf("Client key should exist: %s", certs.ClientKey)
	}
	if !FileExistsWindows(certs.CACert) {
		t.Errorf("CA certificate should exist: %s", certs.CACert)
	}

	// Create HTTP config
	symphonyURL := "https://localhost:8080"
	configPath := CreateHTTPConfigWindows(t, testDir, symphonyURL)
	t.Logf("Created HTTP config: %s", configPath)

	// Create topology
	topologyPath := CreateTestTopologyWindows(t, testDir)
	t.Logf("Created topology: %s", topologyPath)

	// Test Windows environment
	if !IsRunningOnWindows() {
		t.Skip("Skipping Windows-specific test on non-Windows platform")
	}

	// Test PowerShell execution
	scriptContent := `Write-Output "PowerShell test successful"`
	scriptPath := filepath.Join(testDir, "test_script.ps1")
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0644)
	if err != nil {
		t.Errorf("Should be able to write test script: %v", err)
		return
	}

	cmd := ExecutePowerShellScript(t, scriptPath, []string{}, testDir)
	if cmd == nil {
		t.Errorf("PowerShell command should not be nil")
		return
	}

	// Test timeout for PowerShell execution
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = cmd.Start()
	if err != nil {
		t.Errorf("PowerShell script should start successfully: %v", err)
		return
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		t.Errorf("PowerShell script timed out")
	case err := <-done:
		if err != nil {
			t.Errorf("PowerShell script should complete successfully: %v", err)
		} else {
			t.Logf("PowerShell script completed successfully")
		}
	}

	// Get Windows host IP
	hostIP := GetWindowsHostIP(t)
	t.Logf("Windows host IP: %s", hostIP)

	// Test Windows path conversion
	windowsPath := ConvertToWindowsPath("test/path/file.txt")
	t.Logf("Converted path: %s", windowsPath)

	// Check if bootstrap.ps1 exists
	bootstrapPath := filepath.Join(projectRoot, "remote-agent", "bootstrap", "bootstrap.ps1")
	if FileExistsWindows(bootstrapPath) {
		t.Logf("Bootstrap script found at: %s", bootstrapPath)
	} else {
		t.Logf("Bootstrap script not found at: %s (this is expected in test environment)", bootstrapPath)
	}

	t.Logf("Simple Windows remote agent setup test completed successfully")
}
