package main

import (
	"github.com/rudderlabs/analytics-go"
)

func main() {
	// Instantiates a client to use send messages to the Rudder API.
	// User your WRITE KEY in below placeholder "RUDDER WRITE KEY"
	client := analytics.New("1weqc5iqxRkpaUXHgDgYo3g34mg", "https://hosted.rudderlabs.com")

	if client!= nil{
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
