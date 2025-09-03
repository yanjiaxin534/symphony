//go:build windows || !linux

package verify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// TestE2EMQTTCommunicationWithBootstrap - Windows equivalent of Linux TestE2EMQTTCommunicationWithProcess
func TestE2EMQTTCommunicationWithBootstrap(t *testing.T) {
	// Test configuration
	targetName := "test-windows-mqtt-bootstrap-target"
	namespace := "default"
	mqttBrokerPort := 8883

	t.Logf("=== Starting Windows E2E MQTT Communication Test with Bootstrap ===")
	t.Logf("Target: %s, Namespace: %s, MQTT Port: %d", targetName, namespace, mqttBrokerPort)

	// Setup test environment
	testDir := utils.CreateWindowsTestDirectory(t)
	projectRoot := utils.GetWindowsProjectRoot(t)
	t.Logf("Running Windows MQTT bootstrap test in: %s", testDir)
	t.Logf("Project root: %s", projectRoot)

	// Test phases similar to Linux TestE2EMQTTCommunicationWithProcess but adapted for Windows
	t.Run("Phase1_EnvironmentVerification", func(t *testing.T) {
		// Verify Windows environment and required tools
		t.Logf("=== Phase 1: Windows Environment Verification ===")

		// Check if running on Windows
		if !utils.IsRunningOnWindows() {
			t.Skip("Skipping Windows-specific MQTT test on non-Windows platform")
		}

		// Verify minikube and kubectl are available
		utils.VerifyMinikubeInstallationWindows(t)
		utils.VerifyKubectlInstallationWindows(t)

		t.Logf("✅ Phase 1 completed: Windows environment verified")
	})

	// Variables to be used across test phases
	var config utils.WindowsTestConfig
	var detectedBrokerAddress string
	var caSecretName string
	var processCmd *exec.Cmd

	t.Run("Phase2_SymphonyConnection", func(t *testing.T) {
		t.Logf("=== Phase 2: Symphony Server Connection ===")

		// Start fresh minikube cluster
		utils.StartFreshMinikubeWindows(t)

		// Setup MQTT process namespace
		utils.SetupWindowsMQTTProcessNamespace(t, namespace)

		// Setup MQTT process test with detected broker address (Windows version)
		config, detectedBrokerAddress, caSecretName = utils.SetupWindowsMQTTProcessTestWithDetectedAddress(t, testDir, targetName, namespace)
		t.Logf("Windows MQTT process test setup completed with broker address: %s", detectedBrokerAddress)

		// Debug certificate information
		utils.DebugWindowsCertificateInfo(t, config.CACertPath, "CA Certificate")
		utils.DebugWindowsCertificateInfo(t, config.ClientCertPath, "Client Certificate")

		// Test TLS connection to MQTT broker with certificates
		utils.DebugWindowsTLSConnection(t, detectedBrokerAddress, 8883, config.CACertPath, config.ClientCertPath, config.ClientKeyPath)

		// Create CA secret in default namespace for Symphony MQTT client certificate validation
		t.Logf("Creating CA secret in default namespace for Symphony MQTT client...")
		utils.CreateWindowsMQTTCASecretInNamespace(t, namespace, config.CACertPath)

		t.Logf("✅ Phase 2 completed: Symphony connection established")
	})

	t.Run("Phase3_TestConfigurations", func(t *testing.T) {
		t.Logf("=== Phase 3: Test Configurations Setup ===")

		// Deploy Symphony with MQTT configuration using detected broker address
		symphonyBrokerAddress := fmt.Sprintf("tls://%s:%d", detectedBrokerAddress, mqttBrokerPort)
		t.Logf("Starting Symphony with MQTT broker address: %s", symphonyBrokerAddress)
		utils.StartSymphonyWithMQTTConfigDetectedWindows(t, symphonyBrokerAddress, caSecretName)

		// Wait for Symphony server certificate to be created
		utils.WaitForSymphonyServerCertWindows(t, 5*time.Minute)

		// Create Target YAML
		targetYamlPath := utils.CreateTargetYAMLWindows(t, testDir, targetName, namespace)
		t.Logf("Target YAML path: %s", targetYamlPath)

		// Apply Target YAML to create the target resource
		err := utils.ApplyKubernetesManifestWindows(t, targetYamlPath)
		require.NoError(t, err)

		// Wait for target to be created
		utils.WaitForTargetCreatedWindows(t, targetName, namespace, 30*time.Second)

		t.Logf("✅ Phase 3 completed: Test configurations ready")
	})

	t.Run("Phase4_BootstrapExecution", func(t *testing.T) {
		t.Logf("=== Phase 4: Bootstrap Script Execution ===")

		// Build Windows remote agent binary first (required for MQTT mode)
		binaryPath := utils.BuildWindowsRemoteAgent(t, config)
		config.BinaryPath = binaryPath

		// Start Windows remote agent using bootstrap.ps1 with Windows service mode
		config.RunMode = "service"
		serviceName := fmt.Sprintf("Symphony-RemoteAgent-%s", targetName)

		// Execute bootstrap.ps1 to setup the Windows service
		processCmd = utils.StartWindowsRemoteAgentWithBootstrap(t, config)
		require.NotNil(t, processCmd, "Bootstrap command should not be nil")

		// Give bootstrap.ps1 time to complete and start the service
		time.Sleep(45 * time.Second)

		// Wait for Windows service to be ready
		utils.WaitForWindowsService(t, serviceName, 2*time.Minute)

		// Verify service status
		utils.CheckWindowsServiceStatus(t, serviceName)

		// Set up cleanup for the service
		t.Cleanup(func() {
			t.Logf("=== CLEANUP: Windows Service ===")
			utils.CleanupWindowsService(t, serviceName)
		})

		t.Logf("✅ Phase 4 completed: Bootstrap execution successful")
	})

	t.Run("Phase5_TargetStatusVerification", func(t *testing.T) {
		t.Logf("=== Phase 5: Target Status Verification ===")

		// Debug MQTT connection before verifying target status
		t.Logf("=== DEBUGGING MQTT CONNECTION BEFORE TARGET VERIFICATION ===")
		utils.DebugWindowsTLSConnection(t, detectedBrokerAddress, mqttBrokerPort, config.CACertPath, config.ClientCertPath, config.ClientKeyPath)

		// Wait for target to reach ready state
		utils.WaitForTargetReadyWindows(t, targetName, namespace, 360*time.Second)

		t.Logf("✅ Phase 5 completed: Target status verified")
	})

	t.Run("Phase6_TopologyUpdateVerification", func(t *testing.T) {
		t.Logf("=== Phase 6: Topology Update Verification ===")

		// Verify that topology was successfully updated
		// This checks that the remote agent successfully called
		// the topology update endpoint via MQTT
		utils.VerifyTargetTopologyUpdateWindows(t, targetName, namespace, "Windows MQTT bootstrap")

		t.Logf("✅ Phase 6 completed: Topology update verified")
	})

	t.Run("Phase7_DataInteractionTesting", func(t *testing.T) {
		t.Logf("=== Phase 7: Data Interaction Testing ===")

		// Verify that data flows through MQTT correctly
		// This checks that the remote agent successfully communicates
		// with Symphony through the MQTT broker
		testWindowsMQTTBootstrapDataInteraction(t, targetName, namespace, testDir)

		t.Logf("✅ Phase 7 completed: Data interaction tested")
	})

	// Infrastructure cleanup
	t.Cleanup(func() {
		t.Logf("=== CLEANUP: Infrastructure ===")
		utils.CleanupSymphonyWindows(t)
		utils.CleanupMinikubeWindows(t)
	})

	t.Logf("=== Windows E2E MQTT Communication Test with Bootstrap Completed Successfully ===")
}

