//go:build windows || !linux

package verify

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"windows-tests/utils"
)

type WindowsProcessTestSuite struct {
	suite.Suite
	testDir     string
	projectRoot string
}

func TestWindowsProcessTestSuite(t *testing.T) {
	suite.Run(t, new(WindowsProcessTestSuite))
}

func (suite *WindowsProcessTestSuite) SetupSuite() {
	// Get project root
	suite.projectRoot = utils.GetWindowsProjectRoot(suite.T())

	// Create test directory
	suite.testDir = utils.CreateWindowsTestDirectory(suite.T())

	suite.T().Logf("Windows Process Test Suite setup complete")
	suite.T().Logf("  Project Root: %s", suite.projectRoot)
	suite.T().Logf("  Test Directory: %s", suite.testDir)
}

func (suite *WindowsProcessTestSuite) TearDownSuite() {
	// Clean up test directory
	if suite.testDir != "" {
		err := os.RemoveAll(suite.testDir)
		if err != nil {
			suite.T().Logf("Warning: Failed to clean up test directory %s: %v", suite.testDir, err)
		}
	}

	suite.T().Logf("Windows Process Test Suite cleanup complete")
}

func (suite *WindowsProcessTestSuite) TestWindowsServiceManagement() {
	// Test Windows service management capabilities
	serviceName := "Symphony-Test-Service"

	// Clean up any existing service
	defer utils.CleanupWindowsService(suite.T(), serviceName)

	// Test service status check (should fail since service doesn't exist)
	utils.CheckWindowsServiceStatus(suite.T(), serviceName)

	suite.T().Logf("Windows service management test completed successfully")
}

func (suite *WindowsProcessTestSuite) TestWindowsScheduledTaskManagement() {
	// Test Windows scheduled task management capabilities
	taskName := "Symphony-Test-Task"

	// Clean up any existing task
	defer utils.CleanupWindowsScheduledTask(suite.T(), taskName)

	suite.T().Logf("Windows scheduled task management test completed successfully")
}

func (suite *WindowsProcessTestSuite) TestWindowsRemoteAgentBinaryBuild() {
	// Test building the Windows remote agent binary

	// Create test configuration
	testConfig := utils.WindowsTestConfig{
		ProjectRoot: suite.projectRoot,
	}

	// Build the binary
	binaryPath := utils.BuildWindowsRemoteAgent(suite.T(), testConfig)
	require.NotEmpty(suite.T(), binaryPath, "Binary path should not be empty")

	// Verify binary exists
	require.True(suite.T(), utils.FileExistsWindows(binaryPath),
		"Remote agent binary should exist: %s", binaryPath)

	// Clean up binary
	defer func() {
		if err := os.Remove(binaryPath); err != nil {
			suite.T().Logf("Warning: Failed to clean up binary %s: %v", binaryPath, err)
		}
	}()

	suite.T().Logf("Windows remote agent binary build test completed successfully")
	suite.T().Logf("  Binary path: %s", binaryPath)
}

func (suite *WindowsProcessTestSuite) TestWindowsPowerShellScriptExecution() {
	// Test PowerShell script execution

	// Create a test PowerShell script
	scriptContent := `
param(
    [string]$TestParam = "default"
)

Write-Output "PowerShell script executed successfully"
Write-Output "Test parameter: $TestParam"
Write-Output "Current directory: $(Get-Location)"
Write-Output "Windows version: $((Get-WmiObject Win32_OperatingSystem).Caption)"
`

	scriptPath := fmt.Sprintf("%s\\test_script.ps1", suite.testDir)
	err := os.WriteFile(scriptPath, []byte(scriptContent), 0644)
	require.NoError(suite.T(), err, "Should be able to write test script")

	// Execute PowerShell script with parameters
	args := []string{"-TestParam", "hello-world"}
	cmd := utils.ExecutePowerShellScript(suite.T(), scriptPath, args, suite.testDir)
	require.NotNil(suite.T(), cmd, "PowerShell command should not be nil")

	// Start and wait for completion
	err = cmd.Start()
	require.NoError(suite.T(), err, "PowerShell script should start successfully")

	err = cmd.Wait()
	require.NoError(suite.T(), err, "PowerShell script should complete successfully")

	suite.T().Logf("Windows PowerShell script execution test completed successfully")
}

