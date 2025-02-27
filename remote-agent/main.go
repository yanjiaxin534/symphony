package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"net/http"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/providers/target/docker"
	targethttp "github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/providers/target/http"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/providers/target/script"
	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/providers/target/win10/sideload"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/model"
	tgt "github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/providers/target"
	"github.com/eclipse-symphony/symphony/remote-agent/agent"
	remoteMqtt "github.com/eclipse-symphony/symphony/remote-agent/bindings/mqtt"
	remoteProviders "github.com/eclipse-symphony/symphony/remote-agent/providers"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// The version should be hardcoded in the build process
const version = "0.0.0.1"

var (
	symphonyEndpoints model.SymphonyEndpoint
	clientCertPath    *string
	configPath        *string
	clientKeyPath     *string
	namespace         *string
	targetName        *string
	httpClient        *http.Client
	execDir           string
)

func main() {
	// Get the location of the currently running program
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}
	execPath, err = filepath.Abs(execPath)
	if err != nil {
		log.Fatalf("Failed to get absolute path: %v", err)
	}
	execDir = filepath.Dir(execPath)
	fmt.Printf("Executable directory: %s\n", execDir)

	// Create a transcript file in the current working directory
	transcriptFilePath := filepath.Join(execDir, "transcript.log")
	logFile, err := os.OpenFile(transcriptFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	// Redirect stdout and stderr to the transcript file
	log.SetOutput(logFile)
	os.Stdout = logFile
	os.Stderr = logFile

	// Allocate memory for shouldEnd
	// Define a command-line flag for the configuration file path
	configPath = flag.String("config", "config.json", "Path to the configuration file")
	clientCertPath = flag.String("client-cert", "client-cert.pem", "Path to the client certificate file")
	clientKeyPath = flag.String("client-key", "client-key.pem", "Path to the client key file")
	targetName = flag.String("target-name", "remote-target", "remote target name")
	namespace = flag.String("namespace", "default", "Namespace to use for the agent")
	topologyFile := flag.String("topology", "topology.json", "Path to the topology file")

	// Parse the command-line flags
	flag.Parse()

	// Read the configuration file
	setting, err := ioutil.ReadFile(*configPath)
	if err != nil {
		fmt.Println("Error reading configuration file:", err)
		return
	}

	// Load client cert
	clientCert, err := tls.LoadX509KeyPair(*clientCertPath, *clientKeyPath)
	if err != nil {
		fmt.Println("Error loading client certificate and key:", err)
		return
	}

	// Create TLS configuration
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
	}

	// Create HTTP client with TLS configuration
	httpClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	print(httpClient)
	err = json.Unmarshal(setting, &symphonyEndpoints)
	if err != nil {
		fmt.Println("Error unmarshalling configuration file:", err)
		return
	}

	// Compose target providers
	providers := composeTargetProviders(*topologyFile)
	// Create the HttpBinding instance
	// h := &remoteHttp.HttpBinding{
	// 	Agent: agent.Agent{
	// 		Providers: providers,
	// 	},
	// }

	// // Set up the request and response URLs
	// h.RequestUrl = symphonyEndpoints.RequestEndpoint
	// h.ResponseUrl = symphonyEndpoints.ResponseEndpoint
	// h.Client = httpClient
	// h.Target = *targetName
	// h.Namespace = *namespace

	// // Launch the HttpBinding
	// err = h.Launch()
	// if err != nil {
	// 	fmt.Println("Error launching HttpBinding:", err)
	// 	return
	// }

	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://52.237.223.156:32551")
	opts.SetClientID("symphony-client") // Set a unique ClientID
	// opts.SetUsername("abc")
	// opts.SetPassword("abcd")
	mqttClient := mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", token.Error())
	} else {
		fmt.Println("Connected to MQTT broker")
	}
	fmt.Println("Begin to request topic", "ddc")
	// m.RequestTopic = symphonyEndpoints.RequestEndpoint
	// m.ResponseTopic = symphonyEndpoints.ResponseEndpoint
	// for mqtt
	m := &remoteMqtt.MqttBinding{
		Agent: agent.Agent{
			Providers: providers,
		},
		Client: mqttClient,
	}
	m.RequestTopic = "symphony/request"
	m.ResponseTopic = "symphony/response"
	m.Target = *targetName
	m.Namespace = *namespace
	// initialRequest := fmt.Sprintf(`{"target":"%s","namespace":"%s","getAll":"true","preindex":"0"}`, m.Target, m.Namespace)
	// token := m.Client.Publish(m.RequestTopic, 0, false, initialRequest)
	// fmt.Println("Begin to launch %s", token)
	// m.Client.Subscribe(m.RequestTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
	// 	fmt.Printf("Sub topic: %d. \n", msg.Topic())
	// 	var allRequests model.ProviderPagingRequest
	// 	err := json.Unmarshal(msg.Payload(), &allRequests)
	// 	if err != nil {
	// 		fmt.Printf("Error unmarshalling response: %s", err.Error())
	// 		return
	// 	}
	// 	// requests = append(requests, allRequests.RequestList...)
	// 	if allRequests.LastMessageID == "" {
	// 		return
	// 	}
	// })
	m.Launch()
	// Keep the main function running
	select {}
}

func composeTargetProviders(topologyPath string) map[string]tgt.ITargetProvider {
	// read the topology file
	topologyContent, err := os.ReadFile(topologyPath)
	if err != nil {
		fmt.Println("Error reading topology file:", err)
		return nil
	}

	var topology model.TopologySpec
	json.Unmarshal(topologyContent, &topology)

	providers := make(map[string]tgt.ITargetProvider)
	// Add the target providers to the map
	// Add the script provider
	for _, binding := range topology.Bindings {
		switch binding.Provider {
		case "providers.target.script":
			provider := &script.ScriptProvider{}
			err := provider.Init(binding.Config)
			if err != nil {
				fmt.Println("Error initializing script provider:", err)
			}
			providers[binding.Role] = provider
		case "providers.target.remote-agent":
			rProvider := &remoteProviders.RemoteAgentProvider{}
			rProvider.Client = httpClient
			rProviderConfig := remoteProviders.RemoteAgentProviderConfig{
				PublicCertPath: *clientCertPath,
				PrivateKeyPath: *clientKeyPath,
				ConfigPath:     *configPath,
				BaseUrl:        symphonyEndpoints.BaseUrl,
				Version:        version,
				Namespace:      *namespace,
				TargetName:     *targetName,
				TopologyPath:   topologyPath,
				ExecDir:        execDir,
			}
			err = rProvider.Init(rProviderConfig)
			if err != nil {
				fmt.Println("Error remote agent provider:", err)
			}
			providers[binding.Role] = rProvider
		case "providers.target.win10.sideload":
			mProvider := &sideload.Win10SideLoadProvider{}
			err := mProvider.Init(binding.Config)
			if err != nil {
				fmt.Println("Error initializing win10.sideload provider:", err)
			}
			providers[binding.Role] = mProvider
		case "providers.target.docker":
			mProvider := &docker.DockerTargetProvider{}
			err = mProvider.Init(binding.Config)
			if err != nil {
				fmt.Println("Error initializing docker provider:", err)
			}
			providers[binding.Role] = mProvider
		case "providers.target.http":
			mProvider := &targethttp.HttpTargetProvider{}
			err = mProvider.Init(binding.Config)
			fmt.Println("binding.Config")
			fmt.Println(binding.Config)
			if err != nil {
				fmt.Println("Error initializing http provider:", err)
			}
			providers[binding.Role] = mProvider
		default:
			fmt.Println("Unknown provider type:", binding.Role)
		}

	}
	return providers
}