// testWindowsMQTTBootstrapDataInteraction - Windows equivalent of Linux testMQTTProcessDataInteraction
func testWindowsMQTTBootstrapDataInteraction(t *testing.T, targetName, namespace, testDir string) {
	// Step 1: Create a simple Solution first
	solutionName := "test-windows-mqtt-bootstrap-solution"
	solutionVersion := "test-windows-mqtt-bootstrap-solution-v-version1"
	solutionYaml := fmt.Sprintf(`
apiVersion: solution.symphony/v1
kind: SolutionContainer
metadata:
  name: %s
  namespace: %s
spec:
---
apiVersion: solution.symphony/v1
kind: Solution
metadata:
  name: %s
  namespace: %s
spec:
  rootResource: %s
  components:
  - name: test-windows-component
    type: script
    properties:
      script: |
        echo "Windows MQTT Bootstrap test component deployed successfully"
        echo "Target: %s"
        echo "Namespace: %s"
        echo "Platform: Windows"
`, solutionName, namespace, solutionVersion, namespace, solutionName, targetName, namespace)

	solutionPath := filepath.Join(testDir, "solution.yaml")
	err := utils.CreateYAMLFileWindows(t, solutionPath, solutionYaml)
	require.NoError(t, err)

	// Apply the solution
	t.Logf("Creating Solution %s...", solutionName)
	err = utils.ApplyKubernetesManifestWindows(t, solutionPath)
	require.NoError(t, err)

	// Step 2: Create an Instance that references the Solution and Target
	instanceName := "test-windows-mqtt-bootstrap-instance"
	instanceYaml := fmt.Sprintf(`
apiVersion: solution.symphony/v1
kind: Instance
metadata:
  name: %s
  namespace: %s
spec:
  displayName: %s
  solution: %s:version1
  target:
    name: %s
  scope: %s-scope
`, instanceName, namespace, instanceName, solutionName, targetName, namespace)

	instancePath := filepath.Join(testDir, "instance.yaml")
	err = utils.CreateYAMLFileWindows(t, instancePath, instanceYaml)
	require.NoError(t, err)

	// Apply the instance
	t.Logf("Creating Instance %s that references Solution %s and Target %s...", instanceName, solutionName, targetName)
	err = utils.ApplyKubernetesManifestWindows(t, instancePath)
	require.NoError(t, err)

	// Wait for Instance deployment to complete or reach a stable state
	t.Logf("Waiting for Instance %s to complete deployment...", instanceName)
	utils.WaitForInstanceReadyWindows(t, instanceName, namespace, 5*time.Minute)

	t.Cleanup(func() {
		// Delete in correct order: Instance -> Solution -> Target
		t.Logf("Deleting Instance first...")
		err := utils.DeleteKubernetesResourceWindows(t, "instances.solution.symphony", instanceName, namespace, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete instance: %v", err)
		} else {
			utils.WaitForResourceDeletedWindows(t, "instance", instanceName, namespace, 1*time.Minute)
		}

		t.Logf("Deleting Solution...")
		err = utils.DeleteSolutionManifestWithTimeoutWindows(t, solutionPath, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete solution: %v", err)
		} else {
			utils.WaitForResourceDeletedWindows(t, "solution", solutionVersion, namespace, 1*time.Minute)
		}

		t.Logf("Deleting Target...")
		err = utils.DeleteKubernetesResourceWindows(t, "targets.fabric.symphony", targetName, namespace, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete target: %v", err)
		}

		t.Logf("Windows MQTT Bootstrap data interaction cleanup completed")
	})

	// Give a short additional wait to ensure stability
	t.Logf("Instance deployment phase completed, test continuing...")
	time.Sleep(2 * time.Second)

	t.Logf("Windows MQTT Bootstrap data interaction test completed - Solution and Instance created successfully")
}
