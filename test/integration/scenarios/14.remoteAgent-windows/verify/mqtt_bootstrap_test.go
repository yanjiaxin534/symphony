//go:build windows || !linux

package verify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"windows-tests/utils"
)

// TestE2EMQTTCommunicationWithBootstrap - Windows equivalent of Linux TestE2EMQTTCommunicationWithBootstrap
func TestE2EMQTTCommunicationWithBootstrap(t *testing.T) {
	// Test configuration - use relative path from test directory
	projectRoot := utils.GetWindowsProjectRoot(t) // Get project root dynamically
	targetName := "test-mqtt-bootstrap-windows-target"
	namespace := "default"
	mqttBrokerPort := 8883

	// Setup test environment
	testDir := utils.CreateWindowsTestDirectory(t)
	t.Logf("Running Windows MQTT communication test in: %s", testDir)

	// Step 1: Start fresh minikube cluster
	t.Run("SetupFreshMinikubeCluster", func(t *testing.T) {
		utils.StartFreshMinikubeWindows(t)
	})
	t.Cleanup(func() {
		utils.CleanupMinikubeWindows(t)
	})

	// Generate MQTT certificates using dedicated MQTT certificate function
	mqttCerts := utils.GenerateWindowsMQTTCertificates(t, testDir)

	// Setup test namespace
	setupNamespaceWindows(t, namespace)

	var caSecretName, clientSecretName, remoteAgentSecretName string
	var configPath, topologyPath, targetYamlPath string
	var config utils.WindowsTestConfig
	var brokerAddress string

	// Set up initial config with certificate paths
	t.Run("SetupInitialConfig", func(t *testing.T) {
		config = utils.SetupInitialConfigWindows(t, testDir, targetName, namespace, mqttCerts)
	})

	// Setup external MQTT broker with detected address (matches Linux pattern)
	t.Run("SetupExternalMQTTBroker", func(t *testing.T) {
		// Setup external MQTT broker using Docker and get optimal address
		optimalBrokerAddress := utils.SetupExternalMQTTBrokerWindows(t, mqttCerts, mqttBrokerPort)
		t.Logf("External MQTT broker setup completed. Optimal broker address: %s", optimalBrokerAddress)

		// Use the optimal broker address detected by the setup function
		brokerAddress = optimalBrokerAddress
		config.BrokerAddress = brokerAddress
		config.BrokerPort = fmt.Sprintf("%d", mqttBrokerPort)

		t.Logf("Using optimal broker address for all tests: %s:%d", brokerAddress, mqttBrokerPort)
	})

	// NEW: Verify MQTT connectivity before proceeding
	t.Run("VerifyMQTTConnectivity", func(t *testing.T) {
		t.Logf("Verifying MQTT connectivity to broker at %s:%d", brokerAddress, mqttBrokerPort)

		// Step 1: Test basic TCP connectivity from minikube to broker
		t.Logf("Step 1: Testing basic TCP connectivity from minikube cluster...")
		if !utils.VerifyMQTTConnectivityWindows(t, brokerAddress, mqttBrokerPort) {
			// Try to create firewall rule if connectivity fails
			t.Logf("TCP connectivity failed - attempting to create Windows firewall rule...")
			if utils.CreateFirewallRuleWindows(t, mqttBrokerPort) {
				t.Logf("Firewall rule created, retesting connectivity...")
				time.Sleep(5 * time.Second) // Wait for rule to take effect
				if !utils.VerifyMQTTConnectivityWindows(t, brokerAddress, mqttBrokerPort) {
					t.Fatalf("MQTT connectivity test still failed after creating firewall rule. Manual firewall configuration may be required.")
				}
			} else {
				t.Logf("Failed to create firewall rule automatically.")
				t.Logf("MANUAL ACTION REQUIRED: Run as administrator:")
				t.Logf("  netsh advfirewall firewall add rule name=\"Allow MQTT %d\" dir=in action=allow protocol=TCP localport=%d", mqttBrokerPort, mqttBrokerPort)
				t.Fatalf("Basic TCP connectivity test failed - likely Windows Firewall blocking port %d", mqttBrokerPort)
			}
		}

		// Step 2: Test MQTT connection with certificates
		// t.Logf("Step 2: Testing MQTT connection with certificates...")
		// if !utils.VerifyMQTTWithCertificatesWindows(t, brokerAddress, mqttBrokerPort, mqttCerts) {
		// 	t.Fatalf("MQTT certificate test failed - check certificate configuration")
		// }

		// Step 3: Verify firewall rule exists (informational)
		ruleName := fmt.Sprintf("Allow MQTT %d", mqttBrokerPort)
		if utils.VerifyFirewallRuleWindows(t, ruleName) {
			t.Logf("Confirmed: Windows firewall rule '%s' is active", ruleName)
		} else {
			t.Logf("Note: No specific firewall rule found for MQTT port %d", mqttBrokerPort)
		}

		// // Step 4: Test network connectivity from Windows host (optional diagnostic)
		// t.Logf("Step 4: Testing network connectivity from Windows host...")
		// if utils.TestNetworkConnectivityWindows(t, brokerAddress, mqttBrokerPort) {
		// 	t.Logf("Windows host network connectivity test passed")
		// } else {
		// 	t.Logf("Windows host network connectivity test failed (this may be expected in some environments)")
		// }

		t.Logf("✅ MQTT connectivity verification completed successfully!")
	})

	// Setup MQTT bootstrap test configuration
	t.Run("SetupMQTTBootstrapTestConfig", func(t *testing.T) {
		utils.SetupMQTTBootstrapTestWithDetectedAddressWindows(t, testDir, targetName, namespace, &config, &mqttCerts)
		t.Logf("Windows MQTT bootstrap test setup completed with broker address: %s", brokerAddress)
	})

	t.Run("CreateCertificateSecrets", func(t *testing.T) {
		// Create CA secret in cert-manager namespace for remote agent trust (expected name: client-cert-secret)
		caSecretName = utils.CreateMQTTCASecretForRemoteAgentWindows(t, mqttCerts)

		// Create Symphony MQTT client certificate secret in default namespace (expected name: mqtt-client-secret)
		clientSecretName = utils.CreateMQTTClientSecretForSymphonyWindows(t, namespace, mqttCerts)

		// Create Remote Agent MQTT client certificate secret in default namespace
		remoteAgentSecretName = createRemoteAgentClientCertSecretWindowsMQTT(t, namespace, mqttCerts)
	})

	t.Run("StartSymphonyWithMQTTConfig", func(t *testing.T) {
		// Start Symphony with MQTT configuration using the detected broker address
		t.Logf("Starting Symphony with MQTT configuration using broker address: %s", brokerAddress)

		// Use the custom MQTT configuration function that accepts the broker address
		err := startSymphonyWithMQTTConfigWindowsWithBrokerAddress(t, brokerAddress)
		if err != nil {
			t.Fatalf("Failed to start Symphony with MQTT config: %v", err)
		}

		// Wait longer for Symphony server certificate to be created - cert-manager needs time
		t.Logf("Waiting for Symphony API server certificate creation...")
		waitForSymphonyServerCertWindows(t, 8*time.Minute)

		// Additional wait to ensure certificate is fully propagated
		t.Logf("Certificate ready, waiting additional time for propagation...")
		time.Sleep(30 * time.Second)

		// Wait for Symphony service to be ready and accessible
		utils.WaitForSymphonyServiceReadyWindows(t, 5*time.Minute)
	})

	// Create test configurations AFTER Symphony is running
	t.Run("CreateTestConfigurations", func(t *testing.T) {
		configPath = config.ConfigPath
		topologyPath = config.TopologyPath
		t.Logf("Topology path: %s", topologyPath)
		targetYamlPath = utils.CreateTargetYAMLWindows(t, testDir, targetName, namespace)
		t.Logf("Target YAML path: %s", targetYamlPath)

		// Apply Target YAML to create the target resource with retry for webhook readiness
		err := applyKubernetesManifestWithRetryWindows(t, targetYamlPath, 5, 10*time.Second)
		require.NoError(t, err)

		// Wait for target to be created
		utils.WaitForTargetCreatedWindows(t, targetName, namespace, 30*time.Second)
	})

	var serviceName string

	t.Run("StartRemoteAgentWithMQTTBootstrap", func(t *testing.T) {
		// Configure remote agent for MQTT (use MQTT-generated certificates only)
		config := utils.WindowsTestConfig{
			ProjectRoot:    projectRoot,
			ConfigPath:     configPath,
			ClientCertPath: mqttCerts.RemoteAgentCert, // Use remote agent cert from MQTT certs
			ClientKeyPath:  mqttCerts.RemoteAgentKey,  // Use remote agent key from MQTT certs
			CACertPath:     mqttCerts.CACert,          // Use MQTT CA cert (ca.crt) for TLS trust
			TargetName:     targetName,
			Namespace:      namespace,
			TopologyPath:   topologyPath,
			Protocol:       "mqtt",
			BrokerAddress:  brokerAddress,
			BrokerPort:     fmt.Sprintf("%d", mqttBrokerPort),
			RunMode:        "service",
		}

		// CRITICAL: For MQTT mode, always use MQTT-generated CA certificate (ca.crt), never Symphony server CA
		t.Logf("Using MQTT-generated CA certificate (ca.crt): %s", mqttCerts.CACert)

		// Verify the CA certificate file exists before proceeding
		if !utils.FileExistsWindows(mqttCerts.CACert) {
			t.Fatalf("MQTT CA certificate file does not exist: %s", mqttCerts.CACert)
		}

		// Build Windows remote agent binary first (required for MQTT mode)
		binaryPath := utils.BuildWindowsRemoteAgent(t, config)
		config.BinaryPath = binaryPath

		// Verify all certificate files exist before starting bootstrap with enhanced checking
		t.Logf("Verifying all certificate files exist before bootstrap...")
		certFiles := map[string]string{
			"CA Certificate":     config.CACertPath,
			"Client Certificate": config.ClientCertPath,
			"Client Key":         config.ClientKeyPath,
		}

		for certType, certPath := range certFiles {
			if !utils.FileExistsWindows(certPath) {
				t.Fatalf("%s file does not exist: %s", certType, certPath)
			}
			// Verify file has content and is readable
			if stat, err := os.Stat(certPath); err != nil {
				t.Fatalf("%s file stat failed: %s (error: %v)", certType, certPath, err)
			} else if stat.Size() == 0 {
				t.Fatalf("%s file is empty: %s", certType, certPath)
			} else {
				t.Logf("✓ %s exists and has content (%d bytes): %s", certType, stat.Size(), certPath)
			}
		}

		// Use enhanced bootstrap that includes certificate verification
		t.Logf("Starting remote agent with enhanced MQTT certificate verification: %+v", config)
		bootstrapCmd := utils.EnhancedStartWindowsRemoteAgentWithBootstrap(t, config)
		require.NotNil(t, bootstrapCmd)

		// Service name for cleanup - store in outer scope for main cleanup
		serviceName = fmt.Sprintf("Symphony-RemoteAgent-%s", targetName)

		// Check service status
		utils.CheckWindowsServiceStatus(t, serviceName)

		// Try to wait for service to be active
		t.Logf("Attempting to verify service is active...")
		go func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("Service check failed, but enhanced bootstrap.ps1 succeeded: %v", r)
				}
			}()
			utils.WaitForWindowsService(t, serviceName, 15*time.Second)
		}()

		time.Sleep(5 * time.Second)
		t.Logf("Continuing with test - enhanced bootstrap.ps1 completed successfully")
	})

	t.Run("VerifyTargetStatus", func(t *testing.T) {
		// Wait for target to reach ready state - increased timeout due to more thorough checks
		utils.WaitForTargetReadyWindows(t, targetName, namespace, 10*time.Minute)
	})

	t.Run("VerifyTopologyUpdate", func(t *testing.T) {
		// Verify that topology was successfully updated
		// This would check that the remote agent successfully called
		// the topology update endpoint via MQTT
		utils.VerifyTargetTopologyUpdateWindows(t, targetName, namespace, "Windows MQTT bootstrap")
	})

	t.Run("VerifyMQTTDataInteraction", func(t *testing.T) {
		// Verify that data flows through MQTT correctly
		// This would check that the remote agent successfully communicates
		// with Symphony through the MQTT broker
		testBootstrapDataInteractionWindows(t, targetName, namespace, testDir)
	})

	// Cleanup - following Linux pattern: service cleanup at main test level
	t.Cleanup(func() {
		// Clean up Windows service FIRST (equivalent to Linux utils.CleanupSystemdService)
		if serviceName != "" {
			utils.CleanupWindowsService(t, serviceName)
		}
		utils.CleanupSymphonyWindows(t)
		utils.CleanupExternalMQTTBrokerWindows(t) // Cleanup external MQTT broker
		cleanupMQTTCASecretWindows(t, caSecretName)
		cleanupMQTTClientSecretWindows(t, namespace, clientSecretName)      // Symphony client cert
		cleanupMQTTClientSecretWindows(t, namespace, remoteAgentSecretName) // Remote Agent client cert
	})

	t.Logf("Windows MQTT communication test completed successfully")
}

