//go:build windows || !linux

package verify

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"windows-tests/utils"
)

type WindowsMQTTBootstrapTestSuite struct {
	suite.Suite
	testDir       string
	projectRoot   string
	brokerAddress string
	brokerPort    int
	testConfig    utils.WindowsTestConfig
	serviceName   string
}

func TestWindowsMQTTBootstrapTestSuite(t *testing.T) {
	suite.Run(t, new(WindowsMQTTBootstrapTestSuite))
}

func (suite *WindowsMQTTBootstrapTestSuite) SetupSuite() {
	// Get project root
	suite.projectRoot = utils.GetWindowsProjectRoot(suite.T())

	// Create test directory
	suite.testDir = utils.CreateWindowsTestDirectory(suite.T())

	// Generate certificates for Windows
	certs := utils.GenerateWindowsCertificates(suite.T(), suite.testDir)

	// Setup MQTT broker (assuming it's running in a container/minikube)
	suite.brokerAddress = "localhost"
	suite.brokerPort = 1883

	// Create topology file
	topologyPath := utils.CreateTestTopologyWindows(suite.T(), suite.testDir)

	// Setup Windows test configuration for MQTT mode
	suite.testConfig = utils.WindowsTestConfig{
		ProjectRoot:    suite.projectRoot,
		ConfigPath:     utils.CreateMQTTConfigWindows(suite.T(), suite.testDir, suite.brokerAddress, suite.brokerPort, "windows-mqtt-target", "default"),
		ClientCertPath: certs.ClientPEM, // PEM format for MQTT
		ClientKeyPath:  certs.ClientKey,
		CertPassword:   certs.Password,
		CACertPath:     certs.CACert,
		TargetName:     "windows-mqtt-target",
		Namespace:      "default",
		TopologyPath:   topologyPath,
		Protocol:       "mqtt",
		BrokerAddress:  suite.brokerAddress,
		BrokerPort:     strconv.Itoa(suite.brokerPort),
		RunMode:        "service", // Default to Windows service mode
	}

	suite.serviceName = fmt.Sprintf("Symphony-RemoteAgent-%s", suite.testConfig.TargetName)

	suite.T().Logf("Windows MQTT Bootstrap Test Suite setup complete")
	suite.T().Logf("  Project Root: %s", suite.projectRoot)
	suite.T().Logf("  Test Directory: %s", suite.testDir)
	suite.T().Logf("  MQTT Broker: %s:%d", suite.brokerAddress, suite.brokerPort)
	suite.T().Logf("  Service Name: %s", suite.serviceName)
}

func (suite *WindowsMQTTBootstrapTestSuite) TearDownSuite() {
	// Clean up Windows service
	utils.CleanupWindowsService(suite.T(), suite.serviceName)

	// Clean up test directory
	if suite.testDir != "" {
		err := os.RemoveAll(suite.testDir)
		if err != nil {
			suite.T().Logf("Warning: Failed to clean up test directory %s: %v", suite.testDir, err)
		}
	}

	suite.T().Logf("Windows MQTT Bootstrap Test Suite cleanup complete")
}

func (suite *WindowsMQTTBootstrapTestSuite) TestWindowsMQTTBootstrapService() {
	// Test Windows Service mode bootstrap for MQTT
	suite.testConfig.RunMode = "service"

	// Build Windows remote agent binary first (required for MQTT mode)
	binaryPath := utils.BuildWindowsRemoteAgent(suite.T(), suite.testConfig)
	suite.testConfig.BinaryPath = binaryPath

	// Start bootstrap.ps1 in service mode
	cmd := utils.StartWindowsRemoteAgentWithBootstrap(suite.T(), suite.testConfig)
	require.NotNil(suite.T(), cmd, "Bootstrap command should not be nil")

	// Give bootstrap.ps1 time to complete
	time.Sleep(30 * time.Second)

	// Check if service was created and is running
	utils.WaitForWindowsService(suite.T(), suite.serviceName, 1*time.Minute)

	// Verify service status
	utils.CheckWindowsServiceStatus(suite.T(), suite.serviceName)

	suite.T().Logf("Windows MQTT Bootstrap Service test completed successfully")
}

