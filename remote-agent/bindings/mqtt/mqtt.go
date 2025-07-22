package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/model"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/certs"
	utils2 "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/utils"
	"github.com/eclipse-symphony/symphony/coa/pkg/logger/contexts"
	"github.com/eclipse-symphony/symphony/remote-agent/agent"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MqttBinding struct {
	CertProvider  certs.ICertProvider
	Agent         agent.Agent
	Client        mqtt.Client
	RequestTopic  string
	ResponseTopic string
	Target        string
	Namespace     string
}

var (
	ShouldEnd      string        = "false"
	ConcurrentJobs int           = 3
	Interval       time.Duration = 3 * time.Second
)

type Result struct {
	Result string `json:"result"`
}

var check_response = false
var responseReceived = make(chan bool, 10) // Buffered channel to avoid blocking

// Launch the polling agent
func (m *MqttBinding) Launch() error {
	// Start the agent by handling starter requests
	// get mqtt client
	var get_start = true
	var requests []map[string]interface{}
	Parameters := map[string]string{
		"target":    m.Target,
		"namespace": m.Namespace,
		"getAll":    "true",
		"preindex":  "0",
	}
	request := v1alpha2.COARequest{
		Route:      "tasks",
		Method:     "GET",
		Parameters: Parameters,
	}
	data, _ := json.Marshal(request)
	token := m.Client.Publish(m.RequestTopic, 0, false, data)
	token.Wait()
	var wg sync.WaitGroup
	// subscribe the response topic
	token = m.Client.Subscribe(m.ResponseTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		var coaResponse v1alpha2.COARequest
		err := utils2.UnmarshalJson(msg.Payload(), &coaResponse)
		if err != nil {
			fmt.Printf("Error unmarshalling response: %s", err.Error())
			return
		}
		fmt.Printf("Sub topic: %s. \n", msg.Topic())
		fmt.Printf("Sub topic coa: %s. \n", coaResponse.Body)
		// if check_response {

		fmt.Printf("Sub topiccoa11: %s. \n", coaResponse)
		var result Result
		err = utils2.UnmarshalJson(coaResponse.Body, &result)
		if result.Result != "" {
			fmt.Print("handle respponse A: ")
			// A逻辑
			fmt.Print("handle respponse resultA: %s. \n", result.Result)
			if strings.Contains(result.Result, "handle async result successfully") || strings.Contains(result.Result, "get response successfully") {
				select {
				case responseReceived <- true:
					fmt.Println("Response received successfully")
				default:
					fmt.Println("Response channel is full, skipping")
				}
			} else {
				fmt.Printf("Response not as expected: %s\n", result.Result)
				// Non-blocking send to responseReceived channel
				select {
				case responseReceived <- false:
					fmt.Println("Response received with errors")
				default:
					fmt.Println("Response channel is full, skipping")
				}
			}
		} else {
			// B逻辑
			fmt.Print("handle respponse B: ")
			if get_start {
				var allRequests model.ProviderPagingRequest
				err := utils2.UnmarshalJson(coaResponse.Body, &allRequests)
				if err != nil {
					fmt.Printf("Error unmarshalling response: %s", err.Error())
					return
				}
				fmt.Println("Request length: ", len(requests))
				requests = append(requests, allRequests.RequestList...)

				if allRequests.LastMessageID == "" {
					get_start = false
					fmt.Println("get_start: %s", get_start)
					handleRequests(requests, &wg, m)
					fmt.Println("Request length: ", len(requests))
				} else {
					fmt.Println("Request length: ", len(requests))
					Parameters := map[string]string{
						"target":    m.Target,
						"namespace": m.Namespace,
						"getAll":    "true",
						"preindex":  allRequests.LastMessageID,
					}
					request := v1alpha2.COARequest{
						Route:      "tasks",
						Method:     "GET",
						Parameters: Parameters,
					}
					data, _ := json.Marshal(request)
					token := m.Client.Publish(m.RequestTopic, 0, false, data)
					token.Wait()
				}
			} else {

				var singleRequest map[string]interface{}
				err := utils2.UnmarshalJson(coaResponse.Body, &singleRequest)
				fmt.Println("single request is here: ", singleRequest)
				if err != nil {
					fmt.Printf("Error unmarshalling response body: %s", err.Error())
					return
				}
				if strings.Contains(string(coaResponse.Body), m.Target) {
					if err != nil {
						fmt.Printf("Error unmarshalling response body: %s", err.Error())
						return
					}
					fmt.Printf("Sub topic00: %s. \n", singleRequest)
					// handle request
					requests = []map[string]interface{}{singleRequest}
					handleRequests(requests, &wg, m)
				}

			}
		}

	})
	token.Wait()
	fmt.Println("Subscribe topic done: ", m.ResponseTopic)

	// handle request one by one
	handleRequests(requests, &wg, m)

	// get new request
	for ShouldEnd == "false" {
		fmt.Println("Begin to pollRequests")
		m.pollRequests()
	}
	return nil
}

