// Package main implements a client application designed to interact
// with an HTTP server that simulates various HTTP errors. The client
// sends requests to test endpoints and configures error rates and
// response data sizes for each error code. It is primarily used for
// testing and validating server behaviour under different error
// conditions and response sizes.
//
// The client supports configurable parameters for domain, namespace,
// and HTTP scheme, and it operates based on a test plan specified in
// a YAML file. This plan dictates the error conditions to simulate,
// including the error codes, error rates, and response sizes.
//
// Upon execution, the client sets up the desired error rates and
// response sizes on the server using the provided configuration. It
// then sends requests according to the test plan, collecting data on
// the server's response to each request. This includes tracking the
// total number of requests, the actual versus expected error rates,
// and response times.
//
// The application also supports graceful shutdown, allowing it to
// complete ongoing requests and compile results even when
// interrupted.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v2"
)

// RequestSendCondition is a function type that defines a condition
// used to determine whether to continue sending HTTP requests. It
// returns true if the condition to continue is met, false otherwise.
type RequestSendCondition func() bool

// Config represents the configuration for a test plan. It is
// structured according to a YAML file and includes various error
// plans for testing.
type Config struct {
	// ErrorPlans is a slice of ErrorPlan that defines the various
	// error conditions and parameters for each test scenario.
	// These plans are used to simulate and test different HTTP
	// error responses as specified in the test plan YAML file.
	ErrorPlans []ErrorPlan `yaml:"errorPlans"`
}

// ErrorPlan defines the configuration for simulating HTTP errors. It
// includes details like the error code to simulate, the rate of
// errors, the mode of operation (indefinite, fixed, duration), and
// other parameters that control the behaviour of the test.
type ErrorPlan struct {
	// ErrorCode specifies the HTTP error code to simulate in this
	// error plan.
	ErrorCode int `yaml:"errorCode"`

	// ErrorRate defines the percentage of requests that should
	// result in the simulated error.
	ErrorRate int `yaml:"errorRate"`

	// Mode describes the operational mode of the test plan. It
	// can be "indefinite", "fixed", or "duration".
	Mode string `yaml:"mode"`

	// RequestRatePerSecond is the rate at which requests are
	// sent, expressed as requests per second.
	RequestRatePerSecond int `yaml:"requestRatePerSecond"`

	// Requests denotes the total number of requests to send when
	// in "fixed" mode.
	Requests int `yaml:"requests"`

	// Duration specifies the length of time to run the test when
	// in "duration" mode. It should be a string representing a
	// valid duration, such as "5m" or "2h".
	Duration string `yaml:"duration"`

	// ResponseDataSize specifies the size of the response data
	// for successful requests as a string (e.g., "50KB",
	// "100MB").
	ResponseDataSize string `yaml:"responseDataSize"`

	// ParsedResponseDataSize stores the parsed integer value of
	// ResponseDataSize. It represents the size of the response
	// data in bytes. This field is populated by parsing
	// ResponseDataSize at runtime.
	ParsedResponseDataSize int
}

// RequestResult holds the aggregated results of test requests for
// each error plan. It maps each error code to its corresponding
// results and uses a mutex for safe concurrent access.
type RequestResult struct {
	// Results associates each error code with its corresponding
	// ErrorCodeResult. It stores the detailed results of the
	// requests made for each error plan.
	Results map[int]*ErrorCodeResult

	// mutex is used to ensure thread-safe access to the Results
	// map, allowing concurrent operations on this data structure.
	mutex sync.Mutex
}

// ErrorCodeResult captures the results for a specific error plan. It
// includes the total number of requests sent, the expected and actual
// error rates, and a count of responses received for each HTTP status
// code.
// ErrorCodeResult captures the results for a specific error plan.
// It includes the total number of requests sent, the expected and actual
// error rates, and a count of responses received for each HTTP status code.
type ErrorCodeResult struct {
	// TotalRequests is the total number of requests sent for this
	// error plan.
	TotalRequests int `json:"totalRequests"`

	// ExpectedErrorRate is the error rate defined in the plan,
	// representing the anticipated percentage of requests that
	// result in an error.
	ExpectedErrorRate int `json:"expectedErrorRate"`

	// ActualErrorRate is the error rate calculated based on the
	// test results, indicating the actual percentage of requests
	// that resulted in an error.
	ActualErrorRate float64 `json:"actualErrorRate"`

	// ResponseCounts is a map that records the occurrence count
	// of each HTTP response code received during the test. It
	// includes both error and successful response counts.
	ResponseCounts map[int]int `json:"responseCounts"`

	// TotalLatencyInNanos accumulates the total response time for
	// all requests sent as part of this error plan. It is used to
	// calculate the average latency.
	TotalLatencyInNanos time.Duration `json:"totalLatency"`

	// AvgLatencyInMillis represents the average response time for
	// requests in this error plan, calculated as the total
	// response time divided by the number of requests. It is
	// expressed in milliseconds.
	AvgLatencyInMillis time.Duration `json:"averageLatency"`
}

