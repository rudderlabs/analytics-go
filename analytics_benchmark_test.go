package analytics

import (
	"encoding/json"
	"strconv"
	"testing"
)

// generateTestMessages creates a slice of test messages with various user/anonymous IDs
func generateTestMessages(n int) []message {
	messages := make([]message, n)

	for i := 0; i < n; i++ {
		userId := "user-" + strconv.Itoa(i%10)     // Create 10 different user IDs
		anonymousId := "anon-" + strconv.Itoa(i%5) // Create 5 different anonymous IDs

		// Create a batch request with these IDs
		req := batchRequest{
			UserID:      userId,
			AnonymousID: anonymousId,
		}

		// Marshal to JSON
		jsonData, _ := json.Marshal(req)

		messages[i] = message{
			json: jsonData,
			msg:  Track{UserId: userId, AnonymousId: anonymousId},
		}
	}

	return messages
}

// BenchmarkGetNodePayloadNew benchmarks the new getNodePayload function with different message counts
// cpu: Apple M2 Pro
// BenchmarkGetNodePayloadNew
// BenchmarkGetNodePayloadNew/10_msgs
// BenchmarkGetNodePayloadNew/10_msgs-12         	  201585	      5434 ns/op	    3936 B/op	      92 allocs/op
// BenchmarkGetNodePayloadNew/100_msgs
// BenchmarkGetNodePayloadNew/100_msgs-12        	   22730	     51660 ns/op	   39121 B/op	     830 allocs/op
// BenchmarkGetNodePayloadNew/1000_msgs
// BenchmarkGetNodePayloadNew/1000_msgs-12       	    2320	    501609 ns/op	  376894 B/op	    8045 allocs/op
// BenchmarkGetNodePayloadNew/10000_msgs
// BenchmarkGetNodePayloadNew/10000_msgs-12      	     225	   5348015 ns/op	 3961967 B/op	   80067 allocs/op
func BenchmarkGetNodePayloadNew(b *testing.B) {
	c := &client{totalNodes: 5} // Using 5 nodes for the test

	messageCounts := []int{10, 100, 1000, 10000}

	for _, count := range messageCounts {
		messages := generateTestMessages(count)

		b.Run(strconv.Itoa(count)+"_msgs", func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				c.getNodePayload(messages)
			}
		})
	}
}

// BenchmarkGetNodePayloadOld benchmarks the old getNodePayload function(using gjson) with different message counts
// cpu: Apple M2 Pro
// BenchmarkGetNodePayloadOld
// BenchmarkGetNodePayloadOld/10_msgs
// BenchmarkGetNodePayloadOld/10_msgs-12         	  496126	      2374 ns/op	    1456 B/op	      42 allocs/op
// BenchmarkGetNodePayloadOld/100_msgs
// BenchmarkGetNodePayloadOld/100_msgs-12        	   55315	     21272 ns/op	   14320 B/op	     330 allocs/op
// BenchmarkGetNodePayloadOld/1000_msgs
// BenchmarkGetNodePayloadOld/1000_msgs-12       	    6051	    197899 ns/op	  128884 B/op	    3045 allocs/op
// BenchmarkGetNodePayloadOld/10000_msgs
// BenchmarkGetNodePayloadOld/10000_msgs-12      	     577	   2127479 ns/op	 1481886 B/op	   30066 allocs/op
func BenchmarkGetNodePayloadOld(b *testing.B) {
	c := &client{totalNodes: 5} // Using 5 nodes for the test

	messageCounts := []int{10, 100, 1000, 10000}

	for _, count := range messageCounts {
		messages := generateTestMessages(count)

		b.Run(strconv.Itoa(count)+"_msgs", func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				c.getNodePayloadOld(messages)
			}
		})
	}
}
