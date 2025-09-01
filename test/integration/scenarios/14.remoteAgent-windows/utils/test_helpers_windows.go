package utils

import (
	"bytes"
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

	// For Windows testing, we'll use PEM files for now
	// In a real Windows environment, PFX would be generated using openssl or PowerShell
	password := "test123"

	// For the testing phase, we'll use the PEM certificate as ClientCert
	// The bootstrap.ps1 script can handle both PEM and PFX formats
	pfxPath := clientCertPEMPath // Use PEM file for now

	t.Logf("Generated Windows certificates:")
	t.Logf("  CA Certificate: %s", caCertPath)
	t.Logf("  Client Certificate (PEM): %s", clientCertPEMPath)
	t.Logf("  Client Key: %s", clientKeyPath)
	t.Logf("  Note: Using PEM format for testing. In production, PFX would be preferred for Windows HTTP mode.")

	return WindowsCertificatePaths{
		CACert:     caCertPath,
		ClientCert: pfxPath, // Points to PEM file for testing
		ClientKey:  clientKeyPath,
		ClientPEM:  clientCertPEMPath,
		Password:   password,
	}
}

// BuildWindowsRemoteAgent builds the remote agent binary for Windows
func BuildWindowsRemoteAgent(t *testing.T, config WindowsTestConfig) string {
	binaryPath := filepath.Join(config.ProjectRoot, "remote-agent", "bootstrap", "remote-agent.exe")

	t.Logf("Building Windows remote agent binary at: %s", binaryPath)

	// Build the binary: GOOS=windows GOARCH=amd64 go build -o bootstrap/remote-agent.exe
	buildCmd := exec.Command("go", "build", "-o", "bootstrap/remote-agent.exe", ".")
	buildCmd.Dir = filepath.Join(config.ProjectRoot, "remote-agent")
	buildCmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64")

	var stdout, stderr bytes.Buffer
	buildCmd.Stdout = &stdout
	buildCmd.Stderr = &stderr

	err := buildCmd.Run()
	if err != nil {
		t.Logf("Build stdout: %s", stdout.String())
		t.Logf("Build stderr: %s", stderr.String())
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

// ExecutePowerShellScript executes a PowerShell script with given arguments
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

	// Execute bootstrap.ps1
	cmd := ExecutePowerShellScript(t, bootstrapPath, args, filepath.Join(config.ProjectRoot, "remote-agent", "bootstrap"))

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