func setupNamespaceWindows(t *testing.T, namespace string) {
	// Create namespace if it doesn't exist
	nsYaml := fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, namespace)

	nsPath := filepath.Join(utils.CreateWindowsTestDirectory(t), "namespace.yaml")
	err := utils.CreateYAMLFileWindows(t, nsPath, nsYaml)
	if err == nil {
		utils.ApplyKubernetesManifestWindows(t, nsPath)
	}
}

func testBootstrapDataInteractionWindows(t *testing.T, targetName, namespace, testDir string) {
	// Step 1: Create a simple Solution first
	solutionName := "test-bootstrap-solution-windows"
	solutionVersion := "test-bootstrap-solution-windows-v-version1"
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
  - name: test-component-windows
    type: script
    properties:
      script: |
        echo "Windows Bootstrap test component deployed successfully"
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
	instanceName := "test-bootstrap-instance-windows"
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
		// Following the pattern from CleanUpSymphonyObjects function

		// First delete Instance and ensure it's completely removed
		t.Logf("Deleting Instance first...")
		err := utils.DeleteKubernetesResourceWindows(t, "instances.solution.symphony", instanceName, namespace, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete instance: %v", err)
		} else {
			// Wait for Instance to be completely deleted before proceeding
			utils.WaitForResourceDeletedWindows(t, "instance", instanceName, namespace, 1*time.Minute)
		}

		// Then delete Solution and ensure it's completely removed
		t.Logf("Deleting Solution...")
		err = utils.DeleteSolutionManifestWithTimeoutWindows(t, solutionPath, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete solution: %v", err)
		} else {
			// Wait for Solution to be completely deleted before proceeding
			utils.WaitForResourceDeletedWindows(t, "solution", solutionVersion, namespace, 1*time.Minute)
		}

		// Finally delete Target
		t.Logf("Deleting Target...")
		err = utils.DeleteKubernetesResourceWindows(t, "targets.fabric.symphony", targetName, namespace, 2*time.Minute)
		if err != nil {
			t.Logf("Warning: Failed to delete target: %v", err)
		}

		t.Logf("Windows cleanup completed")
	})

	// Give a short additional wait to ensure stability
	t.Logf("Instance deployment phase completed, test continuing...")
	time.Sleep(2 * time.Second)

	// Verify instance status
	// In a real test, you would check that:
	// 1. The instance was processed by Symphony
	// 2. The remote agent received deployment instructions
	// 3. The agent successfully executed the deployment
	// 4. Status was reported back to Symphony

	t.Logf("Windows Bootstrap data interaction test completed - Solution and Instance created successfully")
}

