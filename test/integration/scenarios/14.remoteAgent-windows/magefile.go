//go:build mage

/*
 * Copyright (c) Microsoft Corporation.
 * Licensed under the MIT license.
 * SPDX-License-Identifier: MIT
 */

package main

import (
	"fmt"
	"os"

	"github.com/eclipse-symphony/symphony/test/integration/lib/testhelpers"
	"github.com/princjef/mageutil/shellcmd"
)

// Test config
const (
	TEST_NAME    = "Remote Agent Communication scenario (HTTP and MQTT) - Windows"
	TEST_TIMEOUT = "30m"
)

var (
	// Tests to run - ordered to run sequentially to avoid file conflicts
	testVerify = []string{
		"./verify -run TestE2EHttpCommunicationWithBootstrapWindows",
		"./verify -run TestE2EMQTTCommunicationWithBootstrapWindows",
		"./verify -run TestE2EHttpCommunicationWithProcessWindows",
		"./verify -run TestE2EMQTTCommunicationWithProcessWindows",
	}
)

// Entry point for running the tests
func Test() error {
	fmt.Println("Running ", TEST_NAME)

	defer testhelpers.Cleanup(TEST_NAME)
	err := testhelpers.SetupCluster()
	if err != nil {
		return err
	}

	err = Verify()
	if err != nil {
		return err
	}

	return nil
}

// Run tests
func Verify() error {
	err := shellcmd.Command("go clean -testcache").Run()
	if err != nil {
		return err
	}

	os.Setenv("SYMPHONY_FLAVOR", "oss")
	for _, verify := range testVerify {
		err := shellcmd.Command(fmt.Sprintf("go test -v -timeout %s %s", TEST_TIMEOUT, verify)).Run()
		if err != nil {
			return err
		}
	}

	return nil
}

// Setup prepares the test environment
func Setup() error {
	fmt.Println("Setting up Remote Agent Windows test environment...")
	return testhelpers.SetupCluster()
}

// Cleanup cleans up test resources
func Cleanup() error {
	fmt.Println("Cleaning up Remote Agent Windows test resources...")
	testhelpers.Cleanup(TEST_NAME)
	return nil
}
