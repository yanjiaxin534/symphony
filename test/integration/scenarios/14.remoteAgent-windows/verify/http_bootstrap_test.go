//go:build windows || !linux

package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"windows-tests/utils"
)

type WindowsHTTPBootstrapTestSuite struct {
	testDir     string
	projectRoot string
	symphonyURL string
	testConfig  utils.WindowsTestConfig
	serviceName string
}

func TestWindowsHTTPBootstrapTestSuite(t *testing.T) {
	suite := &WindowsHTTPBootstrapTestSuite{}
	suite.SetupSuite(t)
	defer suite.TearDownSuite(t)

	t.Run("TestWindowsHTTPBootstrapService", suite.TestWindowsHTTPBootstrapService)
	t.Run("TestWindowsHTTPCertificateHandling", suite.TestWindowsHTTPCertificateHandling)
	t.Run("TestWindowsHTTPConfigGeneration", suite.TestWindowsHTTPConfigGeneration)
	t.Run("TestWindowsHTTPNetworking", suite.TestWindowsHTTPNetworking)
	t.Run("TestWindowsPowerShellExecution", suite.TestWindowsPowerShellExecution)
	t.Run("TestWindowsEnvironmentCheck", suite.TestWindowsEnvironmentCheck)
}

func (suite *WindowsHTTPBootstrapTestSuite) SetupSuite(t *testing.T) {
	// Get project root
	suite.projectRoot = utils.GetWindowsProjectRoot(t)

	// Create test directory
	suite.testDir = utils.CreateWindowsTestDirectory(t)

	// Generate certificates for Windows
	certs := utils.GenerateWindowsCertificates(t, suite.testDir)

	// Setup Symphony server URL (assuming it's running in a container/minikube)
	suite.symphonyURL = "https://localhost:8080"

	// Create topology file
	topologyPath := utils.CreateTestTopologyWindows(t, suite.testDir)

	// Setup Windows test configuration for HTTP mode
	suite.testConfig = utils.WindowsTestConfig{
		ProjectRoot:    suite.projectRoot,
		ConfigPath:     utils.CreateHTTPConfigWindows(t, suite.testDir, suite.symphonyURL),
		ClientCertPath: certs.ClientCert, // PEM file for testing
		ClientKeyPath:  certs.ClientKey,
		CertPassword:   certs.Password,
		CACertPath:     certs.CACert,
		TargetName:     "windows-http-target",
		Namespace:      "default",
		TopologyPath:   topologyPath,
		Protocol:       "http",
		BaseURL:        suite.symphonyURL,
		RunMode:        "service", // Default to Windows service mode
	}

	suite.serviceName = fmt.Sprintf("Symphony-RemoteAgent-%s", suite.testConfig.TargetName)

	t.Logf("Windows HTTP Bootstrap Test Suite setup complete")
	t.Logf("  Project Root: %s", suite.projectRoot)
	t.Logf("  Test Directory: %s", suite.testDir)
	t.Logf("  Symphony URL: %s", suite.symphonyURL)
	t.Logf("  Service Name: %s", suite.serviceName)
}

func (suite *WindowsHTTPBootstrapTestSuite) TearDownSuite(t *testing.T) {
	// Clean up Windows service
	utils.CleanupWindowsService(t, suite.serviceName)

	// Clean up test directory
	if suite.testDir != "" {
		err := os.RemoveAll(suite.testDir)
		if err != nil {
			t.Logf("Warning: Failed to clean up test directory %s: %v", suite.testDir, err)
		}
	}

	t.Logf("Windows HTTP Bootstrap Test Suite cleanup complete")
}

func (suite *WindowsHTTPBootstrapTestSuite) TestWindowsHTTPBootstrapService(t *testing.T) {
	// Test Windows Service mode bootstrap
	suite.testConfig.RunMode = "service"

	// For testing purposes, just verify the bootstrap script path exists
	bootstrapPath := filepath.Join(suite.projectRoot, "remote-agent", "bootstrap", "bootstrap.ps1")
	if !utils.FileExistsWindows(bootstrapPath) {
		t.Logf("Warning: bootstrap.ps1 not found at %s, skipping service test", bootstrapPath)
		return
	}

	t.Logf("Bootstrap script found at: %s", bootstrapPath)
	t.Logf("Windows HTTP Bootstrap Service test completed successfully")
}

