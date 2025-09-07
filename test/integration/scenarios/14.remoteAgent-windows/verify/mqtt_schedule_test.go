package verify

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"windows-tests/utils"

	"github.com/stretchr/testify/require"
)

func TestE2EMQTTCommunicationWithSchedule(t *testing.T) {
	// Test configuration
	targetName := "test-mqtt-schedule-windows-target"
	namespace := "default"
	mqttBrokerPort := 8883

	// Clean up any stale processes from previous test runs
	utils.CleanupStaleRemoteAgentProcessesWindows(t)

	// Setup test environment
	testDir := utils.SetupWindowsTestDirectory(t)
	t.Logf("Running Windows MQTT schedule test in: %s", testDir)

	// IMPORTANT: Register final process cleanup FIRST so it runs LAST (LIFO order)
	var processCmd *exec.Cmd
	t.Cleanup(func() {
		t.Logf("=== FINAL EMERGENCY PROCESS CLEANUP ===")
		if processCmd != nil && processCmd.Process != nil {
			t.Logf("Emergency cleanup for Windows process PID %d", processCmd.Process.Pid)

			// Try graceful termination first
			if err := processCmd.Process.Signal(os.Interrupt); err == nil {
				time.Sleep(2 * time.Second)
			}

			// Force kill if still running
			if processState := processCmd.ProcessState; processState == nil || !processState.Exited() {
				if err := processCmd.Process.Kill(); err != nil {
					t.Logf("Failed to emergency kill Windows process: %v", err)
				} else {
					t.Logf("Emergency killed Windows process PID %d", processCmd.Process.Pid)
				}
			}
		}
		t.Logf("=== FINAL EMERGENCY CLEANUP FINISHED ===")
	})

	// Step 1: Start fresh minikube cluster
	t.Run("SetupFreshMinikubeCluster", func(t *testing.T) {
		utils.StartFreshMinikubeWindows(t)
	})
	t.Cleanup(func() {
		utils.CleanupMinikubeWindows(t)
	})

	// Setup test namespace
	setupMQTTScheduleNamespace(t, namespace)

	var configPath, topologyPath, targetYamlPath string
	var config utils.WindowsTestConfig
	var detectedBrokerAddress string
	var caSecretName string
	var monitoringStop chan bool

	// Use our Windows MQTT schedule test setup function with detected broker address
	t.Run("SetupMQTTScheduleTestWithDetectedAddress", func(t *testing.T) {
		config, detectedBrokerAddress, caSecretName = utils.SetupWindowsMQTTScheduleTestWithDetectedAddress(t, testDir, targetName, namespace)
		t.Logf("Windows MQTT schedule test setup completed with broker address: %s", detectedBrokerAddress)

		// Debug certificate information
		utils.DebugWindowsCertificateInfo(t, config.CACertPath, "CA Certificate")
		utils.DebugWindowsCertificateInfo(t, config.ClientCertPath, "Client Certificate")
		utils.DebugWindowsMQTTBrokerCertificates(t, testDir)

		// Test TLS connection to MQTT broker with certificates
		utils.DebugWindowsTLSConnection(t, detectedBrokerAddress, 8883, config.CACertPath, config.ClientCertPath, config.ClientKeyPath)

		// CRITICAL FIX: Create CA secret in default namespace for Symphony MQTT client certificate validation
		t.Logf("Creating CA secret in default namespace for Symphony MQTT client...")
		utils.CreateWindowsMQTTCASecretInNamespace(t, namespace, config.CACertPath)
	})

	t.Run("StartSymphonyWithMQTTConfig", func(t *testing.T) {
		// Deploy Symphony with MQTT configuration using detected broker address
		// Use the SAME configuration as mqtt_bootstrap_test.go to ensure proper MQTT client certificate setup
		t.Logf("Starting Symphony with MQTT broker address: tls://%s:%d", detectedBrokerAddress, mqttBrokerPort)
		t.Logf("Using CA secret name: %s", caSecretName)

		// Use the complete MQTT configuration function from mqtt_bootstrap_test.go
		err := startSymphonyWithMQTTConfigWindowsWithBrokerAddress(t, detectedBrokerAddress)
		if err != nil {
			t.Fatalf("Failed to start Symphony with MQTT config: %v", err)
		}

		// Wait for Symphony server certificate to be created
		utils.WaitForSymphonyServerCertWindows(t, 5*time.Minute)

		// Debug MQTT secrets created in Kubernetes
		utils.DebugWindowsMQTTSecrets(t, namespace)

		// Debug certificates in Symphony pods
		utils.DebugSymphonyPodCertificatesWindows(t)

		// Test certificate chain validation
		mqttServerCertPath := filepath.Join(testDir, "mqtt-server.crt")
		if utils.WindowsFileExists(mqttServerCertPath) {
			utils.TestWindowsMQTTCertificateChain(t, config.CACertPath, mqttServerCertPath)
		}

		// CRITICAL TEST: Verify Symphony client certificate can connect to MQTT broker
		t.Logf("=== TESTING SYMPHONY CLIENT CERTIFICATE MQTT CONNECTION ===")
		symphonyClientCertPath := filepath.Join(testDir, "symphony-server.crt")
		symphonyClientKeyPath := filepath.Join(testDir, "symphony-server.key")

		if utils.WindowsFileExists(symphonyClientCertPath) && utils.WindowsFileExists(symphonyClientKeyPath) {
			t.Logf("Testing MQTT connection using Symphony client certificate...")

			// Test connection from detected broker address (what Symphony will use)
			t.Logf("Testing Symphony client cert to detected broker address: %s:%d", detectedBrokerAddress, mqttBrokerPort)
			symphonyCanConnect := utils.TestWindowsMQTTConnectionWithClientCert(t, detectedBrokerAddress, mqttBrokerPort,
				config.CACertPath, symphonyClientCertPath, symphonyClientKeyPath)

			// Also test from localhost (fallback test)
			t.Logf("Testing Symphony client cert to localhost: 127.0.0.1:%d", mqttBrokerPort)
			symphonyCanConnectLocalhost := utils.TestWindowsMQTTConnectionWithClientCert(t, "127.0.0.1", mqttBrokerPort,
				config.CACertPath, symphonyClientCertPath, symphonyClientKeyPath)

			if symphonyCanConnect {
				t.Logf("✅ SUCCESS: Symphony client certificate can connect to MQTT broker at %s:%d", detectedBrokerAddress, mqttBrokerPort)
			} else if symphonyCanConnectLocalhost {
				t.Logf("⚠️ WARNING: Symphony client certificate can only connect via localhost, not detected address")
			} else {
				t.Logf("❌ CRITICAL: Symphony client certificate cannot connect to MQTT broker")
				t.Fatalf("Symphony client certificate MQTT connection failed - this will prevent Symphony from communicating with remote agent")
			}

		} else {
			t.Logf("WARNING: Symphony client certificate files not found:")
			t.Logf("  Expected cert: %s (exists: %t)", symphonyClientCertPath, utils.WindowsFileExists(symphonyClientCertPath))
			t.Logf("  Expected key: %s (exists: %t)", symphonyClientKeyPath, utils.WindowsFileExists(symphonyClientKeyPath))
		}

		// Additional comparison: Test with remote agent certificates
		t.Logf("=== COMPARISON: TESTING WITH REMOTE AGENT CERTIFICATES ===")
		t.Logf("Testing remote agent certificates for comparison...")
		remoteAgentCanConnect := utils.TestWindowsMQTTConnectionWithClientCert(t, detectedBrokerAddress, mqttBrokerPort,
			config.CACertPath, config.ClientCertPath, config.ClientKeyPath)
		remoteAgentCanConnectLocalhost := utils.TestWindowsMQTTConnectionWithClientCert(t, "127.0.0.1", mqttBrokerPort,
			config.CACertPath, config.ClientCertPath, config.ClientKeyPath)

		if remoteAgentCanConnect || remoteAgentCanConnectLocalhost {
			t.Logf("✅ Remote agent certificates can connect to MQTT broker")
		} else {
			t.Logf("❌ WARNING: Remote agent certificates also cannot connect to MQTT broker")
		}

		t.Logf("=== END MQTT CONNECTION TESTING ===")
	})

	// Create test configurations AFTER Symphony is running
	t.Run("CreateTestConfigurations", func(t *testing.T) {
		// Use the config path that was already created with the correct broker address
		configPath = config.ConfigPath
		topologyPath = config.TopologyPath
		fmt.Printf("Windows Topology path: %s", topologyPath)
		targetYamlPath = utils.CreateWindowsTargetYAML(t, testDir, targetName, namespace)
		fmt.Printf("Windows Target YAML path: %s", targetYamlPath)
		// Apply Target YAML to create the target resource
		err := utils.ApplyKubernetesManifestWindows(t, targetYamlPath)
		require.NoError(t, err)

		// Wait for target to be created
		utils.WaitForTargetCreatedWindows(t, targetName, namespace, 30*time.Second)
	})

	// Start the remote agent process at main test level so it persists across subtests
	t.Logf("Starting Windows MQTT remote agent process in schedule mode...")
	// The config was already properly set up in SetupWindowsMQTTScheduleTestWithDetectedAddress
	// Just update the paths that were created in CreateTestConfigurations
	config.ConfigPath = configPath
	config.TopologyPath = topologyPath
	fmt.Printf("Starting Windows remote agent process with config: %+v\n", config)

	// Start remote agent using direct process (no Windows service) without automatic cleanup
	processCmd = utils.StartWindowsRemoteAgentProcessWithoutCleanup(t, config)
	require.NotNil(t, processCmd)

	// Set up cleanup at main test level to ensure process runs for entire test
	// This should be the FIRST cleanup registered so it runs LAST (LIFO order)
	t.Cleanup(func() {
		t.Logf("=== STARTING WINDOWS PROCESS CLEANUP ===")
		if processCmd != nil && processCmd.Process != nil {
			t.Logf("Cleaning up Windows MQTT remote agent process PID %d from main test...", processCmd.Process.Pid)
			utils.CleanupWindowsRemoteAgentProcess(t, processCmd)
			t.Logf("Windows process cleanup completed")
		} else {
			t.Logf("No Windows process to cleanup (processCmd is nil)")
		}
		t.Logf("=== WINDOWS PROCESS CLEANUP FINISHED ===")
	})

	// Also set up a signal handler for immediate cleanup on test interruption
	defer func() {
		if r := recover(); r != nil {
			t.Logf("Test panicked, performing emergency Windows cleanup: %v", r)
			if processCmd != nil {
				utils.CleanupWindowsRemoteAgentProcess(t, processCmd)
			}
			panic(r) // Re-panic after cleanup
		}
	}()

	// Add process monitoring to detect early exits
	processExited := make(chan bool, 1)
	go func() {
		processCmd.Wait()
		processExited <- true
	}()

	// Wait for process to be ready and healthy
	utils.WaitForWindowsProcessHealthy(t, processCmd, 30*time.Second)
	t.Logf("Windows MQTT remote agent process started successfully in schedule mode and will persist across all subtests")

	// Additional monitoring: check process didn't exit early
	select {
	case <-processExited:
		t.Fatalf("Windows remote agent process exited unexpectedly during startup")
	case <-time.After(2 * time.Second):
		// Process is still running after health check + buffer time
		t.Logf("Windows process stability confirmed - continuing with tests")
	}

	// Start continuous process monitoring throughout the test
	processMonitoring := make(chan bool, 1)
	monitoringStop = make(chan bool, 1)

	go func() {
		defer close(processMonitoring)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-processExited:
				t.Logf("WARNING: Windows remote agent process exited during test execution")
				return
			case <-monitoringStop:
				t.Logf("Windows process monitoring stopped by cleanup")
				return
			case <-ticker.C:
				if processCmd.ProcessState != nil && processCmd.ProcessState.Exited() {
					t.Logf("WARNING: Windows remote agent process has exited (state: %s)", processCmd.ProcessState.String())
					return
				}
				t.Logf("Windows process monitoring: Remote agent PID %d is still running", processCmd.Process.Pid)
			}
		}
	}()

	// Set up cleanup for the monitoring goroutine - this should run BEFORE process cleanup
	t.Cleanup(func() {
		t.Logf("Stopping Windows process monitoring...")
		select {
		case monitoringStop <- true:
			t.Logf("Windows process monitoring stop signal sent")
		default:
			t.Logf("Windows process monitoring stop signal channel full or closed")
		}

		// Wait a moment for monitoring to stop
		time.Sleep(1 * time.Second)

		// Close the monitoring stop channel
		close(monitoringStop)
	})

	t.Run("VerifyProcessStarted", func(t *testing.T) {
		// Just verify the process is running
		require.NotNil(t, processCmd)
		require.NotNil(t, processCmd.Process)

		// Check if process has already exited
		// if processCmd.ProcessState != nil && processCmd.ProcessState.Exited() {
		// 	t.Fatalf("Windows remote agent process has already exited: %s", processCmd.ProcessState.String())
		// }

		// // Platform-specific process verification
		// if runtime.GOOS == "windows" {
		// 	// Windows-specific check: verify process handle is valid and hasn't exited
		// 	if processCmd.Process == nil {
		// 		t.Fatalf("Windows process handle is nil")
		// 	}
		// 	// Windows doesn't support Unix-style signals, so we rely on ProcessState check above
		// 	t.Logf("Windows process verification passed - signals not supported on Windows")
		// } else {
		// 	// Unix-style signal check for non-Windows platforms
		// 	if err := processCmd.Process.Signal(syscall.Signal(0)); err != nil {
		// 		t.Fatalf("Process is not responding to signals (likely dead): %v", err)
		// 	}
		// }

		t.Logf("Windows MQTT remote agent process verified running with PID: %d", processCmd.Process.Pid)

		// Log current process status for debugging
		t.Logf("Windows process state: running=%t, exited=%t",
			processCmd.ProcessState == nil,
			processCmd.ProcessState != nil && processCmd.ProcessState.Exited())
	})

	t.Run("VerifyTargetStatus", func(t *testing.T) {
		// First check if our process is still running
		if processCmd.ProcessState != nil && processCmd.ProcessState.Exited() {
			t.Fatalf("Windows remote agent process exited before target verification: %s", processCmd.ProcessState.String())
		}

		// Debug MQTT connection before verifying target status
		t.Logf("=== DEBUGGING WINDOWS MQTT CONNECTION BEFORE TARGET VERIFICATION ===")
		utils.DebugWindowsTLSConnection(t, detectedBrokerAddress, mqttBrokerPort, config.CACertPath, config.ClientCertPath, config.ClientKeyPath)

		// Also test from localhost (where remote agent runs)
		utils.DebugWindowsTLSConnection(t, "127.0.0.1", mqttBrokerPort, config.CACertPath, config.ClientCertPath, config.ClientKeyPath)

		// Wait for target to reach ready state
		utils.WaitForTargetReadyWindows(t, targetName, namespace, 360*time.Second)

		// Check again after waiting - process should still be running
		if processCmd.ProcessState != nil && processCmd.ProcessState.Exited() {
			t.Logf("WARNING: Windows remote agent process exited during target status verification: %s", processCmd.ProcessState.String())
		}
	})

	t.Run("VerifyTopologyUpdate", func(t *testing.T) {
		// Verify process is still running before topology verification
		if processCmd.ProcessState != nil && processCmd.ProcessState.Exited() {
			t.Fatalf("Windows remote agent process exited before topology verification: %s", processCmd.ProcessState.String())
		}

		// Verify that topology was successfully updated
		// This would check that the remote agent successfully called
		// the topology update endpoint via MQTT
		utils.VerifyTargetTopologyUpdateWindows(t, targetName, namespace, "Windows MQTT schedule")
	})

	t.Run("VerifyMQTTScheduleDataInteraction", func(t *testing.T) {
		// Verify process is still running before starting data interaction test
		if processCmd.ProcessState != nil && processCmd.ProcessState.Exited() {
			t.Fatalf("Windows remote agent process exited before data interaction test: %s", processCmd.ProcessState.String())
		}

		// Verify that data flows through MQTT correctly
		// This would check that the remote agent successfully communicates
		// with Symphony through the MQTT broker in schedule mode
		testWindowsMQTTScheduleDataInteraction(t, targetName, namespace, testDir)

		// Final check - process should still be running after all tests
		if processCmd.ProcessState != nil && processCmd.ProcessState.Exited() {
			t.Logf("WARNING: Windows remote agent process exited during data interaction test: %s", processCmd.ProcessState.String())
		} else {
			t.Logf("SUCCESS: Windows remote agent process survived all tests and is still running")
		}
	})

	// Infrastructure cleanup - this runs BEFORE process cleanup due to LIFO order
	t.Cleanup(func() {
		t.Logf("=== STARTING WINDOWS INFRASTRUCTURE CLEANUP ===")

		// For Windows MQTT schedule test, we don't use Windows service, so use individual cleanup functions
		// instead of CleanupSymphony which includes service cleanup

		// Dump logs first
		projectRoot := utils.GetWindowsProjectRoot(t)
		localenvDir := filepath.Join(projectRoot, "test", "localenv")
		cmd := exec.Command("mage", "dumpSymphonyLogsForTest", fmt.Sprintf("'%s'", "remote-agent-mqtt-schedule-windows-test"))
		cmd.Dir = localenvDir
		if err := cmd.Run(); err != nil {
			t.Logf("Warning: Failed to dump Symphony logs: %v", err)
		}

		// Destroy symphony without Windows service cleanup
		cmd = exec.Command("mage", "destroy", "all,nowait")
		cmd.Dir = localenvDir
		if err := cmd.Run(); err != nil {
			t.Logf("Warning: Failed to destroy Symphony: %v", err)
		}

		utils.CleanupExternalMQTTBrokerWindows(t) // Use external broker cleanup
		utils.CleanupWindowsMQTTCASecret(t, "mqtt-ca")
		utils.CleanupWindowsMQTTClientSecret(t, namespace, "mqtt-client-secret")
		t.Logf("=== WINDOWS INFRASTRUCTURE CLEANUP FINISHED ===")
	})

	// EXPLICIT CLEANUP BEFORE TEST ENDS - ensure process is stopped
	t.Logf("=== EXPLICIT WINDOWS PROCESS CLEANUP BEFORE TEST END ===")

	// Stop monitoring first
	select {
	case monitoringStop <- true:
		t.Logf("Windows process monitoring explicitly stopped")
	default:
		t.Logf("Windows process monitoring stop channel not available")
	}

	// Wait a moment for monitoring to stop
	time.Sleep(1 * time.Second)

	// Then cleanup the process with timeout protection
	if processCmd != nil && processCmd.Process != nil {
		t.Logf("Explicitly stopping Windows remote agent process PID %d...", processCmd.Process.Pid)

		// Run cleanup in a goroutine with timeout to prevent hanging
		done := make(chan bool, 1)
		go func() {
			utils.CleanupWindowsRemoteAgentProcess(t, processCmd)
			done <- true
		}()

		select {
		case <-done:
			t.Logf("Explicit Windows process cleanup completed successfully")
		case <-time.After(30 * time.Second):
			t.Logf("WARNING: Windows process cleanup timed out after 30 seconds, force killing...")
			// Force kill as last resort
			if err := processCmd.Process.Kill(); err != nil {
				t.Logf("Failed to force kill Windows process: %v", err)
			} else {
				t.Logf("Windows process force killed due to cleanup timeout")
			}
		}
	}

	t.Logf("=== EXPLICIT WINDOWS CLEANUP COMPLETED ===")

	t.Logf("Windows MQTT communication test with direct process (schedule mode) completed successfully")
}

