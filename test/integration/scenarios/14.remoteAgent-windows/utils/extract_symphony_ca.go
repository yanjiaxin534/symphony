package utils

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// ExtractSymphonyCAToFileWindows extracts the Symphony CA certificate from Kubernetes secret and saves it to a file
func ExtractSymphonyCAToFileWindows(t *testing.T, testDir string) string {
	t.Logf("Extracting Symphony CA certificate from Kubernetes secret...")

	// Create the CA certificate file path
	symphonyCAPath := filepath.Join(testDir, "symphony-ca.crt")

	// First get the base64 encoded CA certificate
	cmd := exec.Command("kubectl", "get", "secret", "-n", "default", "symphony-api-serving-cert",
		"-o", "jsonpath={.data.ca\\.crt}")
	cmd.Dir = testDir

	base64Output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to extract Symphony CA certificate from secret: %v", err)
	}

	if len(base64Output) == 0 {
		t.Fatalf("Symphony CA certificate secret data is empty")
	}

	// Create a PowerShell script to decode base64 and save to file
	psScript := fmt.Sprintf(`
$base64Content = @"
%s
"@
$caCertContent = [System.Convert]::FromBase64String($base64Content.Trim())
$caCertString = [System.Text.Encoding]::UTF8.GetString($caCertContent)
$caCertString | Set-Content -Path "%s" -Encoding ASCII
Write-Output "CA certificate saved to: %s"
`, string(base64Output), symphonyCAPath, symphonyCAPath)

	// Create temporary PowerShell script file
	psScriptPath := filepath.Join(testDir, "extract_ca.ps1")
	err = CreateYAMLFileWindows(t, psScriptPath, psScript)
	if err != nil {
		t.Fatalf("Failed to create PowerShell script: %v", err)
	}

	// Execute PowerShell script
	psCmd := exec.Command("pwsh", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", psScriptPath)
	psCmd.Dir = testDir

	psOutput, err := psCmd.Output()
	if err != nil {
		t.Fatalf("Failed to execute PowerShell CA extraction script: %v", err)
	}

	t.Logf("PowerShell output: %s", string(psOutput))
	t.Logf("Symphony CA certificate extracted to: %s", symphonyCAPath)
	return symphonyCAPath
}

// ExtractSymphonyCAToFileWindowsWithTimeout extracts Symphony CA with timeout
func ExtractSymphonyCAToFileWindowsWithTimeout(t *testing.T, testDir string, timeout time.Duration) string {
	t.Logf("Extracting Symphony CA certificate with timeout %v...", timeout)

	// Create the CA certificate file path
	symphonyCAPath := filepath.Join(testDir, "symphony-ca.crt")

	// Create a command with timeout context
	cmd := exec.Command("cmd", "/C",
		"kubectl get secret -n default symphony-api-serving-cert -o jsonpath=\"{.data.ca\\.crt}\" | certutil -decode - -")
	cmd.Dir = testDir

	// Set timeout
	done := make(chan error, 1)
	go func() {
		output, err := cmd.Output()
		if err != nil {
			done <- fmt.Errorf("kubectl command failed: %v", err)
			return
		}

		if len(output) == 0 {
			done <- fmt.Errorf("Symphony CA certificate is empty")
			return
		}

		// Write the CA certificate to file
		err = CreateYAMLFileWindows(t, symphonyCAPath, string(output))
		if err != nil {
			done <- fmt.Errorf("failed to write CA certificate: %v", err)
			return
		}

		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Failed to extract Symphony CA certificate: %v", err)
		}
	case <-time.After(timeout):
		t.Fatalf("Timeout waiting for Symphony CA certificate extraction")
	}

	t.Logf("Symphony CA certificate extracted to: %s", symphonyCAPath)
	return symphonyCAPath
}