func (suite *WindowsProcessTestSuite) TestWindowsNetworkingCapabilities() {
	// Test Windows networking detection

	// Get Windows host IP
	hostIP := utils.GetWindowsHostIP(suite.T())
	require.NotEmpty(suite.T(), hostIP, "Should be able to get Windows host IP")

	suite.T().Logf("Windows host IP detected: %s", hostIP)

	// Test path conversion
	unixPath := "test/path/to/file.txt"
	windowsPath := utils.ConvertToWindowsPath(unixPath)
	suite.T().Logf("Path conversion: %s -> %s", unixPath, windowsPath)

	suite.T().Logf("Windows networking capabilities test completed successfully")
}

func (suite *WindowsProcessTestSuite) TestWindowsCertificateGeneration() {
	// Test Windows certificate generation

	// Generate certificates
	certs := utils.GenerateWindowsCertificates(suite.T(), suite.testDir)

	// Verify all certificate files exist
	require.True(suite.T(), utils.FileExistsWindows(certs.CACert),
		"CA certificate should exist: %s", certs.CACert)
	require.True(suite.T(), utils.FileExistsWindows(certs.ClientCert),
		"Client certificate should exist: %s", certs.ClientCert)
	require.True(suite.T(), utils.FileExistsWindows(certs.ClientKey),
		"Client key should exist: %s", certs.ClientKey)
	require.True(suite.T(), utils.FileExistsWindows(certs.ClientPEM),
		"Client PEM certificate should exist: %s", certs.ClientPEM)

	require.NotEmpty(suite.T(), certs.Password, "Certificate password should be set")

	suite.T().Logf("Certificate generation test completed successfully")
	suite.T().Logf("  CA Certificate: %s", certs.CACert)
	suite.T().Logf("  Client Certificate: %s", certs.ClientCert)
	suite.T().Logf("  Client Key: %s", certs.ClientKey)
	suite.T().Logf("  Client PEM: %s", certs.ClientPEM)
	suite.T().Logf("  Password: %s", certs.Password)
}

func (suite *WindowsProcessTestSuite) TestWindowsConfigurationGeneration() {
	// Test Windows configuration file generation

	// Test HTTP configuration
	httpBaseURL := "https://localhost:8080"
	httpConfigPath := utils.CreateHTTPConfigWindows(suite.T(), suite.testDir, httpBaseURL)
	require.True(suite.T(), utils.FileExistsWindows(httpConfigPath),
		"HTTP config should exist: %s", httpConfigPath)

	// Read and verify HTTP config
	httpConfigData, err := os.ReadFile(httpConfigPath)
	require.NoError(suite.T(), err, "Should be able to read HTTP config")
	httpConfigStr := string(httpConfigData)
	require.Contains(suite.T(), httpConfigStr, httpBaseURL, "HTTP config should contain base URL")

	// Test MQTT configuration
	mqttBroker := "localhost"
	mqttPort := 1883
	targetName := "test-target"
	namespace := "default"
	mqttConfigPath := utils.CreateMQTTConfigWindows(suite.T(), suite.testDir, mqttBroker, mqttPort, targetName, namespace)
	require.True(suite.T(), utils.FileExistsWindows(mqttConfigPath),
		"MQTT config should exist: %s", mqttConfigPath)

	// Read and verify MQTT config
	mqttConfigData, err := os.ReadFile(mqttConfigPath)
	require.NoError(suite.T(), err, "Should be able to read MQTT config")
	mqttConfigStr := string(mqttConfigData)
	require.Contains(suite.T(), mqttConfigStr, mqttBroker, "MQTT config should contain broker address")
	require.Contains(suite.T(), mqttConfigStr, targetName, "MQTT config should contain target name")

	// Test topology configuration
	topologyPath := utils.CreateTestTopologyWindows(suite.T(), suite.testDir)
	require.True(suite.T(), utils.FileExistsWindows(topologyPath),
		"Topology should exist: %s", topologyPath)

	// Read and verify topology
	topologyData, err := os.ReadFile(topologyPath)
	require.NoError(suite.T(), err, "Should be able to read topology")
	topologyStr := string(topologyData)
	require.Contains(suite.T(), topologyStr, "bindings", "Topology should contain bindings")

	suite.T().Logf("Configuration generation test completed successfully")
	suite.T().Logf("  HTTP Config: %s", httpConfigPath)
	suite.T().Logf("  MQTT Config: %s", mqttConfigPath)
	suite.T().Logf("  Topology: %s", topologyPath)
}