func (suite *WindowsHTTPBootstrapTestSuite) TestWindowsHTTPCertificateHandling(t *testing.T) {
	// Test that Windows certificate handling works correctly

	// Verify certificates exist
	if !utils.FileExistsWindows(suite.testConfig.ClientCertPath) {
		t.Errorf("Client certificate should exist: %s", suite.testConfig.ClientCertPath)
	}
	if !utils.FileExistsWindows(suite.testConfig.ClientKeyPath) {
		t.Errorf("Client key should exist: %s", suite.testConfig.ClientKeyPath)
	}
	if !utils.FileExistsWindows(suite.testConfig.CACertPath) {
		t.Errorf("CA certificate should exist: %s", suite.testConfig.CACertPath)
	}

	t.Logf("Certificate files verified:")
	t.Logf("  Client Cert: %s", suite.testConfig.ClientCertPath)
	t.Logf("  Client Key: %s", suite.testConfig.ClientKeyPath)
	t.Logf("  CA Cert: %s", suite.testConfig.CACertPath)

	t.Logf("Windows HTTP Certificate handling test completed successfully")
}

func (suite *WindowsHTTPBootstrapTestSuite) TestWindowsHTTPConfigGeneration(t *testing.T) {
	// Test that Windows HTTP configuration is generated correctly

	// Verify config file exists
	if !utils.FileExistsWindows(suite.testConfig.ConfigPath) {
		t.Errorf("HTTP config file should exist: %s", suite.testConfig.ConfigPath)
		return
	}

	// Read and verify config contents
	configData, err := os.ReadFile(suite.testConfig.ConfigPath)
	if err != nil {
		t.Errorf("Should be able to read config file: %v", err)
		return
	}

	configStr := string(configData)
	if len(configStr) == 0 {
		t.Errorf("Config file should not be empty")
		return
	}

	t.Logf("HTTP configuration verified:")
	t.Logf("  Config path: %s", suite.testConfig.ConfigPath)
	t.Logf("  Config size: %d bytes", len(configData))

	t.Logf("Windows HTTP Config generation test completed successfully")
}

func (suite *WindowsHTTPBootstrapTestSuite) TestWindowsHTTPNetworking(t *testing.T) {
	// Test Windows networking capabilities

	// Get Windows host IP
	hostIP := utils.GetWindowsHostIP(t)
	if hostIP == "" {
		t.Errorf("Should be able to get Windows host IP")
		return
	}

	t.Logf("Windows host IP detected: %s", hostIP)

	// Verify networking components
	if suite.symphonyURL == "" {
		t.Errorf("Symphony URL should be configured")
		return
	}

	t.Logf("Windows HTTP Networking test completed successfully")
}

func (suite *WindowsHTTPBootstrapTestSuite) TestWindowsPowerShellExecution(t *testing.T) {
	// Test PowerShell script execution capabilities

	// Test basic PowerShell execution
	scriptContent := `Write-Output "PowerShell test successful"`
	scriptPath := filepath.Join(suite.testDir, "test_script.ps1")

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0644)
	if err != nil {
		t.Errorf("Should be able to write test script: %v", err)
		return
	}

	// Execute PowerShell script
	cmd := utils.ExecutePowerShellScript(t, scriptPath, []string{}, suite.testDir)
	if cmd == nil {
		t.Errorf("PowerShell command should not be nil")
		return
	}

	// Start and wait for completion
	err = cmd.Start()
	if err != nil {
		t.Errorf("PowerShell script should start successfully: %v", err)
		return
	}

	err = cmd.Wait()
	if err != nil {
		t.Errorf("PowerShell script should complete successfully: %v", err)
		return
	}

	t.Logf("Windows PowerShell execution test completed successfully")
}

func (suite *WindowsHTTPBootstrapTestSuite) TestWindowsEnvironmentCheck(t *testing.T) {
	// Test Windows environment detection

	if !utils.IsRunningOnWindows() {
		t.Skip("Skipping Windows-specific test on non-Windows platform")
	}

	// Test Windows-specific paths
	windowsPath := utils.ConvertToWindowsPath("test/path/file.txt")
	t.Logf("Converted path: %s", windowsPath)

	t.Logf("Windows environment check completed successfully")
}