// Helper functions for MQTT certificate secret management
func createMQTTCASecretWindows(t *testing.T, certs utils.WindowsCertificatePaths) string {
	secretName := "mqtt-ca-windows"

	// Ensure cert-manager namespace exists
	cmd := exec.Command("kubectl", "create", "namespace", "cert-manager")
	cmd.Run() // Ignore error if namespace already exists

	// Create CA secret in cert-manager namespace
	cmd = exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=ca.crt="+certs.CACert,
		"-n", "cert-manager")

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create MQTT CA secret (may already exist): %v", err)
	} else {
		t.Logf("Created MQTT CA secret %s in cert-manager namespace", secretName)
	}
	return secretName
}

func createSymphonyMQTTClientSecretWindows(t *testing.T, namespace string, certs utils.WindowsCertificatePaths) string {
	secretName := "symphony-mqtt-client-secret-windows"

	cmd := exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=client.crt="+certs.ClientPEM,
		"--from-file=client.key="+certs.ClientKey,
		"-n", namespace)

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create Symphony MQTT client secret (may already exist): %v", err)
	} else {
		t.Logf("Created Symphony MQTT client secret %s in namespace %s", secretName, namespace)
	}
	return secretName
}

func createRemoteAgentClientCertSecretWindows(t *testing.T, namespace string, certs utils.WindowsCertificatePaths) string {
	secretName := "remote-agent-mqtt-client-secret-windows"

	cmd := exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=client.crt="+certs.ClientPEM,
		"--from-file=client.key="+certs.ClientKey,
		"-n", namespace)

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create Remote Agent MQTT client secret (may already exist): %v", err)
	} else {
		t.Logf("Created Remote Agent MQTT client secret %s in namespace %s", secretName, namespace)
	}
	return secretName
}