// newErrorCodeResult creates and returns a new instance of
// ErrorCodeResult. This function initialises the ErrorCodeResult
// struct, particularly the ResponseCounts map, which tracks the
// occurrence of each HTTP response code.
func newErrorCodeResult() *ErrorCodeResult {
	return &ErrorCodeResult{
		ResponseCounts: make(map[int]int),
	}
}

// newRequestResult initialises and returns a new instance of
// RequestResult for the given error codes. It creates a map within
// the RequestResult to hold ErrorCodeResult instances for each
// provided error code. This setup is used to store and track the
// results of requests made for each error scenario in the test plan.
func newRequestResult(errorCodes []int) *RequestResult {
	result := &RequestResult{
		Results: make(map[int]*ErrorCodeResult),
	}

	for _, code := range errorCodes {
		result.Results[code] = newErrorCodeResult()
	}

	return result
}

// calculateErrorRates computes the actual error rates for each error
// plan based on the test results. It iterates over each plan in the
// given configuration and calculates the actual error rate as a
// percentage. This percentage is determined by comparing the total
// number of errors against the total number of requests.
func calculateErrorRates(result *RequestResult, config Config) {
	for _, plan := range config.ErrorPlans {
		errResult := result.Results[plan.ErrorCode]
		if errResult != nil {
			errResult.ExpectedErrorRate = plan.ErrorRate
			totalErrors := 0
			for responseCode, count := range errResult.ResponseCounts {
				if responseCode != http.StatusOK {
					totalErrors += count
				}
			}
			if errResult.TotalRequests > 0 {
				errResult.ActualErrorRate = (float64(totalErrors) / float64(errResult.TotalRequests)) * 100
				avgLatencyInNano := errResult.TotalLatencyInNanos / time.Duration(errResult.TotalRequests)
				errResult.AvgLatencyInMillis = avgLatencyInNano / time.Millisecond
			}
		}
	}
}

// loadConfig reads a YAML file and unmarshalls its content into a
// Config struct. It returns the loaded Config and any error
// encountered during reading or parsing the file. This function is
// used to load test plans from a YAML configuration file, where each
// plan specifies parameters for simulating HTTP errors.
func loadConfig(filename string) (Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("error reading YAML file: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return Config{}, fmt.Errorf("error parsing YAML file: %w", err)
	}

	return config, nil
}

// parseDataSize converts a data size string (like "50KB", "100MB") to
// an integer number of bytes. If the size string is not valid, it
// returns an error.
func parseDataSize(sizeStr string) (int, error) {
	var size int
	var unit string
	_, err := fmt.Sscanf(sizeStr, "%d%s", &size, &unit)
	if err != nil {
		return 0, err
	}

	switch strings.ToUpper(unit) {
	case "KB":
		return size * 1024, nil
	case "MB":
		return size * 1024 * 1024, nil
	case "GB":
		return size * 1024 * 1024 * 1024, nil
	default:
		return size, nil // Assuming no unit means bytes
	}
}

// setErrorRateOnServer makes an HTTP POST request to a server to set
// the error rate for a specific error code. This function is used to
// configure the server to return the specified error code at the
// given rate, effectively simulating HTTP errors as per the test
// plan.
func setErrorRateOnServer(scheme, namespace, domain string, errorCode, errorRate int) error {
	fullURL := fmt.Sprintf("%s://ocpstrat139-%d-%s.%s/set-error-rate", scheme, errorCode, namespace, domain)
	formData := url.Values{}
	formData.Set("errorCode", strconv.Itoa(errorCode))
	formData.Set("errorRate", strconv.Itoa(errorRate))

	resp, err := http.PostForm(fullURL, formData)
	if err != nil {
		return fmt.Errorf("error setting error rate: %w", err)
	}

	if resp == nil {
		return fmt.Errorf("received nil response when attempting to set error rate for %d", errorCode)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to set error rate for %d: HTTP %d", errorCode, resp.StatusCode)
	}

	return nil
}

