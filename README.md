<p align="center">
  <a href="https://rudderstack.com/">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://cdn.rudderlabs.com/brand/logo_watermark_dark.png">
      <img alt="RudderStack" width="512" src="https://cdn.rudderlabs.com/brand/logo_watermark_light.png">
    </picture>
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

<p align="center"><a href="https://deepwiki.com/rudderlabs/analytics-go"><img src="https://deepwiki.com/badge.svg" alt="Ask DeepWiki"/></a></p>

----

# RudderStack Go SDK

The RudderStack Go SDK lets you send customer event data from your Go applications to your specified destinations.

## Releases and validation

Release Please creates the GitHub release and its `v4.x.y` tag after a release PR is merged. For this Go module (`github.com/rudderlabs/analytics-go/v4`), tagging publishes the module; there is no separate npm-style upload. The Go module proxy may take time to serve that exact version after tagging.

The `Server SDK E2E` workflow validates source candidates on trusted PRs and master pushes. PRs run the reliability suite. `Published Go SDK E2E` runs the full suite after a release and installs the exact version from the public Go module proxy. Because Go publication starts with tagging, this is post-publication acceptance, not a pre-tag gate. See [release acceptance and validation-only retry](.github/RELEASE_E2E.md).

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
			DisableGzip:  false  // Enables Gzip compression - set true to disable Gzip.
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

## Flushing without closing

In serverless or short-lived processes, call `Flush` to send queued messages and keep the client open. It blocks until messages enqueued before the call are sent (or fail after retries), or until the context is done:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if f, ok := client.(interface{ Flush(context.Context) error }); ok {
    if err := f.Flush(ctx); err != nil {
        // ctx.Err() on timeout, analytics.ErrClosed after Close.
    }
}
```

## Gzip support

The Go SDK supports Gzip compression from version 4.1.0 and it is enabled by default. However, you can disable this feature by setting the `DisableGzip` parameter to `true` while initializing the SDK, as shown:

```go
client, _ := analytics.NewWithConfig(WRITE_KEY,
		analytics.Config{
			DataPlaneUrl: DATA_PLANE_URL,
			Interval:     30 * time.Second,
			BatchSize:    100,
			Verbose:      true,
			DisableGzip:  false  // Enables Gzip compression - set true to disable Gzip.
		})
```



| Note: Gzip requires `rudder-server` version 1.4 or later. |
| :-----|

## Sending events

Refer to the [RudderStack Go SDK documentation](https://www.rudderstack.com/docs/sources/event-streams/sdks/rudderstack-go-sdk/) for more information on the supported event types.

## License

The RudderStack Go SDK is released under the [MIT license](License.md).
