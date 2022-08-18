package main

import (
	"fmt"

	"time"

	"github.com/rudderlabs/analytics-go"
)

func main() {
	client, _ := analytics.NewWithConfig("1wvsoF3Kx2SczQNlx1dvcqW9ODW",
		"https://rudderstacz.dataplane.rudderstack.com",
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