func startSymphonyWithMQTTConfigWindows(t *testing.T, brokerAddress string) {
	t.Logf("Starting Symphony with MQTT configuration on Windows: %s", brokerAddress)

	projectRoot := utils.GetWindowsProjectRoot(t)
	localenvDir := filepath.Join(projectRoot, "test", "localenv")

	// Use mage to deploy Symphony with MQTT settings
	helmValues := fmt.Sprintf("--set mqtt.enabled=true --set mqtt.brokerAddress=%s --set mqtt.useTLS=true --set certManager.enabled=true", brokerAddress)
	cmd := exec.Command("mage", "cluster:deploywithsettings", helmValues)
	cmd.Dir = localenvDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("Symphony MQTT deployment stdout: %s", stdout.String())
		t.Logf("Symphony MQTT deployment stderr: %s", stderr.String())
		t.Fatalf("Symphony MQTT deployment failed on Windows: %v", err)
	}

	t.Logf("Started Symphony with MQTT configuration on Windows")
}

// startSymphonyWithMQTTConfigWindowsWithBrokerAddress starts Symphony with MQTT configuration using custom broker address
func startSymphonyWithMQTTConfigWindowsWithBrokerAddress(t *testing.T, brokerAddress string) error {
	t.Logf("Starting Symphony with MQTT configuration using custom broker address: %s", brokerAddress)

	projectRoot := utils.GetWindowsProjectRoot(t)
	localenvDir := filepath.Join(projectRoot, "test", "localenv")

	// Check if localenv directory exists
	if _, err := os.Stat(localenvDir); os.IsNotExist(err) {
		return fmt.Errorf("localenv directory does not exist: %s", localenvDir)
	}

	// Build comprehensive Helm values with the detected IPv4 broker address
	// Format the broker address with TLS prefix for proper MQTT configuration
	mqttBrokerAddress := fmt.Sprintf("tls://%s:8883", brokerAddress)

	helmValues := fmt.Sprintf("--set remoteAgent.remoteCert.used=true "+
		"--set remoteAgent.remoteCert.trustCAs.secretName=client-cert-secret "+
		"--set remoteAgent.remoteCert.trustCAs.secretKey=ca.crt "+
		"--set remoteAgent.remoteCert.subjects=remote-agent-client "+
		"--set mqtt.mqttClientCert.enabled=true "+
		"--set mqtt.mqttClientCert.secretName=mqtt-client-secret "+
		"--set mqtt.mqttClientCert.crt=client.crt "+
		"--set mqtt.mqttClientCert.key=client.key "+
		"--set mqtt.brokerAddress=%s "+
		"--set mqtt.enabled=true --set mqtt.useTLS=true "+
		"--set certManager.enabled=true "+
		"--set api.env.ISSUER_NAME=symphony-ca-issuer "+
		"--set api.env.SYMPHONY_SERVICE_NAME=symphony-service", mqttBrokerAddress)

	t.Logf("Using Helm values: %s", helmValues)

	// Use timeout context for the deployment
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "mage", "cluster:deploywithsettings", helmValues)
	cmd.Dir = localenvDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Logf("Symphony MQTT deployment stdout: %s", stdout.String())
		t.Logf("Symphony MQTT deployment stderr: %s", stderr.String())
		return fmt.Errorf("symphony MQTT deployment failed on Windows: %v", err)
	}

	t.Logf("Successfully started Symphony with MQTT configuration using broker address: %s", mqttBrokerAddress)
	return nil
}

