package analytics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type flusher interface {
	Flush(context.Context) error
}

func newFlushClient(t *testing.T, config Config) (Client, flusher) {
	t.Helper()
	if config.Interval == 0 {
		// Keep the ticker out of the way so only Flush can send a partial batch.
		config.Interval = time.Hour
	}
	if config.Transport == nil {
		config.Transport = testTransportOK
	}
	if config.Logger == nil {
		config.Logger = testLogger{t.Logf, t.Logf}
	}
	c, err := NewWithConfig(WRITE_KEY, config)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := c.(flusher)
	if !ok {
		t.Fatalf("%T does not implement Flush(context.Context) error", c)
	}
	return c, f
}

func flushCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestFlushSendsPartialBatch(t *testing.T) {
	var sent atomic.Int64
	client, f := newFlushClient(t, Config{
		BatchSize: 100,
		Callback:  testCallback{func(Message) { sent.Add(1) }, nil},
	})
	defer client.Close()

	for i := 0; i < 3; i++ {
		if err := client.Enqueue(Track{UserId: "A", Event: "B"}); err != nil {
			t.Fatal(err)
		}
	}

	if err := f.Flush(flushCtx(t)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n := sent.Load(); n != 3 {
		t.Fatalf("sent %d messages before Flush returned, want 3", n)
	}
}

func TestFlushSendsMessagesBufferedInChannel(t *testing.T) {
	// The loop goroutine logs every pushed message when Verbose is set, so a
	// blocking logger parks it while later messages pile up in the channel.
	for i := 0; i < 20; i++ {
		var sent atomic.Int64
		var once sync.Once
		parked := make(chan struct{})
		release := make(chan struct{})
		client, f := newFlushClient(t, Config{
			BatchSize: 100,
			Verbose:   true,
			Callback:  testCallback{func(Message) { sent.Add(1) }, nil},
			Logger: testLogger{
				logf: func(string, ...interface{}) {
					once.Do(func() {
						close(parked)
						<-release
					})
				},
			},
		})

		const n = 50
		for j := 0; j < n; j++ {
			if err := client.Enqueue(Track{UserId: "A", Event: "B"}); err != nil {
				t.Fatal(err)
			}
			if j == 0 {
				<-parked
			}
		}

		errc := make(chan error, 1)
		go func() { errc <- f.Flush(flushCtx(t)) }()
		close(release)

		if err := <-errc; err != nil {
			t.Fatalf("Flush: %v", err)
		}
		if got := sent.Load(); got != n {
			t.Fatalf("sent %d messages before Flush returned, want %d", got, n)
		}
		client.Close()
	}
}

func TestFlushWaitsForFinalFailure(t *testing.T) {
	var failed atomic.Int64
	client, f := newFlushClient(t, Config{
		Transport:  testTransportError,
		RetryAfter: func(int) time.Duration { return time.Millisecond },
		Callback:   testCallback{nil, func(Message, error) { failed.Add(1) }},
	})
	defer client.Close()

	if err := client.Enqueue(Track{UserId: "A", Event: "B"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Flush(flushCtx(t)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if n := failed.Load(); n != 1 {
		t.Fatalf("got %d failure callbacks before Flush returned, want 1", n)
	}
}

func TestFlushReturnsContextError(t *testing.T) {
	release := make(chan struct{})
	client, f := newFlushClient(t, Config{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			<-release
			return testTransportOK.RoundTrip(r)
		}),
	})
	defer client.Close()
	defer close(release)

	if err := client.Enqueue(Track{UserId: "A", Event: "B"}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := f.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Flush: got %v, want %v", err, context.DeadlineExceeded)
	}

	canceled, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if err := f.Flush(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Flush: got %v, want %v", err, context.Canceled)
	}
}

func TestFlushConcurrentWithEnqueue(t *testing.T) {
	var mu sync.Mutex
	sent := map[string]int{}
	client, f := newFlushClient(t, Config{
		// Small batches so full batches are also sent from push, not only by Flush.
		BatchSize: 7,
		Callback: testCallback{func(m Message) {
			mu.Lock()
			sent[m.(Track).Event]++
			mu.Unlock()
		}, nil},
	})
	defer client.Close()

	const workers, perWorker, rounds = 8, 25, 5
	var wg sync.WaitGroup
	errc := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				event := fmt.Sprintf("w%d-r%d", w, r)
				for i := 0; i < perWorker; i++ {
					if err := client.Enqueue(Track{UserId: "A", Event: event}); err != nil {
						errc <- err
						return
					}
				}
				if err := f.Flush(flushCtx(t)); err != nil {
					errc <- err
					return
				}
				mu.Lock()
				got := sent[event]
				mu.Unlock()
				if got != perWorker {
					errc <- fmt.Errorf("%s: sent %d before Flush returned, want %d", event, got, perWorker)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Error(err)
	}
}

func TestFlushDoesNotCloseClient(t *testing.T) {
	var sent atomic.Int64
	client, f := newFlushClient(t, Config{
		Callback: testCallback{func(Message) { sent.Add(1) }, nil},
	})

	if err := f.Flush(flushCtx(t)); err != nil {
		t.Fatalf("Flush on empty client: %v", err)
	}
	if err := client.Enqueue(Track{UserId: "A", Event: "B"}); err != nil {
		t.Fatalf("Enqueue after Flush: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close after Flush: %v", err)
	}
	if n := sent.Load(); n != 1 {
		t.Fatalf("sent %d messages, want 1", n)
	}
}

func TestFlushAfterClose(t *testing.T) {
	client, f := newFlushClient(t, Config{})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Flush(flushCtx(t)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Flush after Close: got %v, want %v", err, ErrClosed)
	}
}
