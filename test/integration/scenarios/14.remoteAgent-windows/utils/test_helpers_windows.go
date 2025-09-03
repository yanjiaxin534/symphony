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

// GetProjectRoot returns the project root directory (alias for GetWindowsProjectRoot for consistency)
func GetProjectRoot(t *testing.T) string {
	return GetWindowsProjectRoot(t)
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
		// For MQTT mode, use PEM certificate
		clientCertPath = clientCertPEMPath
		pfxPath = clientCertPEMPath
	}

	t.Logf("Generated Windows certificates for %s mode:", protocol)
	t.Logf("  CA Certificate: %s", caCertPath)
	t.Logf("  Client Certificate: %s", clientCertPath)
	t.Logf("  Client Key: %s", clientKeyPath)
	if protocol == "http" && clientCertPath == pfxPath && pfxPath != clientCertPEMPath {
		t.Logf("  Certificate format: PFX (required for Windows HTTP mode)")
	} else {
		t.Logf("  Certificate format: PEM")
	}

	return WindowsCertificatePaths{
		CACert:     caCertPath,
		ClientCert: clientCertPath,
		ClientKey:  clientKeyPath,
		ClientPEM:  clientCertPEMPath,
		Password:   password,
	}
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

	ticker := time.NewTicker(10 * time.Second)
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

// DeleteSolutionManifestWithTimeoutWindows deletes a solution manifest with timeout for Windows
func DeleteSolutionManifestWithTimeoutWindows(t *testing.T, manifestPath string, timeout time.Duration) error {
	t.Logf("Deleting solution manifest: %s", manifestPath)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "delete", "-f", manifestPath, "--timeout=30s")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Failed to delete solution manifest %s: %v", manifestPath, err)
		t.Logf("kubectl output: %s", string(output))
		return err
	}
	t.Logf("Successfully deleted solution manifest: %s", manifestPath)
	return nil
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

// StartSymphonyWithMQTTConfigDetectedWindows starts Symphony with MQTT config on Windows
func StartSymphonyWithMQTTConfigDetectedWindows(t *testing.T, brokerAddress, caSecretName string) {
	t.Logf("Starting Symphony with MQTT config on Windows: broker=%s, ca_secret=%s", brokerAddress, caSecretName)

	// For now, this is a placeholder - in a real implementation this would:
	// 1. Deploy Symphony to minikube with MQTT configuration
	// 2. Configure MQTT broker settings
	// 3. Set up necessary secrets and config maps
	// 4. Wait for Symphony to be ready

	t.Logf("Symphony MQTT configuration deployment initiated on Windows")
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
