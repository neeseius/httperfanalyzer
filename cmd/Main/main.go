package main

import (
	"flag"
	"httperfanalyzer/pkg/client"
)

var (
	headers        string
	maxConnections int
	RequestsToSend int
	url            string
	method         string
	requestBody    string
	requestTimeout int
)

func init() {
	flag.StringVar(&headers,
		"headers", "", "headers to include in request [h1=v1,h2=v2...]")
	flag.IntVar(&maxConnections,
		"maxConnections", 15, "Max number of simultaneous connections")
	flag.IntVar(&RequestsToSend,
		"count", 10000, "Number of requests to make")
	flag.StringVar(&url,
		"url", "", "endpoint url to stress")
	flag.StringVar(&method,
		"method", "GET", "request method [GET,POST]")
	flag.StringVar(&requestBody,
		"data", "", "Optionally send a payload body")
	flag.IntVar(&requestTimeout,
		"timeout", 10, "request timeout in seconds")

	flag.Parse()
}

func main() {
	client.Stress(
		url, method, requestBody, headers, RequestsToSend, maxConnections, requestTimeout)
}
