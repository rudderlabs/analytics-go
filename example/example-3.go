package main

import (
	"github.com/rudderlabs/analytics-go"
)

func main() {
	// Instantiates a client to use send messages to the Rudder API.
	// User your WRITE KEY in below placeholder "RUDDER WRITE KEY"
	client, _ := analytics.NewWithConfig("WRITE-KEY", "DATA-PLANE-URL",
		analytics.Config{
			MaxMessageBytes: 35000, //new field to control the max message size
	})

	if client!= nil{
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
