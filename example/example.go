package main

import (
	"fmt"
	"os"

	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/rudderlabs/analytics-go/v4"
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
	// LOCAL_DATA_PLANE_URL := goDotEnvVariable("LOCAL_DATA_PLANE_URL")

	client, _ := analytics.NewWithConfig(WRITE_KEY,
		analytics.Config{
			DataPlaneUrl: DATA_PLANE_URL,
			Interval:     30 * time.Second,
			BatchSize:    100,
			Verbose:      true,
		})
	defer client.Close()

	done := time.After(2 * time.Second)
	tick := time.Tick(1 * time.Second)

	properties := map[string]interface{}{
		"application": "Rudder Desktop",
		"version":     "1.1.0",
		"platform":    "osx",
	}

	traits := analytics.NewTraits().
		SetFirstName("First").
		SetLastName("Last").
		Set("Role", "Jedi").SetAge(25)

	userId := "123456"
	anonymousId := uuid.NewString()

	context := analytics.Context{
		Screen: analytics.ScreenInfo{
			Density: 3,
			Height:  393,
			Width:   852,
		},
		OS: analytics.OSInfo{
			Name:    "macOS",
			Version: "2.0.0",
		},
		Locale: "en-US",
		Library: analytics.LibraryInfo{
			Name:    "analytics-random-sdk",
			Version: "1.0.0.beta.1",
		},
	}

	track := analytics.Track{
		Event:       "Test Track",
		UserId:      userId,
		AnonymousId: anonymousId,
		Properties:  properties,
		Context:     &context,
	}

	screen := analytics.Screen{
		Name:        "Test Screen",
		UserId:      userId,
		AnonymousId: anonymousId,
		Properties:  properties,
		Context:     &context,
	}

	identify := analytics.Identify{
		UserId:  "654321",
		Traits:  traits,
		Context: &context,
	}

	group := analytics.Group{
		GroupId:     uuid.NewString(),
		UserId:      userId,
		AnonymousId: anonymousId,
		Traits:      traits,
		Context:     &context,
	}

	alias := analytics.Alias{
		PreviousId: "654321",
		UserId:     userId,
		Context:    &context,
	}

	page := analytics.Page{
		Name:        "Test Page",
		UserId:      userId,
		AnonymousId: anonymousId,
		Properties:  properties,
		Context:     &context,
	}

	for {
		select {
		case <-done:
			fmt.Println("exiting")
			return

		case <-tick:
			if err := client.Enqueue(track); err != nil {
				fmt.Println("error:", err)
				return
			}
			if err := client.Enqueue(screen); err != nil {
				fmt.Println("error:", err)
				return
			}
			if err := client.Enqueue(identify); err != nil {
				fmt.Println("error:", err)
				return
			}
			if err := client.Enqueue(group); err != nil {
				fmt.Println("error:", err)
				return
			}
			if err := client.Enqueue(alias); err != nil {
				fmt.Println("error:", err)
				return
			}
			if err := client.Enqueue(page); err != nil {
				fmt.Println("error:", err)
				return
			}
		}
	}
}