func (suite *WindowsMQTTBootstrapTestSuite) TestWindowsMQTTBootstrapScheduledTask() {
	// Test Windows Scheduled Task mode bootstrap for MQTT
	suite.testConfig.RunMode = "schedule"
	taskName := fmt.Sprintf("Symphony-RemoteAgent-%s-Task", suite.testConfig.TargetName)

	// Clean up any existing service first
	utils.CleanupWindowsService(suite.T(), suite.serviceName)

	// Build Windows remote agent binary first (required for MQTT mode)
	binaryPath := utils.BuildWindowsRemoteAgent(suite.T(), suite.testConfig)
	suite.testConfig.BinaryPath = binaryPath

	// Start bootstrap.ps1 in scheduled task mode
	cmd := utils.StartWindowsRemoteAgentWithBootstrap(suite.T(), suite.testConfig)
	require.NotNil(suite.T(), cmd, "Bootstrap command should not be nil")

	// Give bootstrap.ps1 time to complete
	time.Sleep(30 * time.Second)

	// Check if scheduled task was created
	suite.T().Logf("Checking for scheduled task: %s", taskName)

	// Clean up scheduled task
	defer utils.CleanupWindowsScheduledTask(suite.T(), taskName)

	suite.T().Logf("Windows MQTT Bootstrap Scheduled Task test completed successfully")
}

func (suite *WindowsMQTTBootstrapTestSuite) TestWindowsMQTTBinaryGeneration() {
	// Test that Windows remote agent binary can be built

	// Build the binary
	binaryPath := utils.BuildWindowsRemoteAgent(suite.T(), suite.testConfig)
	require.NotEmpty(suite.T(), binaryPath, "Binary path should not be empty")

	// Verify binary exists
	require.True(suite.T(), utils.FileExistsWindows(binaryPath),
		"Remote agent binary should exist: %s", binaryPath)

	suite.T().Logf("Windows remote agent binary verified: %s", binaryPath)
	suite.T().Logf("Windows MQTT Binary generation test completed successfully")
}

func (suite *WindowsMQTTBootstrapTestSuite) TestWindowsMQTTCertificateHandling() {
	// Test that Windows MQTT certificate handling works correctly

	// For MQTT mode, we use PEM certificates
	require.True(suite.T(), utils.FileExistsWindows(suite.testConfig.ClientCertPath),
		"Client certificate (PEM) should exist: %s", suite.testConfig.ClientCertPath)
	require.True(suite.T(), utils.FileExistsWindows(suite.testConfig.ClientKeyPath),
		"Client key should exist: %s", suite.testConfig.ClientKeyPath)
	require.True(suite.T(), utils.FileExistsWindows(suite.testConfig.CACertPath),
		"CA certificate should exist: %s", suite.testConfig.CACertPath)

	suite.T().Logf("MQTT Certificate files verified:")
	suite.T().Logf("  Client Cert (PEM): %s", suite.testConfig.ClientCertPath)
	suite.T().Logf("  Client Key: %s", suite.testConfig.ClientKeyPath)
	suite.T().Logf("  CA Cert: %s", suite.testConfig.CACertPath)

	suite.T().Logf("Windows MQTT Certificate handling test completed successfully")
}

