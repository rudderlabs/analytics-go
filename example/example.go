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

/*
{
	"messageId": "dc6e25da-e0aa-481b-9d16-cbf7d9f944ee",
	"sentAt": "2022-11-24T11:04:48.673214+05:30",
	"batch": [{
		"type": "track",
		"messageId": "db5459b7-3851-45e9-b09d-d18daf3a7b4f",
		"anonymousId": "123456",
		"userId": "123456",
		"event": "Download",
		"timestamp": "2022-11-24T11:03:43.394658+05:30",
		"context": {
			"library": {
				"name": "analytics-go",
				"version": "3.3.3"
			}
		},
		"properties": {
			"application": "Rudder Desktop",
			"platform": "osx",
			"version": "1.1.0"
		},
		"channel": "server"
	}],
	"context": {
		"library": {
			"name": "analytics-go",
			"version": "3.3.3"
		}
	}
}
*/

/*
{
	"sentAt": "2022-11-24T09:17:29.314Z",
	"batch": [{
		"messageId": "1669281437-16527cc8-d561-4b62-8fa6-9d1ba280008b",
		"anonymousId": "anonymous_id",
		"userId": "new_user_id",
		"channel": "mobile",
		"event": "simple_track_with_props",
		"context": {
			"screen": {
				"density": 3,
				"width": 844,
				"height": 390
			},
			"os": {
				"name": "iOS",
				"version": "16.0"
			},
			"locale": "en-US",
			"app": {
				"version": "1.0",
				"namespace": "com.rudderstack.ios.test.objc",
				"name": "RudderSampleAppObjC",
				"build": "1"
			},
			"device": {
				"manufacturer": "Apple",
				"id": "b309004c-b562-4b3b-876e-05083b1002a4",
				"model": "iPhone",
				"type": "iOS",
				"token": "your_device_token",
				"attTrackingStatus": 0,
				"name": "iPhone 13"
			},
			"traits": {
				"firstname": "First Name",
				"userId": "new_user_id",
				"id": "new_user_id"
			},
			"library": {
				"name": "rudder-ios-library",
				"version": "1.7.2"
			},
			"timezone": "Asia\/Kolkata",
			"sessionId": 1669281353,
			"network": {
				"carrier": "unavailable",
				"cellular": false,
				"wifi": true
			},
			"externalId": [{
				"type": "brazeExternalId",
				"id": "some_external_id_1"
			}]
		},
		"originalTimestamp": "2022-11-24T09:17:17.222Z",
		"properties": {
			"key_1": "value_1",
			"key_2": "value_2"
		},
		"type": "track",
		"integrations": {
			"All": true
		},
		"sentAt": "2022-11-24T09:17:29.314Z"
	}, {
		"messageId": "1669281437-2b75fab9-897d-4570-8d04-28e3367d64b1",
		"anonymousId": "anonymous_id",
		"userId": "testUserId",
		"channel": "mobile",
		"event": "identify",
		"context": {
			"screen": {
				"density": 3,
				"width": 844,
				"height": 390
			},
			"os": {
				"name": "iOS",
				"version": "16.0"
			},
			"locale": "en-US",
			"app": {
				"version": "1.0",
				"namespace": "com.rudderstack.ios.test.objc",
				"name": "RudderSampleAppObjC",
				"build": "1"
			},
			"device": {
				"attTrackingStatus": 0,
				"manufacturer": "Apple",
				"id": "b309004c-b562-4b3b-876e-05083b1002a4",
				"model": "iPhone",
				"adTrackingEnabled": true,
				"type": "iOS",
				"token": "your_device_token",
				"advertisingId": "advertisement_Id",
				"name": "iPhone 13"
			},
			"traits": {
				"firstname": "First Name",
				"userId": "testUserId"
			},
			"library": {
				"name": "rudder-ios-library",
				"version": "1.7.2"
			},
			"timezone": "Asia\/Kolkata",
			"sessionId": 1669281353,
			"network": {
				"carrier": "unavailable",
				"cellular": false,
				"wifi": true
			},
			"externalId": [{
				"type": "brazeExternalId",
				"id": "some_external_id_1"
			}]
		},
		"originalTimestamp": "2022-11-24T09:17:17.230Z",
		"type": "identify",
		"integrations": {
			"All": true
		},
		"sentAt": "2022-11-24T09:17:29.314Z"
	}, {
		"messageId": "1669281437-654df845-3382-48ba-9bfa-e20751902db2",
		"anonymousId": "anonymous_id",
		"userId": "testUserId",
		"channel": "mobile",
		"event": "ViewController",
		"context": {
			"screen": {
				"density": 3,
				"width": 844,
				"height": 390
			},
			"os": {
				"name": "iOS",
				"version": "16.0"
			},
			"locale": "en-US",
			"app": {
				"version": "1.0",
				"namespace": "com.rudderstack.ios.test.objc",
				"name": "RudderSampleAppObjC",
				"build": "1"
			},
			"device": {
				"attTrackingStatus": 0,
				"manufacturer": "Apple",
				"id": "b309004c-b562-4b3b-876e-05083b1002a4",
				"model": "iPhone",
				"adTrackingEnabled": true,
				"type": "iOS",
				"token": "your_device_token",
				"advertisingId": "advertisement_Id",
				"name": "iPhone 13"
			},
			"traits": {
				"firstname": "First Name",
				"userId": "testUserId"
			},
			"library": {
				"name": "rudder-ios-library",
				"version": "1.7.2"
			},
			"timezone": "Asia\/Kolkata",
			"sessionId": 1669281353,
			"network": {
				"carrier": "unavailable",
				"cellular": false,
				"wifi": true
			},
			"externalId": [{
				"type": "brazeExternalId",
				"id": "some_external_id_1"
			}]
		},
		"originalTimestamp": "2022-11-24T09:17:17.251Z",
		"properties": {
			"name": "ViewController"
		},
		"type": "screen",
		"integrations": {
			"All": true
		},
		"sentAt": "2022-11-24T09:17:29.314Z"
	}, {
		"messageId": "1669281437-b6f9102b-2f97-43a1-8229-13cff277c065",
		"traits": {
			"foo": "bar",
			"email": "ruchira@gmail.com",
			"foo1": "bar1"
		},
		"channel": "mobile",
		"anonymousId": "anonymous_id",
		"userId": "testUserId",
		"context": {
			"screen": {
				"density": 3,
				"width": 844,
				"height": 390
			},
			"os": {
				"name": "iOS",
				"version": "16.0"
			},
			"locale": "en-US",
			"app": {
				"version": "1.0",
				"namespace": "com.rudderstack.ios.test.objc",
				"name": "RudderSampleAppObjC",
				"build": "1"
			},
			"device": {
				"attTrackingStatus": 0,
				"manufacturer": "Apple",
				"id": "b309004c-b562-4b3b-876e-05083b1002a4",
				"model": "iPhone",
				"adTrackingEnabled": true,
				"type": "iOS",
				"token": "your_device_token",
				"advertisingId": "advertisement_Id",
				"name": "iPhone 13"
			},
			"traits": {
				"firstname": "First Name",
				"userId": "testUserId"
			},
			"library": {
				"name": "rudder-ios-library",
				"version": "1.7.2"
			},
			"timezone": "Asia\/Kolkata",
			"sessionId": 1669281353,
			"network": {
				"carrier": "unavailable",
				"cellular": false,
				"wifi": true
			},
			"externalId": [{
				"type": "brazeExternalId",
				"id": "some_external_id_1"
			}]
		},
		"originalTimestamp": "2022-11-24T09:17:17.262Z",
		"type": "group",
		"groupId": "sample_group_id",
		"integrations": {
			"All": true
		},
		"sentAt": "2022-11-24T09:17:29.314Z"
	}, {
		"messageId": "1669281437-f9d0d873-371d-4811-84fb-fc1a714f5657",
		"anonymousId": "anonymous_id",
		"channel": "mobile",
		"userId": "new_user_id",
		"previousId": "testUserId",
		"context": {
			"screen": {
				"density": 3,
				"width": 844,
				"height": 390
			},
			"os": {
				"name": "iOS",
				"version": "16.0"
			},
			"locale": "en-US",
			"app": {
				"version": "1.0",
				"namespace": "com.rudderstack.ios.test.objc",
				"name": "RudderSampleAppObjC",
				"build": "1"
			},
			"device": {
				"attTrackingStatus": 0,
				"manufacturer": "Apple",
				"id": "b309004c-b562-4b3b-876e-05083b1002a4",
				"model": "iPhone",
				"adTrackingEnabled": true,
				"type": "iOS",
				"token": "your_device_token",
				"advertisingId": "advertisement_Id",
				"name": "iPhone 13"
			},
			"traits": {
				"firstname": "First Name",
				"userId": "new_user_id",
				"id": "new_user_id"
			},
			"library": {
				"name": "rudder-ios-library",
				"version": "1.7.2"
			},
			"timezone": "Asia\/Kolkata",
			"sessionId": 1669281353,
			"network": {
				"carrier": "unavailable",
				"cellular": false,
				"wifi": true
			},
			"externalId": [{
				"type": "brazeExternalId",
				"id": "some_external_id_1"
			}]
		},
		"originalTimestamp": "2022-11-24T09:17:17.276Z",
		"type": "alias",
		"integrations": {
			"All": true
		},
		"sentAt": "2022-11-24T09:17:29.314Z"
	}, {
		"messageId": "1669281437-02e61777-ab01-4dc9-b382-194d0812b700",
		"anonymousId": "anonymous_id",
		"userId": "new_user_id",
		"channel": "mobile",
		"event": "Application Opened",
		"context": {
			"screen": {
				"density": 3,
				"width": 844,
				"height": 390
			},
			"os": {
				"name": "iOS",
				"version": "16.0"
			},
			"locale": "en-US",
			"app": {
				"version": "1.0",
				"namespace": "com.rudderstack.ios.test.objc",
				"name": "RudderSampleAppObjC",
				"build": "1"
			},
			"device": {
				"attTrackingStatus": 0,
				"manufacturer": "Apple",
				"id": "b309004c-b562-4b3b-876e-05083b1002a4",
				"model": "iPhone",
				"adTrackingEnabled": true,
				"type": "iOS",
				"token": "your_device_token",
				"advertisingId": "advertisement_Id",
				"name": "iPhone 13"
			},
			"traits": {
				"firstname": "First Name",
				"userId": "new_user_id",
				"id": "new_user_id"
			},
			"library": {
				"name": "rudder-ios-library",
				"version": "1.7.2"
			},
			"timezone": "Asia\/Kolkata",
			"sessionId": 1669281353,
			"network": {
				"carrier": "unavailable",
				"cellular": false,
				"wifi": true
			},
			"externalId": [{
				"type": "brazeExternalId",
				"id": "some_external_id_1"
			}]
		},
		"originalTimestamp": "2022-11-24T09:17:17.292Z",
		"properties": {
			"version": "1.0",
			"from_background": false
		},
		"type": "track",
		"integrations": {
			"All": true
		},
		"sentAt": "2022-11-24T09:17:29.314Z"
	}, {
		"messageId": "1669281437-2b2efbf5-eacd-462f-82d2-99c905bf62d2",
		"anonymousId": "anonymous_id",
		"userId": "new_user_id",
		"channel": "mobile",
		"event": "_",
		"context": {
			"screen": {
				"density": 3,
				"width": 844,
				"height": 390
			},
			"os": {
				"name": "iOS",
				"version": "16.0"
			},
			"locale": "en-US",
			"app": {
				"version": "1.0",
				"namespace": "com.rudderstack.ios.test.objc",
				"name": "RudderSampleAppObjC",
				"build": "1"
			},
			"device": {
				"attTrackingStatus": 0,
				"manufacturer": "Apple",
				"id": "b309004c-b562-4b3b-876e-05083b1002a4",
				"model": "iPhone",
				"adTrackingEnabled": true,
				"type": "iOS",
				"token": "your_device_token",
				"advertisingId": "advertisement_Id",
				"name": "iPhone 13"
			},
			"traits": {
				"firstname": "First Name",
				"userId": "new_user_id",
				"id": "new_user_id"
			},
			"library": {
				"name": "rudder-ios-library",
				"version": "1.7.2"
			},
			"timezone": "Asia\/Kolkata",
			"sessionId": 1669281353,
			"network": {
				"carrier": "unavailable",
				"cellular": false,
				"wifi": true
			},
			"externalId": [{
				"type": "brazeExternalId",
				"id": "some_external_id_1"
			}]
		},
		"originalTimestamp": "2022-11-24T09:17:17.314Z",
		"properties": {
			"name": "_",
			"automatic": true
		},
		"type": "screen",
		"integrations": {
			"All": true
		},
		"sentAt": "2022-11-24T09:17:29.314Z"
	}]
}
*/