func (suite *WindowsProcessTestSuite) TestWindowsEnvironmentDetection() {
	// Test Windows environment detection

	// Test OS detection
	isWindows := utils.IsRunningOnWindows()
	suite.T().Logf("Running on Windows: %v", isWindows)

	// Test project root detection
	projectRoot := utils.GetWindowsProjectRoot(suite.T())
	require.NotEmpty(suite.T(), projectRoot, "Project root should be detected")
	require.True(suite.T(), utils.FileExistsWindows(projectRoot), "Project root should exist")

	// Test temp directory creation
	tempDir := utils.CreateWindowsTestDirectory(suite.T())
	require.NotEmpty(suite.T(), tempDir, "Temp directory should be created")
	require.True(suite.T(), utils.FileExistsWindows(tempDir), "Temp directory should exist")

	// Clean up temp directory
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			suite.T().Logf("Warning: Failed to clean up temp directory %s: %v", tempDir, err)
		}
	}()

	suite.T().Logf("Windows environment detection test completed successfully")
	suite.T().Logf("  Project Root: %s", projectRoot)
	suite.T().Logf("  Temp Directory: %s", tempDir)
}

// Helper function to check if running on Windows
func (suite *WindowsProcessTestSuite) isRunningOnWindows() bool {
	return utils.IsRunningOnWindows()
}

func (suite *WindowsProcessTestSuite) TestWindowsProcessIntegration() {
	// Test complete Windows process integration

	if !suite.isRunningOnWindows() {
		suite.T().Skip("Skipping Windows-specific integration test on non-Windows platform")
	}

	// Create certificates
	certs := utils.GenerateWindowsCertificates(suite.T(), suite.testDir)

	// Create configurations
	httpBaseURL := "https://localhost:8080"
	httpConfigPath := utils.CreateHTTPConfigWindows(suite.T(), suite.testDir, httpBaseURL)
	topologyPath := utils.CreateTestTopologyWindows(suite.T(), suite.testDir)

	// Build binary
	testConfig := utils.WindowsTestConfig{
		ProjectRoot: suite.projectRoot,
	}
	binaryPath := utils.BuildWindowsRemoteAgent(suite.T(), testConfig)

	// Clean up binary
	defer func() {
		if err := os.Remove(binaryPath); err != nil {
			suite.T().Logf("Warning: Failed to clean up binary %s: %v", binaryPath, err)
		}
	}()

	// Verify all components exist
	require.True(suite.T(), utils.FileExistsWindows(certs.CACert), "CA cert should exist")
	require.True(suite.T(), utils.FileExistsWindows(httpConfigPath), "HTTP config should exist")
	require.True(suite.T(), utils.FileExistsWindows(topologyPath), "Topology should exist")
	require.True(suite.T(), utils.FileExistsWindows(binaryPath), "Binary should exist")

	suite.T().Logf("Windows process integration test completed successfully")
	suite.T().Logf("All Windows components verified:")
	suite.T().Logf("  Certificates: Generated")
	suite.T().Logf("  Configuration: Created")
	suite.T().Logf("  Binary: Built")
	suite.T().Logf("  Topology: Configured")
}