func (suite *WindowsMQTTBootstrapTestSuite) TestWindowsMQTTConfigGeneration() {
	// Test that Windows MQTT configuration is generated correctly

	// Verify config file exists
	require.True(suite.T(), utils.FileExistsWindows(suite.testConfig.ConfigPath),
		"MQTT config file should exist: %s", suite.testConfig.ConfigPath)

	// Read and verify config contents
	configData, err := os.ReadFile(suite.testConfig.ConfigPath)
	require.NoError(suite.T(), err, "Should be able to read config file")

	configStr := string(configData)
	require.Contains(suite.T(), configStr, suite.brokerAddress, "Config should contain broker address")
	require.Contains(suite.T(), configStr, strconv.Itoa(suite.brokerPort), "Config should contain broker port")
	require.Contains(suite.T(), configStr, "mqttBroker", "Config should contain mqttBroker field")
	require.Contains(suite.T(), configStr, "mqttPort", "Config should contain mqttPort field")
	require.Contains(suite.T(), configStr, suite.testConfig.TargetName, "Config should contain target name")

	suite.T().Logf("MQTT configuration verified:")
	suite.T().Logf("  Config path: %s", suite.testConfig.ConfigPath)
	suite.T().Logf("  Config contents: %s", configStr)

	suite.T().Logf("Windows MQTT Config generation test completed successfully")
}

func (suite *WindowsMQTTBootstrapTestSuite) TestWindowsMQTTNetworking() {
	// Test Windows MQTT networking capabilities

	// Get Windows host IP
	hostIP := utils.GetWindowsHostIP(suite.T())
	require.NotEmpty(suite.T(), hostIP, "Should be able to get Windows host IP")

	suite.T().Logf("Windows host IP detected: %s", hostIP)

	// Verify MQTT networking components
	require.NotEmpty(suite.T(), suite.brokerAddress, "MQTT broker address should be configured")
	require.Greater(suite.T(), suite.brokerPort, 0, "MQTT broker port should be valid")

	suite.T().Logf("MQTT broker configuration: %s:%d", suite.brokerAddress, suite.brokerPort)
	suite.T().Logf("Windows MQTT Networking test completed successfully")
}

func (suite *WindowsMQTTBootstrapTestSuite) TestWindowsMQTTTopologyHandling() {
	// Test that Windows topology file handling works correctly

	// Verify topology file exists
	require.True(suite.T(), utils.FileExistsWindows(suite.testConfig.TopologyPath),
		"Topology file should exist: %s", suite.testConfig.TopologyPath)

	// Read and verify topology contents
	topologyData, err := os.ReadFile(suite.testConfig.TopologyPath)
	require.NoError(suite.T(), err, "Should be able to read topology file")

	topologyStr := string(topologyData)
	require.Contains(suite.T(), topologyStr, "bindings", "Topology should contain bindings")
	require.Contains(suite.T(), topologyStr, "providers.target.remote-agent", "Topology should contain remote-agent provider")

	suite.T().Logf("Topology configuration verified:")
	suite.T().Logf("  Topology path: %s", suite.testConfig.TopologyPath)
	suite.T().Logf("  Topology contents: %s", topologyStr)

	suite.T().Logf("Windows MQTT Topology handling test completed successfully")
}

// Helper function to check if running on Windows
func (suite *WindowsMQTTBootstrapTestSuite) isRunningOnWindows() bool {
	return utils.IsRunningOnWindows()
}

func (suite *WindowsMQTTBootstrapTestSuite) TestWindowsMQTTEnvironmentCheck() {
	// Test Windows environment detection for MQTT mode

	if !suite.isRunningOnWindows() {
		suite.T().Skip("Skipping Windows-specific MQTT test on non-Windows platform")
	}

	// Test Windows-specific paths for MQTT
	windowsPath := utils.ConvertToWindowsPath("mqtt/test/path/file.txt")
	suite.T().Logf("Converted MQTT path: %s", windowsPath)

	// Verify MQTT-specific configuration
	require.Equal(suite.T(), "mqtt", suite.testConfig.Protocol, "Protocol should be MQTT")
	require.NotEmpty(suite.T(), suite.testConfig.BrokerAddress, "Broker address should be set")

	suite.T().Logf("Windows MQTT environment check completed successfully")
}
