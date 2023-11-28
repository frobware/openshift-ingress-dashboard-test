// Package main implements an HTTP server for simulating various HTTP
// errors based on configurable rates. The server dynamically creates
// endpoints for simulating HTTP errors with codes ranging from 400 to
// 599, each of which can be configured to simulate errors at
// different rates. Additionally, it provides endpoints for setting
// the response data size for each error code, allowing for testing of
// different response sizes.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	// errorRates holds the simulation error rates for different
	// HTTP error codes. The map's key is the HTTP error code
	// (e.g., 404, 500), and the value is the percentage (0-100)
	// representing the probability that a request to the
	// corresponding error simulation endpoint will result in an
	// error response.
	errorRates map[int]int

	// endpointResponses maps each error code to its pre-generated
	// response data. This response data is sent only when there
	// is no simulated error for the corresponding error code.
	endpointResponses map[int]string

	// mutex is used to synchronise access to both the errorRates
	// and endpointResponses maps.
	mutex sync.Mutex
)

func init() {
	errorRates = make(map[int]int)
	endpointResponses = make(map[int]string)
}

// getResponseData retrieves the response data for the given error
// code.
func getResponseData(errorCode int) string {
	mutex.Lock()
	defer mutex.Unlock()
	data, exists := endpointResponses[errorCode]
	if !exists {
		return "dummy data"
	}
	return data
}

// generateResponseData creates a response data string for the given
// error code. The data consists of the string version of the error
// code, repeated up to the specified size in bytes.
func generateResponseData(errorCode, size int) string {
	errorCodeStr := strconv.Itoa(errorCode)
	var responseData strings.Builder

	// Keep appending the error code string until the desired size
	// is reached.
	for responseData.Len() < size {
		responseData.WriteString(errorCodeStr)
	}

	// If the last append exceeds the size, truncate to the exact
	// size.
	if responseData.Len() > size {
		return responseData.String()[:size]
	}

	return responseData.String()
}

// makeErrorHandler returns a http.HandlerFunc that simulates HTTP
// errors based on a specified error code. If an error should be
// simulated, it returns an HTTP error response. Otherwise, it
// retrieves and returns the pre-generated static response data
// associated with the error code.
func makeErrorHandler(errorCode int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if shouldSimulateError(errorCode) {
			http.Error(w, fmt.Sprintf("Simulated Error: %d", errorCode), errorCode)
			return
		}
		if _, err := fmt.Fprint(w, getResponseData(errorCode)); err != nil {
			log.Println(err)
		}
	}
}

// setErrorRate is an HTTP handler function that sets the error
// simulation rate for a specific HTTP error code. It expects a POST
// request with 'errorCode' and 'errorRate' form values. The error
// rate is stored in the global errorRates map, which is protected by
// a mutex for concurrent access.
func setErrorRate(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	errorCode, err := strconv.Atoi(req.FormValue("errorCode"))
	if err != nil {
		http.Error(w, "Invalid error code", http.StatusBadRequest)
		return
	}

	errRate, err := strconv.Atoi(req.FormValue("errorRate"))
	if err != nil {
		http.Error(w, "Invalid error rate", http.StatusBadRequest)
		return
	}

	mutex.Lock()
	errorRates[errorCode] = errRate
	mutex.Unlock()

	if _, err := fmt.Fprintf(w, "Error rate for error code %d set to %d%%", errorCode, errRate); err != nil {
		log.Println(err)
	}
}

// setResponseDataSize is an HTTP handler function that sets the
// response data size and generates the response data for a specific
// HTTP error code. It expects a POST request with 'errorCode' and
// 'size' form values. The size and generated data are stored in
// global maps, protected by a mutex for concurrent access.
func setResponseDataSize(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	errorCode, err := strconv.Atoi(req.FormValue("errorCode"))
	if err != nil {
		http.Error(w, "Invalid error code", http.StatusBadRequest)
		return
	}

	size, err := strconv.Atoi(req.FormValue("size"))
	if err != nil {
		http.Error(w, "Invalid size", http.StatusBadRequest)
		return
	}

	responseData := generateResponseData(errorCode, size)

	mutex.Lock()
	endpointResponses[errorCode] = responseData
	mutex.Unlock()

	if _, err := fmt.Fprintf(w, "Response data size for error code %d set to %d", errorCode, size); err != nil {
		log.Println(err)
	}
}

// shouldSimulateError determines whether an error should be simulated
// for a given error code. It checks the error rate associated with
// the error code in the errorRates map and uses a random number
// generator to decide whether to simulate the error based on the
// error rate. It returns true if an error should be simulated, and
// false otherwise.
func shouldSimulateError(errorCode int) bool {
	mutex.Lock()
	rate, exists := errorRates[errorCode]
	mutex.Unlock()

	if !exists {
		// If no specific rate is set for this error code,
		// assume 0% error rate.
		return false
	}

	return rand.Intn(100) < rate
}

// main initialises the server and sets up HTTP routes for health
// checks, error rate configuration, dynamic error simulation, and
// setting response data sizes for simulated errors.
//
// The server listens on the specified port (default 8080) and
// provides the following endpoints:
//
//   - "/healthz": A health check endpoint that logs the connection and
//     responds with a health status.
//
//   - "/set-error-rate": An endpoint to configure the error simulation
//     rate for different HTTP error codes.
//
//   - "/set-response-data-size": An endpoint to set the size of the
//     response data for successful requests on a per-error-code basis.
//     This allows for testing the handling of responses of different
//     sizes.
//
// Additionally, the server dynamically creates endpoints for
// simulating HTTP errors with codes ranging from 400 to 599. Each of
// these endpoints simulates the corresponding HTTP error based on
// configurable error rates and response data sizes.
//
// The server uses global maps 'errorRates' and 'endpointResponses' to
// store the error rates and response data sizes, respectively, for
// different error codes. A mutex 'mutex' ensures thread-safe access
// to these maps.
//
// The random number generator is seeded at startup to ensure varied
// error simulation outcomes. Note: Seeding the random number
// generator is not necessary for Go versions >= 1.20.
//
// Signal handling is implemented to gracefully shut down the server
// upon receiving SIGINT or SIGTERM.
func main() {
	var port string
	flag.StringVar(&port, "port", "8080", "HTTP server port")
	flag.Parse()

	http.HandleFunc("/healthz", func(w http.ResponseWriter, req *http.Request) {
		log.Println("/healthz connection from", req.RemoteAddr)
		if _, err := fmt.Fprint(w, "healthy\n"); err != nil {
			log.Println(err)
		}
	})

	http.HandleFunc("/set-error-rate", setErrorRate)
	http.HandleFunc("/set-response-data-size", setResponseDataSize)

	for errorCode := 400; errorCode < 600; errorCode++ {
		http.HandleFunc(fmt.Sprintf("/simulate-%d", errorCode), makeErrorHandler(errorCode))
	}

	srv := &http.Server{Addr: ":" + port}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ListenAndServe(): %v", err)
		}
	}()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	if sig := <-stopChan; sig == os.Interrupt {
		// We add a newline to the output. This is a small
		// quality-of-life improvement that ensures the
		// shutdown log message appears on a new line after
		// the '^C' line printed by the shell.
		fmt.Println()
	}
	log.Println("Shutting down HTTP server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("Server Shutdown Failed: %+v", err)
	}
}
