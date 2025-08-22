//go:build windows
// +build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "symphony-service"
const serviceDesc = "Symphony Remote Agent Service"

type symphonyService struct {
	workerCmd *exec.Cmd
	mutex     sync.Mutex
	stopChan  chan bool
}

func (s *symphonyService) Execute(args []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	log.Printf("Service Execute called with args: %v", args)

	s.stopChan = make(chan bool, 1)
	status <- svc.Status{State: svc.StartPending}

	// Start the worker process
	if err := s.startWorker(args); err != nil {
		log.Printf("Failed to start worker: %v", err)
		return false, 1
	}

	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	log.Printf("Service is now running")

	// Monitor worker process in a separate goroutine
	go s.monitorWorker()

	// Service control loop
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				log.Printf("Service stop/shutdown requested")
				status <- svc.Status{State: svc.StopPending}
				s.stopWorker()
				return false, 0
			default:
				log.Printf("Unexpected service command: %d", c.Cmd)
			}
		case <-s.stopChan:
			log.Printf("Worker process stopped unexpectedly")
			status <- svc.Status{State: svc.StopPending}
			return false, 1
		}
	}
}

func (s *symphonyService) startWorker(args []string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.workerCmd != nil && s.workerCmd.Process != nil {
		return fmt.Errorf("worker is already running")
	}

	// Get the directory where the service executable is located
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}
	execDir := filepath.Dir(execPath)

	// Look for remote-agent.exe in the same directory
	agentPath := filepath.Join(execDir, "remote-agent.exe")
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		return fmt.Errorf("remote-agent.exe not found at %s", agentPath)
	}

	log.Printf("Starting worker: %s with args: %v", agentPath, args)

	s.workerCmd = exec.Command(agentPath, args...)
	s.workerCmd.Dir = execDir

	// Set up logging for the worker process
	logFile, err := os.OpenFile(filepath.Join(execDir, "symphony-service.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("Warning: failed to open service log file2: %v", err)
	} else {
		s.workerCmd.Stdout = logFile
		s.workerCmd.Stderr = logFile
	}

	if err := s.workerCmd.Start(); err != nil {
		return fmt.Errorf("failed to start worker process: %v", err)
	}

	log.Printf("Worker process started with PID: %d", s.workerCmd.Process.Pid)
	return nil
}

func (s *symphonyService) stopWorker() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.workerCmd == nil || s.workerCmd.Process == nil {
		log.Printf("No worker process to stop")
		return
	}

	log.Printf("Stopping worker process (PID: %d)", s.workerCmd.Process.Pid)

	// Try graceful shutdown first
	if err := s.workerCmd.Process.Signal(os.Interrupt); err != nil {
		log.Printf("Failed to send interrupt signal: %v", err)
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		done <- s.workerCmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Printf("Worker process exited with error: %v", err)
		} else {
			log.Printf("Worker process exited gracefully")
		}
	case <-time.After(30 * time.Second):
		log.Printf("Worker process did not exit gracefully, forcing termination")
		if err := s.workerCmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill worker process: %v", err)
		}
		<-done // Wait for process to actually exit
	}

	s.workerCmd = nil
}

func (s *symphonyService) monitorWorker() {
	s.mutex.Lock()
	cmd := s.workerCmd
	s.mutex.Unlock()

	if cmd == nil {
		return
	}

	// Wait for the worker process to exit
	err := cmd.Wait()

	s.mutex.Lock()
	s.workerCmd = nil
	s.mutex.Unlock()

	if err != nil {
		log.Printf("Worker process exited with error: %v", err)
	} else {
		log.Printf("Worker process exited normally")
	}

	// Signal the service to stop
	select {
	case s.stopChan <- true:
	default:
	}
}

func isWindowsService() bool {
	log.Printf("Checking if running as a Windows service")
	ok, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("svc.IsWindowsService() error: %v", err)
	}
	return ok
}

func installService(exePath string, args []string) error {
	log.Printf("Installing service %s at %s with args: %v", serviceName, exePath, args)
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", serviceName)
	}

	// Build the full command line
	binPath := "\"" + exePath + "\""
	for _, arg := range args {
		binPath += " " + arg
	}

	s, err = m.CreateService(serviceName, binPath, mgr.Config{
		DisplayName: serviceName,
		Description: serviceDesc,
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return err
	}
	defer s.Close()

	err = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return fmt.Errorf("SetupEventLogSource() failed: %s", err)
	}
	return nil
}

func removeService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", serviceName)
	}
	defer s.Close()

	if err := s.Delete(); err != nil {
		return err
	}
	_ = eventlog.Remove(serviceName)
	return nil
}

func startService() error {
	log.Printf("Starting service %s", serviceName)
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", serviceName)
	}
	defer s.Close()

	return s.Start()
}

func stopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", serviceName)
	}
	defer s.Close()

	status, err := s.Control(svc.Stop)
	if err != nil {
		return err
	}

	for status.State != svc.Stopped {
		status, err = s.Query()
		if err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func main() {
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path: %v", err)
	}

	// Set up logging
	execDir := filepath.Dir(execPath)
	logFile, err := os.OpenFile(filepath.Join(execDir, "symphony-service.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("Warning: failed to open log file: %v", err)
	} else {
		log.SetOutput(logFile)
	}

	log.Printf("Symphony Service started with args: %v", os.Args)

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			// Pass all arguments after "install" to the worker
			if err := installService(execPath, os.Args[2:]); err != nil {
				log.Fatalf("Failed to install service: %v", err)
			}
			fmt.Println("Service installed with arguments:", os.Args[2:])
			return
		case "uninstall":
			if err := removeService(); err != nil {
				log.Fatalf("Failed to remove service: %v", err)
			}
			fmt.Println("Service removed.")
			return
		case "start":
			if err := startService(); err != nil {
				log.Fatalf("Failed to start service: %v", err)
			}
			fmt.Println("Service started.")
			return
		case "stop":
			if err := stopService(); err != nil {
				log.Fatalf("Failed to stop service: %v", err)
			}
			fmt.Println("Service stopped.")
			return
		}
	}

	isService := isWindowsService()
	if isService {
		err := svc.Run(serviceName, &symphonyService{})
		if err != nil {
			log.Fatalf("Service failed: %v", err)
		}
		return
	}

	// If running in console mode, just start the worker directly
	service := &symphonyService{}
	if err := service.startWorker(os.Args[1:]); err != nil {
		log.Fatalf("Failed to start worker in console mode: %v", err)
	}

	// Wait for worker to finish
	service.monitorWorker()
}
