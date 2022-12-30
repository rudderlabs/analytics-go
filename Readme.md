<p align="center">
  <a href="https://rudderstack.com/">
    <img src="https://user-images.githubusercontent.com/59817155/121357083-1c571300-c94f-11eb-8cc7-ce6df13855c9.png">
  </a>
</p>

<p align="center"><b>The Customer Data Platform for Developers</b></p>

<p align="center">
  <b>
    <a href="https://rudderstack.com">Website</a>
    ·
    <a href="https://www.rudderstack.com/docs/sources/event-streams/sdks/rudderstack-go-sdk/">Documentation</a>
    ·
    <a href="https://rudderstack.com/join-rudderstack-slack-community">Community Slack</a>
  </b>
</p>

<p align="center"><a href="https://github.com/rudderlabs/analytics-go"><img src="https://img.shields.io/github/v/release/rudderlabs/analytics-go.svg?label=Version"/></a></p>

----

# RudderStack Go SDK

The RudderStack Go SDK lets you send customer event data from your Go applications to your specified destinations.

## SDK setup requirements

- Set up a [RudderStack open source](https://app.rudderstack.com/signup?type=opensource) account.
- Set up a Go source in the dashboard.
- Copy the write key and the data plane URL. For more information, refer to the [Go SDK documentation](https://www.rudderstack.com/docs/sources/event-streams/sdks/rudderstack-go-sdk/#sdk-setup-requirements).

## Installation

You can install the Go SDK via the `go get` command.

| It is highly recommended to use a tool like Godep to avoid any issues related to the breaking API changes introduced between the major versions of the library. |
| :-----|

To install the SDK in the `GOPATH`, run the following:

```go
go get github.com/rudderlabs/analytics-go
```

## Using the SDK

```go
package main

import (
    "github.com/rudderlabs/analytics-go/v4"
)

func main() {
    // Instantiates a client to use send messages to the RudderStack API.
    
    // Use your write key in the below placeholder:
    
    client := analytics.New(<WRITE_KEY>, <DATA_PLANE_URL>)

    // Enqueues a track event that will be sent asynchronously.
    client.Enqueue(analytics.Track{
        UserId: "test-user",
        Event:  "test-snippet",
    })

    // Flushes any queued messages and closes the client.
    client.Close()
}
```

Alternatively, you can run the following snippet:

```go
package main

import (
    "github.com/rudderlabs/analytics-go/v4"
)

func main() {
    // Instantiates a client to use send messages to the RudderStack API.
    
    // User your write key in the below placeholder:
    
    client, _ := analytics.NewWithConfig(WRITE_KEY,
		analytics.Config{
			DataPlaneUrl: DATA_PLANE_URL,
			Interval:     30 * time.Second,
			BatchSize:    100,
			Verbose:      true,
			Gzip:         0,  // Enables Gzip compression - set to 1 to disable Gzip.
		})

    // Enqueues a track event that will be sent asynchronously.
    
    client.Enqueue(analytics.Track{
        UserId: "test-user",
        Event:  "test-snippet",
    })

    // Flushes any queued messages and closes the client.
    
    client.Close()
}
```

## Gzip support

The Go SDK supports Gzip compression from version 4.0.0 and it is enabled (set to `0`) by default. However, you can disable this feature by setting the `Gzip` parameter to `1` while initializing the SDK, as shown:

```go
client, _ := analytics.NewWithConfig(WRITE_KEY,
		analytics.Config{
			DataPlaneUrl: DATA_PLANE_URL,
			Interval:     30 * time.Second,
			BatchSize:    100,
			Verbose:      true,
			Gzip:         0  // Enables Gzip compression - set to 1 to disable Gzip.
		})
```



| Note: Gzip requires `rudder-server` version 1.4 or later. |
| :-----|

## Sending events

Refer to the [RudderStack Go SDK documentation](https://www.rudderstack.com/docs/sources/event-streams/sdks/rudderstack-go-sdk/) for more information on the supported event types.

## License

The RudderStack Go SDK is released under the [MIT license](License.md).