// setResponseDataSizeOnServer sends a POST request to the server to
// set the response data size for a specific HTTP error code. This
// function constructs the request URL using the provided namespace,
// domain, and error code, and sends the data size as part of the POST
// form data.
//
// Parameters:
//
//	scheme -    The HTTP scheme (i.e., http, https).
//	namespace - The namespace of the OpenShift route.
//	domain    - The domain of the OpenShift route.
//	errorCode - The HTTP error code for which the response data size is set.
//	dataSize  - The size of the response data to be set, in bytes.
//
// The function forms a URL targeting the '/set-response-data-size'
// endpoint on the server. It sends the error code and data size as
// form values. If the request is successful, the server configures
// the specified error code endpoint to respond with the given data
// size for successful requests.
//
// Returns:
//
// An error if the request fails or if the server responds with a
// status code other than HTTP 200 OK. Otherwise, it returns nil
// indicating successful configuration.
func setResponseDataSizeOnServer(scheme, namespace, domain string, errorCode, dataSize int) error {
	fullURL := fmt.Sprintf("%s://ocpstrat139-%d-%s.%s/set-response-data-size", scheme, errorCode, namespace, domain)
	formData := url.Values{}
	formData.Set("errorCode", strconv.Itoa(errorCode))
	formData.Set("size", strconv.Itoa(dataSize))

	resp, err := http.PostForm(fullURL, formData)
	if err != nil {
		return fmt.Errorf("error setting response data size: %w", err)
	}

	if resp == nil {
		return fmt.Errorf("received nil response when attempting to set error rate for %d", errorCode)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to set response data size for %d: HTTP %d", errorCode, resp.StatusCode)
	}
	return nil
}

// sendRequestAndUpdateResult sends an HTTP GET request to the
// specified URL and updates the test results based on the response.
// It increments the total request count and records the response
// status code in the appropriate ErrorCodeResult. This function is
// called concurrently for each request made as part of the test plan.
func sendRequestAndUpdateResult(scheme, namespace, domain string, plan ErrorPlan, result *RequestResult, wg *sync.WaitGroup) error {
	defer wg.Done()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	fullURL := fmt.Sprintf("%s://ocpstrat139-%d-%s.%s/simulate-%d",
		scheme, plan.ErrorCode, namespace, domain, plan.ErrorCode)

	startTime := time.Now()

	resp, err := client.Get(fullURL)
	if err != nil {
		return fmt.Errorf("error sending request to %s: %w", fullURL, err)
	}

	if resp == nil {
		return fmt.Errorf("received nil response from GET: %s", fullURL)
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(resp.Body)

	latency := time.Since(startTime)

	result.mutex.Lock()
	defer result.mutex.Unlock()

	errResult, exists := result.Results[plan.ErrorCode]
	if !exists {
		// While this check may seem redundant due to
		// initialisation in newRequestResult, it safeguards
		// against future changes that could alter the
		// initialisation logic, potentially leading to nil
		// dereferences.
		errResult = newErrorCodeResult()
		result.Results[plan.ErrorCode] = errResult
	}

	errResult.TotalRequests++
	errResult.ResponseCounts[resp.StatusCode]++
	errResult.TotalLatencyInNanos += latency

	return nil
}

// sendRequests dispatches HTTP requests according to the specified
// ErrorPlan. Depending on the mode set in the plan, it calls the
// appropriate function to send requests either indefinitely, for a
// fixed number of requests, or for a specific duration. The function
// uses context for graceful cancellation of ongoing requests.
func sendRequests(ctx context.Context, scheme, namespace, domain string, plan ErrorPlan, result *RequestResult, wg *sync.WaitGroup) {
	var condition RequestSendCondition

	alwaysTrue := func() bool {
		return true
	}

	untilDuration := func(duration time.Duration) RequestSendCondition {
		end := time.Now().Add(duration)
		return func() bool {
			return time.Now().Before(end)
		}
	}

	forFixedRequests := func(totalRequests int) RequestSendCondition {
		counter := 0
		return func() bool {
			counter++
			return counter <= totalRequests
		}
	}

	switch plan.Mode {
	case "indefinite":
		condition = alwaysTrue
	case "duration":
		duration, err := time.ParseDuration(plan.Duration)
		if err != nil {
			log.Printf("Invalid duration format: %s", err)
			return
		}
		condition = untilDuration(duration)
	case "fixed":
		condition = forFixedRequests(plan.Requests)
	default:
		log.Fatalf("Invalid mode: %s", plan.Mode)
		return
	}

	sendRequestsGeneric(ctx, scheme, namespace, domain, plan, result, wg, condition)
}

// createRequestTicker creates and returns a new ticker that ticks at
// a rate determined by the request rate per second specified in the
// ErrorPlan. This ticker is used to control the rate at which HTTP
// requests are sent in the test plan.
func createRequestTicker(plan ErrorPlan) *time.Ticker {
	rate := plan.RequestRatePerSecond
	if rate <= 0 {
		rate = 1 // default to 1 request/s.
	}
	return time.NewTicker(time.Second / time.Duration(rate))
}

// sendRequestsGeneric is a generic function to send requests based on
// a given ErrorPlan. It handles the timing and logging of requests
// and uses a provided condition function to determine when to stop
// sending requests. This function is utilised by sendRequests to
// implement the logic for different modes of request dispatch
// (indefinite, fixed, duration).
func sendRequestsGeneric(ctx context.Context, scheme, namespace, domain string, plan ErrorPlan, result *RequestResult, wg *sync.WaitGroup, condition RequestSendCondition) {
	ticker := createRequestTicker(plan)
	defer ticker.Stop()

	requestCounter := 0
	logTicker := time.NewTicker(time.Second)
	defer logTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !condition() {
				return
			}
			wg.Add(1)
			go sendRequestAndUpdateResult(scheme, namespace, domain, plan, result, wg)
			requestCounter++
		case <-logTicker.C:
			log.Printf("/simulate-%d: %d req/s", plan.ErrorCode, requestCounter)
			requestCounter = 0
		}
	}
}

