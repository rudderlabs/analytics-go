package main

import (
	"fmt"
	"os"

	"time"

	"github.com/joho/godotenv"
	"github.com/rudderlabs/analytics-go/v3"
)

func goDotEnvVariable(key string) string {
	// load .env file
	err := godotenv.Load(".env")

	if err != nil {
		fmt.Println("Error loading .env file")
	}

	return os.Getenv(key)
}

func main() {
	// create a .env file inside example directory and add the following variables.
	WRITE_KEY := goDotEnvVariable("WRITE_KEY")
	DATA_PLANE_URL := goDotEnvVariable("DATA_PLANE_URL")

	client, _ := analytics.NewWithConfig(WRITE_KEY, DATA_PLANE_URL,
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
