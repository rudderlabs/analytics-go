package main

import (
	"fmt"
	"time"

	"github.com/rudderlabs/analytics-go"
)

func main() {
	ex1()
	ex2()
	ex3()
}

func ex1() {
	// Instantiates a client to use send messages to the Rudder API.
	// User your WRITE KEY in below placeholder "RUDDER WRITE KEY"
	client := analytics.New("1weqc5iqxRkpaUXHgDgYo3g34mg", "https://hosted.rudderlabs.com")

	if client != nil {
		// Enqueues a track event that will be sent asynchronously.
		client.Enqueue(analytics.Track{
			UserId: "test-user",
			Event:  "test-snippet",
			Properties: analytics.NewProperties().
				Set("text", "Lorem Ipsum is simply dummy text of the printing and typesetting industry."),
		})

		// Flushes any queued messages and closes the client.
		client.Close()
	}
}

func ex2() {
	client, _ := analytics.NewWithConfig("1aUR9IELHp6jqOW8HWkrYvMYHWy",
		"https://218da72a.ngrok.io",
		analytics.Config{
			Interval:  30 * time.Second,
			BatchSize: 100,
			Verbose:   true,
		})
	defer client.Close()

	done := time.After(2 * time.Second)
	tick := time.Tick(1 * time.Second)

	for {
		select {
		case <-done:
			fmt.Println("exiting")
			return

		case <-tick:
			if err := client.Enqueue(analytics.Track{
				Event:  "Download",
				UserId: "123456",
				Properties: map[string]interface{}{
					"application": "Rudder Desktop",
					"version":     "1.1.0",
					"platform":    "osx",
				},
			}); err != nil {
				fmt.Println("error:", err)
				return
			}
		}
	}
}

func ex3() {
	// Instantiates a client to use send messages to the Rudder API.
	// User your WRITE KEY in below placeholder "RUDDER WRITE KEY"
	client, _ := analytics.NewWithConfig("WRITE-KEY", "DATA-PLANE-URL",
		analytics.Config{
			MaxMessageBytes: 35000, //new field to control the max message size
		})

	if client != nil {
		// Enqueues a track event that will be sent asynchronously.
		client.Enqueue(analytics.Track{
			UserId: "test-user",
			Event:  "test-snippet",
			Properties: analytics.NewProperties().
				Set("text", "Lorem Ipsum is simply dummy text of the printing and typesetting industry.  specimen book. It has survived not only five centuries, but also the leap into electronic typesetting, remaining essentially unchanged. It was popularised in the 1960s with the release of Letraset sheets containing Lorem Ipsum passages, and more recently with cently ng and typesetting industry. Lorem Ipsum has been the industry's standard dummy text ever since the 1500s, when an unknown printer took a galley of type and scrambled it to make a type specimen book. It has survived not only five centuries, but also the leap into electronic typesetting, remaining essentially unchanged. It was popularised in the 1960s"),
		})

		// Flushes any queued messages and closes the client.
		client.Close()
	}
}