// main sets up and executes the HTTP error simulation client.
//
// The client performs the following steps:
//
//   - Parses command-line arguments to get the path to the test plan YAML
//     file ('test-plan'), the domain ('domain'), and the namespace
//     ('namespace') of the OpenShift route.
//
//   - Loads the test plan configuration from the specified YAML file. This
//     configuration includes various error plans with specified error
//     codes, error rates, and response data sizes.
//
//   - Initialises a result tracker for the different HTTP error codes.
//
//   - Sets the error rates and response data sizes on the target server
//     for each error code as defined in the test plan. This involves
//     sending configuration requests to the server's '/set-error-rate'
//     and '/set-response-data-size' endpoints.
//
//   - Concurrently sends requests according to each error plan, which
//     includes simulating the specified errors and measuring the server's
//     response.
//
// The client also handles graceful shutdown. It listens for an interrupt
// signal (SIGINT), and upon receiving this signal, cancels all ongoing
// requests, waits for their completion, and then calculates and prints
// the error simulation results.
func main() {
	domain := flag.String("domain", "", "Domain of the OpenShift route")
	namespace := flag.String("namespace", "", "Namespace of the OpenShift route")
	scheme := flag.String("scheme", "http", "HTTP scheme")
	testPlanFile := flag.String("test-plan", "", "Path to the test plan YAML file")

	flag.Parse()

	// Check if the required flags are provided.
	if *testPlanFile == "" || *domain == "" || *namespace == "" {
		log.Fatal("Error: Test plan file, domain, and namespace are required.")
	}

	config, err := loadConfig(*testPlanFile)
	if err != nil {
		log.Fatalf("Failed to load test configuration: %s", err)
	}

	var errorCodes = make([]int, 0, len(config.ErrorPlans))

	for i, plan := range config.ErrorPlans {
		if err := setErrorRateOnServer(*scheme, *namespace, *domain, plan.ErrorCode, plan.ErrorRate); err != nil {
			log.Fatalf("Error setting error rate: %s", err)
		}

		size, err := parseDataSize(plan.ResponseDataSize)
		if err != nil {
			log.Fatalf("Invalid response data size for error code %d: %s", plan.ErrorCode, err)
		}
		config.ErrorPlans[i].ParsedResponseDataSize = size

		if err := setResponseDataSizeOnServer(*scheme, *namespace, *domain, plan.ErrorCode, plan.ParsedResponseDataSize); err != nil {
			log.Fatalf("Error setting response data size: %s", err)
		}
		errorCodes = append(errorCodes, plan.ErrorCode)
	}

	if len(errorCodes) == 0 {
		log.Fatalf("Error: Zero plans listed in %s", *testPlanFile)
	}

	result := newRequestResult(errorCodes)

	for _, plan := range config.ErrorPlans {
		if err := setErrorRateOnServer(*scheme, *namespace, *domain, plan.ErrorCode, plan.ErrorRate); err != nil {
			log.Fatal(err)
		}
	}

	wg := &sync.WaitGroup{}
	ctx, cancelRequests := context.WithCancel(context.Background())

	for _, plan := range config.ErrorPlans {
		wg.Add(1)
		go func(plan ErrorPlan) {
			defer wg.Done()
			sendRequests(ctx, *scheme, *namespace, *domain, plan, result, wg)
		}(plan)
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if sig := <-stopChan; sig == os.Interrupt {
			// We add a newline to the output. This is a
			// small quality-of-life improvement that
			// ensures the shutdown log message appears on
			// a new line after the '^C' line printed by
			// the terminal.
			fmt.Println()
		}
		log.Println("Waiting for pending requests to complete...")

		// signal that all HTTP requests should stop.
		cancelRequests()
	}()

	wg.Wait()

	calculateErrorRates(result, config)

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling results to JSON: %s", err)
	}
	if _, err := fmt.Println(string(jsonData)); err != nil {
		log.Fatal(err)
	}
}