func handleRequests(requests []map[string]interface{}, wg *sync.WaitGroup, m *MqttBinding) {
	for _, request := range requests {
		wg.Add(1)
		fmt.Println("begin to handle request", request)
		go func(req map[string]interface{}) {
			defer wg.Done()
			correlationId, ok := req[contexts.ConstructHttpHeaderKeyForActivityLogContext(contexts.Activity_CorrelationId)].(string)
			if !ok {
				fmt.Println("error: correlationId not found or not a string. Using a mock one.")
				correlationId = "00000000-0000-0000-0000-000000000000"
			}
			fmt.Println("correlationId: ", correlationId)
			retCtx := context.TODO()
			retCtx = context.WithValue(retCtx, contexts.Activity_CorrelationId, correlationId)

			body, err := json.Marshal(req)
			if err != nil {
				fmt.Println("error marshalling request:", err)
				return
			}
			ret := m.Agent.Handle(body, retCtx)
			ret.Namespace = m.Namespace

			// Send response back
			result, err := json.Marshal(ret)
			if err != nil {
				fmt.Println("error marshalling response:", err)
			}
			fmt.Println("Agent response2:", string(result))

			response := v1alpha2.COARequest{
				Route:       "getResult",
				Method:      "POST",
				ContentType: "application/json",
				Body:        result,
			}
			data, err := json.Marshal(response)
			if err != nil {
				fmt.Printf("Error marshalling response: %s\n", err)
				return
			}
			fmt.Println("Publishing response to MQTT %s", data)
			token := m.Client.Publish(m.RequestTopic, 0, false, data)
			token.Wait()
			if token.Error() != nil {
				fmt.Printf("Error publishing response: %s\n", token.Error())
			} else {
				fmt.Println("Response published successfully")

				select {
				case success := <-responseReceived:
					if success {
						fmt.Println("Response received successfully")
					} else {
						fmt.Println("Response not received successfully")
					}
				case <-time.After(10 * time.Second):
					fmt.Println("Timeout waiting for response.")
				}
				// 清理 responseReceived 通道
				// select {
				// case <-responseReceived:
				// default:
				// }
			}
		}(request)
	}
	wg.Wait()
}

func (m *MqttBinding) pollRequests() {
	for i := 0; i < ConcurrentJobs; i++ {
		// Publish request to get jobs
		Parameters := map[string]string{
			"target":    m.Target,
			"namespace": m.Namespace,
		}
		request := v1alpha2.COARequest{
			Route:      "tasks",
			Method:     "GET",
			Parameters: Parameters,
		}
		fmt.Println("Begin to request topic Get task")
		data, _ := json.Marshal(request)
		token := m.Client.Publish(m.RequestTopic, 0, false, data)
		token.Wait()
		time.Sleep(Interval)
	}
}