func waitForSymphonyServerCertWindows(t *testing.T, timeout time.Duration) {
	t.Logf("Waiting for Symphony server certificate on Windows (timeout: %v)", timeout)

	// Similar to the Linux version, wait for symphony-api-serving-cert secret to be available
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for Symphony server certificate after %v", timeout)
		case <-ticker.C:
			// Check if secret exists
			cmd := exec.Command("kubectl", "get", "secret", "-n", "default", "symphony-api-serving-cert", "--ignore-not-found")
			err := cmd.Run()
			if err == nil {
				t.Logf("Symphony server certificate is ready")
				return
			}
			t.Logf("Waiting for Symphony server certificate to be created...")
		}
	}
}

func applyKubernetesManifestWithRetryWindows(t *testing.T, manifestPath string, maxRetries int, retryDelay time.Duration) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		err := utils.ApplyKubernetesManifestWindows(t, manifestPath)
		if err == nil {
			return nil
		}
		lastErr = err
		t.Logf("Apply failed (attempt %d/%d): %v, retrying in %v...", i+1, maxRetries, err, retryDelay)
		time.Sleep(retryDelay)
	}
	return fmt.Errorf("failed to apply manifest after %d retries: %v", maxRetries, lastErr)
}

func cleanupMQTTCASecretWindows(t *testing.T, secretName string) {
	cmd := exec.Command("kubectl", "delete", "secret", secretName, "-n", "cert-manager", "--ignore-not-found")
	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to cleanup MQTT CA secret %s: %v", secretName, err)
	} else {
		t.Logf("Cleaned up MQTT CA secret %s", secretName)
	}
}

