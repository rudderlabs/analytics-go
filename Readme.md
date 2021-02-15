# What is RudderStack?

[RudderStack](https://rudderstack.com/) is a **customer data pipeline** tool for collecting, routing and processing data from your websites, apps, cloud tools, and data warehouse.

More information on RudderStack can be found [here](https://github.com/rudderlabs/rudder-server).

## Installation

The package can be simply installed via `go get`. We recommend that you use a tool like Godep to avoid issues related to API breaking changes introduced between major versions of the library.

To install it in the GOPATH:
```
go get https://github.com/rudderlabs/analytics-go
```


## Usage

```go
package main

import (
    "github.com/rudderlabs/analytics-go"
)

func main() {
    // Instantiates a client to use send messages to the Rudder API.
    // User your WRITE KEY in below placeholder "RUDDER WRITE KEY"
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
OR

```go
package main

import (
    "github.com/rudderlabs/analytics-go"
)

func main() {
    // Instantiates a client to use send messages to the Rudder API.
    // User your WRITE KEY in below placeholder "RUDDER WRITE KEY"
    client, _ := analytics.NewWithConfig(<WRITE_KEY>, <DATA_PLANE_URL>,
		analytics.Config{
			Interval:  30 * time.Second,
			BatchSize: 100,
			Verbose:   true,
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


## License

The library is released under the [MIT license](https://github.com/segmentio/analytics-go/blob/master/LICENSE).

## Contact Us

For any queries, please feel free to [contact us](https://rudderstack.com/contact/) or start a conversation on our [Slack](https://resources.rudderstack.com/join-rudderstack-slack) channel.