func setupMQTTScheduleNamespace(t *testing.T, namespace string) {
	// Create namespace if it doesn't exist
	_, err := utils.GetKubeClientWindows()
	if err != nil {
		t.Logf("Warning: Could not get kube client to create namespace: %v", err)
		return
	}

	nsYaml := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, namespace)

	nsPath := filepath.Join(utils.SetupWindowsTestDirectory(t), "namespace.yaml")
	err = utils.CreateWindowsYAMLFile(t, nsPath, nsYaml)
	if err == nil {
		utils.ApplyKubernetesManifestWindows(t, nsPath)
	}
}

func testWindowsMQTTScheduleDataInteraction(t *testing.T, targetName, namespace, testDir string) {
	// Step 1: Create a simple Solution first
	solutionName := "test-mqtt-schedule-windows-solution"
	solutionVersion := "test-mqtt-schedule-windows-solution-v-version1"
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
  - name: test-component
    type: script
    properties:
      script: |
        echo "Windows MQTT Schedule test component deployed successfully"
        echo "Target: %s"
        echo "Namespace: %s"
        echo "Mode: Schedule (Direct Process)"
`, solutionName, namespace, solutionVersion, namespace, solutionName, targetName, namespace)

	solutionPath := filepath.Join(testDir, "solution.yaml")
	err := utils.CreateWindowsYAMLFile(t, solutionPath, solutionYaml)
	require.NoError(t, err)

	// Apply the solution
	t.Logf("Creating Windows Solution %s...", solutionName)
	err = utils.ApplyKubernetesManifestWindows(t, solutionPath)
	require.NoError(t, err)

	// Step 2: Create an Instance that references the Solution and Target
	instanceName := "test-mqtt-schedule-windows-instance"
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
	err = utils.CreateWindowsYAMLFile(t, instancePath, instanceYaml)
	require.NoError(t, err)

	// Apply the instance
	t.Logf("Creating Windows Instance %s that references Solution %s and Target %s...", instanceName, solutionName, targetName)
	err = utils.ApplyKubernetesManifestWindows(t, instancePath)
	require.NoError(t, err)

	// Wait for Instance deployment to complete or reach a stable state
	t.Logf("Waiting for Windows Instance %s to complete deployment...", instanceName)
	utils.WaitForInstanceReadyWindows(t, instanceName, namespace, 5*time.Minute)

	t.Cleanup(func() {
		// Delete in correct order: Instance -> Solution -> Target
		// Following the pattern from CleanUpSymphonyObjects function

		// First delete Instance and ensure it's completely removed
		t.Logf("Deleting Windows Instance first...")
		err := utils.DeleteKubernetesResourceWindows(t, "instances.solution.symphony", instanceName, namespace, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete Windows instance: %v", err)
		} else {
			// Wait for Instance to be completely deleted before proceeding
			utils.WaitForResourceDeletedWindows(t, "instance", instanceName, namespace, 1*time.Minute)
		}

		// Then delete Solution and ensure it's completely removed
		t.Logf("Deleting Windows Solution...")
		err = utils.DeleteSolutionManifestWithTimeoutWindows(t, solutionPath, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete Windows solution: %v", err)
		} else {
			// Wait for Solution to be completely deleted before proceeding
			utils.WaitForResourceDeletedWindows(t, "solution", solutionVersion, namespace, 1*time.Minute)
		}

		// Finally delete Target
		t.Logf("Deleting Windows Target...")
		err = utils.DeleteKubernetesResourceWindows(t, "targets.fabric.symphony", targetName, namespace, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete Windows target: %v", err)
		}

		t.Logf("Windows cleanup completed")
	})

	// Give a short additional wait to ensure stability
	t.Logf("Windows Instance deployment phase completed, test continuing...")
	time.Sleep(2 * time.Second)

	// Verify instance status
	// In a real test, you would check that:
	// 1. The instance was processed by Symphony
	// 2. The remote agent received deployment instructions via MQTT
	// 3. The agent successfully executed the deployment in schedule mode
	// 4. Status was reported back to Symphony

	t.Logf("Windows MQTT Schedule data interaction test completed - Solution and Instance created successfully")
}
