# HTTP Error Simulation Client/Server for OpenShift Ingress Dashboard

## Overview

This project, consisting of a client application and a server application both implemented in Go, is specifically designed to simulate various HTTP errors. The primary goal is to facilitate the testing and validation of HTTP server response metrics within the "Ingress Dashboard" which can be found in the "Observe -> Dashboards" section of the OpenShift Console. 

### Client Application

The client application is designed to interact with an HTTP server that simulates HTTP errors. Key features include:
- Sending requests to test endpoints.
- Configuring error rates and response data sizes for each error code.
- Operates based on a test plan specified in a YAML file.

### Server Application

The server application simulates HTTP errors based on configurable rates. Its features include:
- Dynamically creating endpoints for simulating HTTP errors (error codes 400-599).
- Configuring simulation errors at different rates.
- Endpoints for setting response data size for each error code.

## Getting Started

### Prerequisites

- An OpenShift cluster
- A Go installation for building the client application
- YAML file for the test plan

### Installation

1. Clone the repository: `git clone https://github.com/frobware/openshift-ingress-dashboard-test`

### Configuration

- Create a [`test-plan.yaml`](./test-plan.yaml) file to specify the test plan for the client. This file should define the various scenarios and parameters you want the client to execute, including error rates, response sizes, and specific endpoints to target.

### Running the test

1. Deploy the server application to an OpenShift cluster:

```sh
make deploy-manifests
```

2. Build the client application:

```sh
make
```

3. Run the test plan:

```sh
./hack/run-test test-plan.yaml
```

## Monitoring the Simulation

### Using the Ingress Dashboard in OpenShift

To observe the simulation, you can view the "Ingress Dashboard" available in the OpenShift console. Follow these steps:

1. Log into your OpenShift console.
2. Navigate to Observe->Dashboards and in the drop-down search for "Ingress".
3. Monitor the "HTTP Server Response Error Rate" panel.

![Ingress Dashboard Metrics](screenshots/ingress-dashboard.png?raw=true "Ingress Dashboard Metrics")