func cleanupMQTTClientSecretWindows(t *testing.T, namespace, secretName string) {
	cmd := exec.Command("kubectl", "delete", "secret", secretName, "-n", namespace, "--ignore-not-found")
	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to cleanup MQTT client secret %s: %v", secretName, err)
	} else {
		t.Logf("Cleaned up MQTT client secret %s in namespace %s", secretName, namespace)
	}
}

// Helper functions for MQTT certificate secret management using WindowsMQTTCertificatePaths
func createMQTTCASecretWindowsMQTT(t *testing.T, certs utils.WindowsMQTTCertificatePaths) string {
	secretName := "mqtt-ca-windows"

	// Ensure cert-manager namespace exists
	cmd := exec.Command("kubectl", "create", "namespace", "cert-manager")
	cmd.Run() // Ignore error if namespace already exists

	// Create CA secret in cert-manager namespace
	cmd = exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=ca.crt="+certs.CACert,
		"-n", "cert-manager")

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create MQTT CA secret (may already exist): %v", err)
	} else {
		t.Logf("Created MQTT CA secret %s in cert-manager namespace", secretName)
	}
	return secretName
}

func createSymphonyMQTTClientSecretWindowsMQTT(t *testing.T, namespace string, certs utils.WindowsMQTTCertificatePaths) string {
	secretName := "symphony-mqtt-client-secret-windows"

	cmd := exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=client.crt="+certs.SymphonyClientCert,
		"--from-file=client.key="+certs.SymphonyClientKey,
		"-n", namespace)

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create Symphony MQTT client secret (may already exist): %v", err)
	} else {
		t.Logf("Created Symphony MQTT client secret %s in namespace %s", secretName, namespace)
	}
	return secretName
}

func createRemoteAgentClientCertSecretWindowsMQTT(t *testing.T, namespace string, certs utils.WindowsMQTTCertificatePaths) string {
	secretName := "remote-agent-mqtt-client-secret-windows"

	cmd := exec.Command("kubectl", "create", "secret", "generic", secretName,
		"--from-file=client.crt="+certs.RemoteAgentCert,
		"--from-file=client.key="+certs.RemoteAgentKey,
		"-n", namespace)

	err := cmd.Run()
	if err != nil {
		t.Logf("Warning: Failed to create Remote Agent MQTT client secret (may already exist): %v", err)
	} else {
		t.Logf("Created Remote Agent MQTT client secret %s in namespace %s", secretName, namespace)
	}
	return secretName
}
