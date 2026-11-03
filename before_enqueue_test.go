package analytics

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Records log lines so tests can assert on levels without printing payloads.
type beforeEnqueueLog struct {
	mu     sync.Mutex
	infos  []string
	errors []string
}

func (l *beforeEnqueueLog) Logf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, fmt.Sprintf(format, args...))
}

func (l *beforeEnqueueLog) Errorf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, fmt.Sprintf(format, args...))
}

func (l *beforeEnqueueLog) has(lines []string, substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range lines {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// Starts a client that records every uploaded batch; cfg overrides the hook
// options and callback. Close the client before reading the batches.
func newBeforeEnqueueClient(t *testing.T, cfg Config) (Client, func() []map[string]interface{}, *beforeEnqueueLog) {
	t.Helper()
	var mu sync.Mutex
	var batches [][]byte
	cfg.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		batches = append(batches, b)
		mu.Unlock()
		return testTransportOK.RoundTrip(r)
	})
	log := &beforeEnqueueLog{}
	cfg.Logger = log
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 1
	}
	cfg.DisableGzip = true
	cfg.NoProxySupport = true
	cfg.now = mockTime
	cfg.uid = mockId
	c, err := NewWithConfig(WRITE_KEY, cfg)
	if err != nil {
		t.Fatal("creating client:", err)
	}
	sent := func() []map[string]interface{} {
		mu.Lock()
		defer mu.Unlock()
		var out []map[string]interface{}
		for _, b := range batches {
			var v struct {
				Batch []map[string]interface{} `json:"batch"`
			}
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatal("decoding batch:", err)
			}
			out = append(out, v.Batch...)
		}
		return out
	}
	return c, sent, log
}

func failOnCallback(t *testing.T) Callback {
	return testCallback{
		func(m Message) {},
		func(m Message, e error) { t.Errorf("failure callback fired: %v", e) },
	}
}

func TestBeforeEnqueueEdit(t *testing.T) {
	c, sent, _ := newBeforeEnqueueClient(t, Config{
		BeforeEnqueue: func(m Message) Message {
			tr := m.(Track)
			tr.Properties["plan"] = "redacted"
			tr.Context.Extra = map[string]interface{}{"region": "eu"}
			return tr
		},
	})
	if err := c.Enqueue(Track{UserId: "u", Event: "E", Properties: Properties{"plan": "pro"}}); err != nil {
		t.Fatal(err)
	}
	c.Close()

	got := sent()
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if p := got[0]["properties"].(map[string]interface{})["plan"]; p != "redacted" {
		t.Errorf("edit not sent: %v", p)
	}
	if r := got[0]["context"].(map[string]interface{})["region"]; r != "eu" {
		t.Errorf("context edit not sent: %v", r)
	}
}

func TestBeforeEnqueueInputIsEnriched(t *testing.T) {
	var seen Track
	c, _, _ := newBeforeEnqueueClient(t, Config{
		BeforeEnqueue: func(m Message) Message { seen = m.(Track); return m },
	})
	c.Enqueue(&Track{UserId: "u", Event: "E"})
	c.Close()

	if seen.Type != "track" || seen.MessageId != mockId() || seen.Channel != "server" ||
		!seen.OriginalTimestamp.Equal(mockTime()) || seen.AnonymousId == "" ||
		seen.Context == nil || seen.Context.Library.Name != "analytics-go" {
		t.Errorf("hook input not enriched: %+v", seen)
	}
	if !seen.SentAt.Equal(seen.OriginalTimestamp) {
		t.Errorf("provisional SentAt = %v, want OriginalTimestamp", seen.SentAt)
	}
}

func TestBeforeEnqueueSentAtOverwrittenAtSend(t *testing.T) {
	sendTime := mockTime().Add(time.Hour)
	c, sent, _ := newBeforeEnqueueClient(t, Config{
		BeforeEnqueue: func(m Message) Message {
			tr := m.(Track)
			tr.SentAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			return tr
		},
	})
	c.(*client).now = func() time.Time { return sendTime }
	c.Enqueue(Track{UserId: "u", Event: "E", OriginalTimestamp: mockTime()})
	c.Close()

	if s := sent()[0]["sentAt"]; s != sendTime.Format(time.RFC3339) {
		t.Errorf("sentAt = %v, want send time", s)
	}
}

func TestBeforeEnqueueDrop(t *testing.T) {
	for name, ret := range map[string]Message{
		"nil":         nil,
		"typed nil *": (*Track)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			var callbacks int32
			c, sent, log := newBeforeEnqueueClient(t, Config{
				Verbose:       true,
				BeforeEnqueue: func(Message) Message { return ret },
				Callback: testCallback{
					func(Message) { atomic.AddInt32(&callbacks, 1) },
					func(Message, error) { atomic.AddInt32(&callbacks, 1) },
				},
			})
			if err := c.Enqueue(Track{UserId: "u", Event: "E"}); err != nil {
				t.Errorf("drop returned error: %v", err)
			}
			c.Close()

			if n := len(sent()); n != 0 {
				t.Errorf("dropped event was sent (%d)", n)
			}
			if n := atomic.LoadInt32(&callbacks); n != 0 {
				t.Errorf("callback fired %d times on drop", n)
			}
			if !log.has(log.infos, "beforeEnqueue dropped analytics.Track") {
				t.Errorf("drop not logged at debug: %v", log.infos)
			}
			if len(log.errors) != 0 {
				t.Errorf("drop logged errors: %v", log.errors)
			}
		})
	}
}

func TestBeforeEnqueueHookError(t *testing.T) {
	hooks := map[string]func(Message) Message{
		"panic":       func(Message) Message { panic("boom") },
		"non-event":   func(Message) Message { return testErrorMessage{} },
		"other type":  func(m Message) Message { return Identify{UserId: "u"} },
		"type string": func(m Message) Message { tr := m.(Track); tr.Type = "identify"; return tr },
		"no ids": func(m Message) Message {
			tr := m.(Track)
			tr.UserId, tr.AnonymousId = "", ""
			return tr
		},
		"no event": func(m Message) Message { tr := m.(Track); tr.Event = ""; return tr },
	}
	for name, hook := range hooks {
		for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
			t.Run(fmt.Sprintf("%s/policy=%d", name, policy), func(t *testing.T) {
				c, sent, log := newBeforeEnqueueClient(t, Config{
					BeforeEnqueue:        hook,
					BeforeEnqueueOnError: policy,
					Callback:             failOnCallback(t),
				})
				if err := c.Enqueue(Track{UserId: "u", Event: "E", Properties: Properties{"k": "v"}}); err != nil {
					t.Errorf("hook error raised to app: %v", err)
				}
				c.Close()

				if name == "panic" && (!log.has(log.errors, "panic: string") || log.has(log.errors, "boom")) {
					t.Errorf("panic must be logged by type only: %v", log.errors)
				}
				got := sent()
				if policy == BeforeEnqueueOnErrorDrop {
					if len(got) != 0 {
						t.Errorf("event sent with drop policy: %v", got)
					}
					if !log.has(log.errors, "beforeEnqueue failed, dropping analytics.Track") {
						t.Errorf("error not logged: %v", log.errors)
					}
					return
				}
				if len(got) != 1 || got[0]["type"] != "track" || got[0]["event"] != "E" || got[0]["userId"] != "u" {
					t.Fatalf("original not sent: %v", got)
				}
				if !log.has(log.errors, "beforeEnqueue failed, sending the original analytics.Track") {
					t.Errorf("error not logged: %v", log.errors)
				}
			})
		}
	}
}

func TestBeforeEnqueuePerTypeValidation(t *testing.T) {
	tests := map[string]struct {
		msg  Message
		hook func(Message) Message
	}{
		"group without groupId": {
			Group{GroupId: "g", UserId: "u"},
			func(m Message) Message { g := m.(Group); g.GroupId = ""; return g },
		},
		"alias without previousId": {
			Alias{PreviousId: "p", UserId: "u"},
			func(m Message) Message { a := m.(Alias); a.PreviousId = ""; return a },
		},
		"alias without userId": {
			Alias{PreviousId: "p", UserId: "u"},
			func(m Message) Message { a := m.(Alias); a.UserId = ""; return a },
		},
		"identify without ids": {
			Identify{UserId: "u"},
			func(m Message) Message { i := m.(Identify); i.UserId, i.AnonymousId = "", ""; return i },
		},
		"page without ids": {
			Page{UserId: "u"},
			func(m Message) Message { p := m.(Page); p.UserId, p.AnonymousId = "", ""; return p },
		},
		"screen without ids": {
			Screen{UserId: "u"},
			func(m Message) Message { s := m.(Screen); s.UserId, s.AnonymousId = "", ""; return s },
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, sent, log := newBeforeEnqueueClient(t, Config{BeforeEnqueue: tt.hook})
			c.Enqueue(tt.msg)
			c.Close()

			got := sent()
			if len(got) != 1 {
				t.Fatalf("original not sent: %v", got)
			}
			for k, v := range got[0] {
				if s, ok := v.(string); ok && s == "" && k != "type" {
					t.Errorf("hook edit leaked: %s is empty", k)
				}
			}
			if !log.has(log.errors, "beforeEnqueue failed, sending the original") {
				t.Errorf("validation failure not logged: %v", log.errors)
			}
		})
	}
}

func TestBeforeEnqueueReservedFields(t *testing.T) {
	hooks := map[string]func(Message) Message{
		"library and channel": func(m Message) Message {
			tr := m.(Track)
			tr.Context.Library = LibraryInfo{Name: "spoof", Version: "0"}
			tr.Channel = "web"
			tr.MessageId = "spoofed"
			return tr
		},
		"nil context": func(m Message) Message {
			tr := m.(Track)
			tr.Context = nil
			tr.Channel = ""
			tr.MessageId = ""
			return tr
		},
	}
	for name, hook := range hooks {
		t.Run(name, func(t *testing.T) {
			c, sent, log := newBeforeEnqueueClient(t, Config{BeforeEnqueue: hook})
			c.Enqueue(Track{UserId: "u", Event: "E", MessageId: "mid-1"})
			c.Close()

			got := sent()
			if len(got) != 1 {
				t.Fatalf("expected 1 event, got %v", got)
			}
			if got[0]["messageId"] != "mid-1" {
				t.Errorf("messageId not restored to pre-hook value: %v", got[0]["messageId"])
			}
			if !log.has(log.infos, "reserved field messageId") {
				t.Errorf("messageId warning not logged: %v", log.infos)
			}
			lib := got[0]["context"].(map[string]interface{})["library"].(map[string]interface{})
			if lib["name"] != "analytics-go" || lib["version"] != Version {
				t.Errorf("context.library not restored: %v", lib)
			}
			if got[0]["channel"] != "server" {
				t.Errorf("channel not restored: %v", got[0]["channel"])
			}
			if !log.has(log.infos, "reserved field context.library") || !log.has(log.infos, "reserved field channel") {
				t.Errorf("warnings not logged: %v", log.infos)
			}
			if len(log.errors) != 0 {
				t.Errorf("reserved field restore logged errors: %v", log.errors)
			}
		})
	}
}

func TestBeforeEnqueueRetainedOutputIsNotWritten(t *testing.T) {
	var retained *Track
	c, sent, _ := newBeforeEnqueueClient(t, Config{
		BatchSize: 2, // both events queued before the flush
		BeforeEnqueue: func(m Message) Message {
			if retained == nil {
				tr := m.(Track)
				tr.Context.Library = LibraryInfo{}
				retained = &tr
			}
			return retained
		},
	})
	c.Enqueue(Track{UserId: "u", Event: "E", MessageId: "mid-1"})
	c.Enqueue(Track{UserId: "u", Event: "E", MessageId: "mid-2"})
	c.Close()

	got := sent()
	if len(got) != 2 || got[0]["messageId"] != "mid-1" || got[1]["messageId"] != "mid-2" {
		t.Fatalf("expected mid-1 and mid-2, got %v", got)
	}
	for _, e := range got {
		lib := e["context"].(map[string]interface{})["library"].(map[string]interface{})
		if lib["name"] != "analytics-go" || e["channel"] != "server" {
			t.Errorf("reserved fields not restored: %v", e)
		}
	}
	if retained.MessageId != "mid-1" || retained.Context.Library != (LibraryInfo{}) {
		t.Errorf("SDK wrote into the hook's retained message: messageId=%s library=%v", retained.MessageId, retained.Context.Library)
	}
}

func TestBeforeEnqueueMutateThenFail(t *testing.T) {
	ctx := &Context{Traits: Traits{"email": "a@b.c"}, Extra: map[string]interface{}{"nested": map[string]interface{}{"k": "v"}}}
	props := Properties{"email": "a@b.c", "items": []interface{}{map[string]interface{}{"sku": "1"}}}
	integ := Integrations{"All": true}

	c, sent, _ := newBeforeEnqueueClient(t, Config{
		BeforeEnqueue: func(m Message) Message {
			tr := m.(Track)
			delete(tr.Properties, "email")
			tr.Properties["items"].([]interface{})[0].(map[string]interface{})["sku"] = "x"
			delete(tr.Context.Traits, "email")
			tr.Context.Extra["nested"].(map[string]interface{})["k"] = "x"
			tr.Integrations["All"] = false
			panic("after mutating")
		},
	})
	c.Enqueue(Track{UserId: "u", Event: "E", Context: ctx, Properties: props, Integrations: integ})
	c.Close()

	got := sent()
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %v", got)
	}
	b, _ := json.Marshal(got[0])
	for _, want := range []string{`"email":"a@b.c"`, `"sku":"1"`, `"k":"v"`, `"All":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("partial hook edit leaked, missing %s in %s", want, b)
		}
	}
	if props["email"] != "a@b.c" || ctx.Traits["email"] != "a@b.c" || integ["All"] != true {
		t.Errorf("caller's maps mutated by hook")
	}
}

func TestBeforeEnqueueOversizeAfterHook(t *testing.T) {
	for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
		t.Run(fmt.Sprintf("policy=%d", policy), func(t *testing.T) {
			c, sent, log := newBeforeEnqueueClient(t, Config{
				BeforeEnqueue: func(m Message) Message {
					tr := m.(Track)
					tr.Properties = Properties{"blob": strings.Repeat("x", defMaxMessageBytes)}
					return tr
				},
				BeforeEnqueueOnError: policy,
				Callback:             failOnCallback(t),
			})
			if err := c.Enqueue(Track{UserId: "u", Event: "E"}); err != nil {
				t.Fatal(err)
			}
			c.Close()

			got := sent()
			if policy == BeforeEnqueueOnErrorDrop {
				if len(got) != 0 {
					t.Errorf("event sent with drop policy: %v", got)
				}
			} else if len(got) != 1 || got[0]["properties"] != nil {
				t.Errorf("pre-hook original not sent: %v", got)
			}
			if !log.has(log.errors, ErrMessageTooBig.Error()) {
				t.Errorf("oversize not logged as hook error: %v", log.errors)
			}
		})
	}
}

func TestBeforeEnqueueUncopyableValue(t *testing.T) {
	for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
		t.Run(fmt.Sprintf("policy=%d", policy), func(t *testing.T) {
			var calls int32
			failures := make(chan error, 1)
			c, sent, log := newBeforeEnqueueClient(t, Config{
				BeforeEnqueue:        func(m Message) Message { atomic.AddInt32(&calls, 1); return m },
				BeforeEnqueueOnError: policy,
				Callback:             testCallback{nil, func(m Message, e error) { failures <- e }},
			})
			if err := c.Enqueue(Track{UserId: "u", Event: "E", Properties: Properties{"fn": func() {}}}); err != nil {
				t.Fatal(err)
			}
			c.Close()

			if n := atomic.LoadInt32(&calls); n != 0 {
				t.Errorf("hook called with an uncopyable value")
			}
			if !log.has(log.errors, "cannot copy func value") {
				t.Errorf("copy failure not logged as hook error: %v", log.errors)
			}
			if len(sent()) != 0 {
				t.Error("unserialisable event sent")
			}
			// 'send' hands the original to the usual marshal-failure path.
			if policy == BeforeEnqueueOnErrorSend && len(failures) != 1 {
				t.Error("original did not get the usual marshal failure handling")
			}
			if policy == BeforeEnqueueOnErrorDrop && len(failures) != 0 {
				t.Error("drop policy fired Callback.Failure")
			}
		})
	}
}

const logSecret = "s3cret-pii"

// Encodes as "v" for the first ok calls, then fails with an error quoting
// logSecret. Fields are unexported so the hook's deep copy shares the counter.
type leakyMarshaler struct {
	calls *int32
	ok    int32
}

func (l leakyMarshaler) MarshalJSON() ([]byte, error) {
	if atomic.AddInt32(l.calls, 1) > l.ok {
		return nil, errors.New("leaking " + logSecret + " " + beforeEnqueueMarker)
	}
	return []byte(`"v"`), nil
}

func assertNoSecretLogged(t *testing.T, log *beforeEnqueueLog) {
	t.Helper()
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, line := range append(append([]string{}, log.infos...), log.errors...) {
		if strings.Contains(line, logSecret) {
			t.Errorf("payload logged: %s", line)
		}
	}
}

func TestNoPayloadInLogs(t *testing.T) {
	hooks := map[string]func(Message) Message{
		"no hook": nil,
		"oversize hook": func(m Message) Message {
			tr := m.(Track)
			tr.Properties["blob"] = strings.Repeat("x", defMaxMessageBytes)
			return tr
		},
		"panicking hook":  func(Message) Message { panic("leaking " + logSecret) },
		"error panic":     func(Message) Message { panic(errors.New("leaking " + logSecret)) },
		"type change":     func(m Message) Message { tr := m.(Track); tr.Type = logSecret; return tr },
		"reserved change": func(m Message) Message { tr := m.(Track); tr.Channel = "web"; return tr },
		"dropping hook":   func(Message) Message { return nil },
		"pass-through":    func(m Message) Message { return m },
	}
	for name, hook := range hooks {
		for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
			t.Run(fmt.Sprintf("%s/policy=%d", name, policy), func(t *testing.T) {
				c, _, log := newBeforeEnqueueClient(t, Config{Verbose: true, BeforeEnqueue: hook, BeforeEnqueueOnError: policy})
				c.Enqueue(Track{UserId: "u", Event: "E", Properties: Properties{"email": logSecret}})
				// Oversize without a hook goes through push's existing error log.
				c.Enqueue(Track{UserId: "u", Event: "E", Properties: Properties{"email": logSecret, "blob": strings.Repeat("x", defMaxMessageBytes)}})
				// Marshal error quoting the event: the hook's size check and push's initial encoding.
				var calls int32
				c.Enqueue(Track{UserId: "u", Event: "E", Properties: Properties{"bad": leakyMarshaler{calls: &calls}}})
				c.Close()
				assertNoSecretLogged(t, log)
			})
		}
	}
}

// setSentAt re-marshals at send time; a custom MarshalJSON can fail only then.
func TestNoPayloadInLogsSendTimeMarshalError(t *testing.T) {
	hooks := map[string]struct {
		hook func(Message) Message
		ok   int32 // successful encodings before send: push, plus the hook's size check
	}{
		"no hook":      {nil, 1},
		"pass-through": {func(m Message) Message { return m }, 2},
	}
	for name, tc := range hooks {
		for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
			t.Run(fmt.Sprintf("%s/policy=%d", name, policy), func(t *testing.T) {
				var mu sync.Mutex
				var failures []error
				var calls int32
				c, _, log := newBeforeEnqueueClient(t, Config{
					Verbose:              true,
					BeforeEnqueue:        tc.hook,
					BeforeEnqueueOnError: policy,
					Callback: testCallback{
						func(Message) {},
						func(_ Message, e error) { mu.Lock(); failures = append(failures, e); mu.Unlock() },
					},
				})
				c.Enqueue(Track{UserId: "u", Event: "E", Properties: Properties{"bad": leakyMarshaler{calls: &calls, ok: tc.ok}}})
				c.Close()

				assertNoSecretLogged(t, log)
				if !log.has(log.errors, "*json.MarshalerError") {
					t.Errorf("send-time marshal failure not logged by error type: %v", log.errors)
				}
				mu.Lock()
				defer mu.Unlock()
				// The delivery callback still gets the original error.
				if len(failures) == 0 || !strings.Contains(failures[0].Error(), logSecret) {
					t.Errorf("Callback.Failure did not get the marshal error: %v", failures)
				}
			})
		}
	}
}

func TestBeforeEnqueueOncePerEventOnRetry(t *testing.T) {
	var calls, attempts int32
	ok := make(chan struct{}, 1)
	c, err := NewWithConfig(WRITE_KEY, Config{
		Logger:     testLogger{t.Logf, t.Logf},
		BatchSize:  1,
		RetryAfter: func(int) time.Duration { return time.Millisecond },
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if atomic.AddInt32(&attempts, 1) < 3 {
				return nil, errorTest
			}
			return testTransportOK.RoundTrip(r)
		}),
		Callback: testCallback{func(Message) { ok <- struct{}{} }, func(m Message, e error) { t.Error(e) }},
		BeforeEnqueue: func(m Message) Message {
			atomic.AddInt32(&calls, 1)
			return m
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Enqueue(Track{UserId: "u", Event: "E"})
	<-ok
	c.Close()

	if n := atomic.LoadInt32(&attempts); n != 3 {
		t.Errorf("expected 3 delivery attempts, got %d", n)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("hook called %d times, want 1", n)
	}
}

func TestBeforeEnqueuePassThroughMatchesNoHook(t *testing.T) {
	msg := Track{UserId: "u", AnonymousId: "a", Event: "E", Properties: Properties{"k": 1}, Context: &Context{IP: net.ParseIP("10.0.0.1")}}

	plain, sentPlain, _ := newBeforeEnqueueClient(t, Config{})
	plain.Enqueue(msg)
	plain.Close()

	hooked, sentHooked, _ := newBeforeEnqueueClient(t, Config{BeforeEnqueue: func(m Message) Message { return &[]Track{m.(Track)}[0] }})
	hooked.Enqueue(msg)
	hooked.Close()

	a, _ := json.Marshal(sentPlain())
	b, _ := json.Marshal(sentHooked())
	if string(a) != string(b) {
		t.Errorf("pass-through hook changed payload:\n%s\n%s", a, b)
	}
}

func TestBeforeEnqueueConcurrent(t *testing.T) {
	var calls int32
	c, sent, _ := newBeforeEnqueueClient(t, Config{
		BeforeEnqueue: func(m Message) Message {
			atomic.AddInt32(&calls, 1)
			tr := m.(Track)
			tr.Properties["seen"] = true
			return tr
		},
	})
	shared := Properties{"k": "v"}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Enqueue(Track{UserId: "u", Event: "E", Properties: shared})
		}()
	}
	wg.Wait()
	c.Close()

	if n := atomic.LoadInt32(&calls); n != 50 {
		t.Errorf("hook called %d times, want 50", n)
	}
	if n := len(sent()); n != 50 {
		t.Errorf("sent %d events, want 50", n)
	}
	if _, ok := shared["seen"]; ok {
		t.Error("hook edits leaked into caller's map")
	}
}

func TestBeforeEnqueueConfigValidation(t *testing.T) {
	_, err := NewWithConfig(WRITE_KEY, Config{BeforeEnqueueOnError: 7})
	var cfgErr ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Field != "BeforeEnqueueOnError" {
		t.Errorf("expected ConfigError for BeforeEnqueueOnError, got %v", err)
	}
}

func TestDeepCopyMessage(t *testing.T) {
	cyclic := map[string]interface{}{}
	cyclic["self"] = cyclic
	ts := time.Date(2020, 1, 2, 3, 4, 5, 6, time.UTC)
	orig := Track{
		Event:      "E",
		UserId:     "u",
		Properties: Properties{"cyclic": cyclic, "at": ts, "products": []Product{{SKU: "1"}}},
		Context:    &Context{IP: net.ParseIP("10.0.0.1"), Traits: Traits{"a": 1}},
	}

	m, err := deepCopyMessage(orig)
	if err != nil {
		t.Fatal(err)
	}
	cp := m.(Track)
	cp.Context.IP[len(cp.Context.IP)-1] = 2
	cp.Context.Traits["a"] = 2
	cp.Properties["products"].([]Product)[0].SKU = "2"

	if orig.Context.IP.String() != "10.0.0.1" || orig.Context.Traits["a"] != 1 ||
		orig.Properties["products"].([]Product)[0].SKU != "1" {
		t.Errorf("copy shares state with original: %+v", orig)
	}
	if !cp.Properties["at"].(time.Time).Equal(ts) {
		t.Errorf("time not copied: %v", cp.Properties["at"])
	}
	c := cp.Properties["cyclic"].(map[string]interface{})
	if c["self"].(map[string]interface{})["self"] == nil {
		t.Error("cycle not preserved")
	}
	c["new"] = 1
	if _, ok := cyclic["new"]; ok {
		t.Error("cyclic map shared with original")
	}
}

// The shared fixtures bind SDK-owned metadata to a complete no-hook request.
// Capture at RoundTrip, retaining the endpoint and envelope as well as events.
type beforeEnqueueRequest struct {
	method, url string
	body        interface{}
	raw         string
}

func newBeforeEnqueueWireClient(t *testing.T, cfg Config) (Client, func() []beforeEnqueueRequest, *beforeEnqueueLog) {
	t.Helper()
	var mu sync.Mutex
	var requests []beforeEnqueueRequest
	transport := cfg.Transport
	cfg.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var body interface{}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		requests = append(requests, beforeEnqueueRequest{r.Method, r.URL.String(), body, string(b)})
		mu.Unlock()
		if transport != nil {
			return transport.RoundTrip(r)
		}
		return testTransportOK.RoundTrip(r)
	})
	log := &beforeEnqueueLog{}
	cfg.Logger = log
	cfg.DataPlaneUrl = "https://fixture.example"
	cfg.DisableGzip, cfg.NoProxySupport = true, true
	cfg.Verbose = true
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 1
	}
	cfg.now = func() time.Time { return beforeEnqueueFixtureTime().Add(time.Second) }
	cfg.uid = func() string { return "m1" }
	c, err := NewWithConfig(WRITE_KEY, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c, func() []beforeEnqueueRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]beforeEnqueueRequest(nil), requests...)
	}, log
}

const beforeEnqueueMarker = "BE_PAYLOAD_MARKER_5704"

func beforeEnqueueFixtureTime() time.Time {
	return time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
}

func beforeEnqueueFixtureTrack() Track {
	return Track{
		UserId: "user-1", AnonymousId: "a1", Event: "Order Completed", MessageId: "m1",
		OriginalTimestamp: beforeEnqueueFixtureTime(),
		Properties:        Properties{"marker": beforeEnqueueMarker, "total": 42},
		Context: &Context{Extra: map[string]interface{}{
			"wrapperLibrary": map[string]interface{}{"name": "fixture-wrapper"},
		}},
	}
}

func assertBeforeEnqueueRequests(t *testing.T, got, want []beforeEnqueueRequest, log *beforeEnqueueLog) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("request count = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].method != want[i].method || got[i].url != want[i].url || !reflect.DeepEqual(got[i].body, want[i].body) {
			t.Errorf("complete request %d differs:\ngot %s %s %s\nwant %s %s %v", i, got[i].method, got[i].url, got[i].raw, want[i].method, want[i].url, want[i].body)
		}
		b, err := json.Marshal(want[i].body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), beforeEnqueueMarker) && strings.Contains(got[i].raw, beforeEnqueueMarker) {
			t.Error("removed marker remains in raw request")
		}
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, lines := range [][]string{log.infos, log.errors} {
		for _, line := range lines {
			if strings.Contains(line, beforeEnqueueMarker) {
				t.Errorf("payload marker in log: %s", line)
			}
		}
	}
}

// GO-BE-01–15, 22, 25: all comparisons include the complete serialized HTTP
// body and endpoint. SDK-specific boundary checks remain in the tests above.
func TestBeforeEnqueueConformance(t *testing.T) {
	identity := func(m Message) Message { return m }
	hooks := map[string]func(Message) Message{
		"no_hook_unchanged":           nil,
		"identity_semantically_equal": func(m Message) Message { tr := m.(Track); return &tr },
		"enriched_input": func(m Message) Message {
			tr := m.(Track)
			if tr.Type != "track" || tr.MessageId != "m1" || tr.AnonymousId == "" || tr.Channel != "server" ||
				tr.Context.Library != (LibraryInfo{Name: "analytics-go", Version: Version}) ||
				!tr.SentAt.Equal(tr.OriginalTimestamp) || !tr.OriginalTimestamp.Equal(beforeEnqueueFixtureTime()) ||
				tr.Context.Extra["wrapperLibrary"].(map[string]interface{})["name"] != "fixture-wrapper" {
				t.Error("hook input missing enrichment or wrapper context")
			}
			return tr
		},
		"edit_reaches_wire":         func(m Message) Message { tr := m.(Track); tr.Properties["total"] = 99; return tr },
		"null_drop_accepted":        func(Message) Message { return nil },
		"typed_nil_drop":            func(Message) Message { return (*Track)(nil) },
		"throw_send_original":       func(Message) Message { panic(beforeEnqueueMarker) },
		"exception_text_not_logged": func(Message) Message { panic(errors.New(beforeEnqueueMarker)) },
		"mutate_then_throw_isolated": func(m Message) Message {
			tr := m.(Track)
			tr.Properties["total"] = 99
			tr.Context.Extra["wrapperLibrary"].(map[string]interface{})["name"] = beforeEnqueueMarker
			panic(beforeEnqueueMarker)
		},
		"wrong_shape_hook_error":          func(Message) Message { return testErrorMessage{} },
		"different_concrete_type":         func(Message) Message { return Identify{UserId: "user-1"} },
		"type_change_hook_error":          func(m Message) Message { tr := m.(Track); tr.Type = beforeEnqueueMarker; return tr },
		"restore_messageid":               func(m Message) Message { tr := m.(Track); tr.MessageId = beforeEnqueueMarker; return tr },
		"restore_removed_messageid":       func(m Message) Message { tr := m.(Track); tr.MessageId = ""; return tr },
		"restore_context_library":         func(m Message) Message { tr := m.(Track); tr.Context.Library.Name = beforeEnqueueMarker; return tr },
		"restore_removed_context_library": func(m Message) Message { tr := m.(Track); tr.Context.Library = LibraryInfo{}; return tr },
		"restore_channel":                 func(m Message) Message { tr := m.(Track); tr.Channel = beforeEnqueueMarker; return tr },
		"restore_removed_channel":         func(m Message) Message { tr := m.(Track); tr.Channel = ""; return tr },
		"removed_context_recreated":       func(m Message) Message { tr := m.(Track); tr.Context = nil; return tr },
		"identity_fields_revalidated":     func(m Message) Message { tr := m.(Track); tr.UserId, tr.AnonymousId = "", ""; return tr },
		"required_track_field":            func(m Message) Message { tr := m.(Track); tr.Event = ""; return tr },
		"post_hook_oversize": func(m Message) Message {
			tr := m.(Track)
			tr.Properties["blob"] = strings.Repeat(beforeEnqueueMarker, defMaxMessageBytes/len(beforeEnqueueMarker)+1)
			return tr
		},
		"unserialisable_output": func(m Message) Message {
			tr := m.(Track)
			var calls int32
			tr.Properties["bad"] = leakyMarshaler{calls: &calls}
			return tr
		},
		"no_payload_in_logs": identity,
		"sent_at_ownership":  func(m Message) Message { tr := m.(Track); tr.SentAt = time.Time{}; return tr },
	}
	for name, hook := range hooks {
		for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
			t.Run(fmt.Sprintf("%s/policy=%d", name, policy), func(t *testing.T) {
				msg := beforeEnqueueFixtureTrack()
				baseline, sentBaseline, baselineLog := newBeforeEnqueueWireClient(t, Config{})
				if err := baseline.Enqueue(msg); err != nil {
					t.Fatal(err)
				}
				baseline.Close()
				want := sentBaseline()
				if len(want) != 1 || want[0].method != "POST" || want[0].url != "https://fixture.example/v1/batch" {
					t.Fatal("invalid baseline")
				}
				assertBeforeEnqueueRequests(t, want, want, baselineLog)
				var calls int
				var wrapped func(Message) Message
				if hook != nil {
					wrapped = func(m Message) Message { calls++; return hook(m) }
				}
				var successes, failures int32
				c, sent, log := newBeforeEnqueueWireClient(t, Config{
					BeforeEnqueue: wrapped, BeforeEnqueueOnError: policy,
					Callback: testCallback{func(Message) { atomic.AddInt32(&successes, 1) }, func(Message, error) { atomic.AddInt32(&failures, 1) }},
				})
				if err := c.Enqueue(msg); err != nil {
					t.Fatal(err)
				}
				c.Close()
				isError := name == "throw_send_original" || name == "exception_text_not_logged" || name == "mutate_then_throw_isolated" || name == "wrong_shape_hook_error" || name == "different_concrete_type" || name == "type_change_hook_error" || name == "identity_fields_revalidated" || name == "required_track_field" || name == "post_hook_oversize" || name == "unserialisable_output"
				if name == "null_drop_accepted" || name == "typed_nil_drop" || (isError && policy == BeforeEnqueueOnErrorDrop) {
					want = nil
				} else {
					event := want[0].body.(map[string]interface{})["batch"].([]interface{})[0].(map[string]interface{})
					if name == "edit_reaches_wire" {
						event["properties"].(map[string]interface{})["total"] = float64(99)
					}
					if name == "removed_context_recreated" {
						delete(event["context"].(map[string]interface{}), "wrapperLibrary")
					}
				}
				assertBeforeEnqueueRequests(t, sent(), want, log)
				if hook != nil && calls != 1 {
					t.Errorf("hook calls = %d, want 1", calls)
				}
				if atomic.LoadInt32(&failures) != 0 || int(atomic.LoadInt32(&successes)) != len(want) {
					t.Error("unexpected delivery callbacks")
				}
				if isError != (len(log.errors) > 0) {
					t.Errorf("hook error logs = %v", log.errors)
				}
				if strings.HasPrefix(name, "restore_") || name == "removed_context_recreated" {
					if !log.has(log.infos, "changed reserved field") {
						t.Error("missing restoration warning")
					}
				}
				if msg.Properties["total"] != 42 || msg.Context.Extra["wrapperLibrary"].(map[string]interface{})["name"] != "fixture-wrapper" {
					t.Error("caller data mutated")
				}
				if name == "no_hook_unchanged" && len(want) == 1 && sent()[0].raw != want[0].raw {
					t.Error("no-hook bytes changed")
				}
			})
		}
	}
}

// GO-BE-26: JSON serialization rejects a memoised cycle inside the hook-error
// boundary. Send retains the usual downstream encoding failure; Drop does not.
func TestBeforeEnqueueCyclicInputConformance(t *testing.T) {
	for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
		t.Run(fmt.Sprint(policy), func(t *testing.T) {
			msg := beforeEnqueueFixtureTrack()
			msg.Properties["self"] = msg.Properties
			var calls int
			failures := make(chan error, 1)
			c, sent, log := newBeforeEnqueueWireClient(t, Config{
				BeforeEnqueue: func(m Message) Message {
					calls++
					tr := m.(Track)
					tr.Properties["self"].(Properties)["copied"] = true
					if tr.Properties["copied"] != true {
						t.Error("copy lost cycle")
					}
					return tr
				}, BeforeEnqueueOnError: policy,
				Callback: testCallback{func(Message) { t.Error("cyclic event sent") }, func(_ Message, err error) { failures <- err }},
			})
			if err := c.Enqueue(msg); err != nil {
				t.Fatal(err)
			}
			c.Close()
			assertBeforeEnqueueRequests(t, sent(), nil, log)
			if calls != 1 || !log.has(log.errors, "beforeEnqueue failed") {
				t.Error("cycle did not end as hook error")
			}
			if _, ok := msg.Properties["copied"]; ok {
				t.Error("caller cycle mutated")
			}
			if msg.Properties["marker"] != beforeEnqueueMarker {
				t.Error("caller marker changed")
			}
			if policy == BeforeEnqueueOnErrorSend {
				if len(failures) != 1 {
					t.Fatal("missing downstream failure")
				}
				var unsupported *json.UnsupportedValueError
				if !errors.As(<-failures, &unsupported) {
					t.Error("wrong downstream error")
				}
			} else if len(failures) != 0 {
				t.Error("hook error fired failure callback")
			}
		})
	}
}

// GO-BE-27: choose the smallest repeated marker exceeding the enriched size.
func TestBeforeEnqueueOriginalOversizeConformance(t *testing.T) {
	msg := beforeEnqueueFixtureTrack()
	msg.Type, msg.Channel = "track", "server"
	msg.SentAt = msg.OriginalTimestamp
	msg.Context.Library = LibraryInfo{Name: "analytics-go", Version: Version}
	msg.Properties["blob"] = ""
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	n := (defMaxMessageBytes-len(b))/len(beforeEnqueueMarker) + 1
	msg.Properties["blob"] = strings.Repeat(beforeEnqueueMarker, n)
	for _, hooked := range []bool{false, true} {
		t.Run(fmt.Sprint(hooked), func(t *testing.T) {
			var calls int
			var hook func(Message) Message
			if hooked {
				hook = func(Message) Message { calls++; panic("marker-free error") }
			}
			failures := make(chan error, 1)
			c, sent, log := newBeforeEnqueueWireClient(t, Config{BeforeEnqueue: hook,
				Callback: testCallback{func(Message) { t.Error("oversized event sent") }, func(_ Message, err error) { failures <- err }},
			})
			if err := c.Enqueue(msg); err != nil {
				t.Fatal(err)
			}
			c.Close()
			assertBeforeEnqueueRequests(t, sent(), nil, log)
			if len(failures) != 1 || !errors.Is(<-failures, ErrMessageTooBig) {
				t.Error("original lost downstream size failure")
			}
			if hooked && (calls != 1 || !log.has(log.errors, "beforeEnqueue failed")) {
				t.Error("hook not called once")
			}
			if msg.Properties["blob"] != strings.Repeat(beforeEnqueueMarker, n) {
				t.Error("caller blob changed")
			}
		})
	}
}

// GO-BE-28: aliasing is valid and preserved without sharing caller state.
func TestBeforeEnqueueSharedAcyclicConformance(t *testing.T) {
	msg := beforeEnqueueFixtureTrack()
	shared := map[string]interface{}{"marker": beforeEnqueueMarker}
	msg.Properties["a"], msg.Properties["b"] = shared, shared
	baseline, sentBaseline, _ := newBeforeEnqueueWireClient(t, Config{})
	if err := baseline.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	baseline.Close()
	var calls int
	c, sent, log := newBeforeEnqueueWireClient(t, Config{BeforeEnqueue: func(m Message) Message {
		calls++
		tr := m.(Track)
		a := tr.Properties["a"].(map[string]interface{})
		a["temporary"] = true
		if tr.Properties["b"].(map[string]interface{})["temporary"] != true {
			t.Error("copy lost shared reference")
		}
		delete(a, "temporary")
		return tr
	}, Callback: failOnCallback(t)})
	if err := c.Enqueue(msg); err != nil {
		t.Fatal(err)
	}
	c.Close()
	assertBeforeEnqueueRequests(t, sent(), sentBaseline(), log)
	if calls != 1 || len(log.errors) != 0 || len(shared) != 1 || shared["marker"] != beforeEnqueueMarker {
		t.Error("invalid shared-container handling")
	}
}

func TestBeforeEnqueueGoexitPropagates(t *testing.T) {
	c, sent, log := newBeforeEnqueueWireClient(t, Config{BeforeEnqueue: func(Message) Message { runtime.Goexit(); return nil }})
	done := make(chan struct{})
	returned := make(chan struct{}, 1)
	go func() {
		defer close(done)
		c.Enqueue(beforeEnqueueFixtureTrack())
		returned <- struct{}{}
	}()
	<-done
	c.Close()
	assertBeforeEnqueueRequests(t, sent(), nil, log)
	if len(returned) != 0 || len(log.errors) != 0 {
		t.Error("Goexit was intercepted")
	}
}

func TestBeforeEnqueueNilPanicConformance(t *testing.T) {
	baseline, sentBaseline, _ := newBeforeEnqueueWireClient(t, Config{})
	baseline.Enqueue(beforeEnqueueFixtureTrack())
	baseline.Close()
	c, sent, log := newBeforeEnqueueWireClient(t, Config{BeforeEnqueue: func(Message) Message { panic(nil) }})
	if err := c.Enqueue(beforeEnqueueFixtureTrack()); err != nil {
		t.Fatal(err)
	}
	c.Close()
	assertBeforeEnqueueRequests(t, sent(), sentBaseline(), log)
	if !log.has(log.errors, "panic:") {
		t.Error("nil panic not treated as hook error")
	}
}

// GO-BE-10: required fields and identity checks for every other message type.
func TestBeforeEnqueuePerTypeConformance(t *testing.T) {
	tr := beforeEnqueueFixtureTrack()
	tests := map[string]struct {
		msg  Message
		hook func(Message) Message
	}{
		"required_group_field": {
			Group{UserId: tr.UserId, AnonymousId: tr.AnonymousId, GroupId: "g1", Traits: Traits{"marker": beforeEnqueueMarker}, Context: tr.Context, MessageId: "m1", OriginalTimestamp: tr.OriginalTimestamp},
			func(m Message) Message { g := m.(Group); g.GroupId = ""; return g },
		},
		"required_alias_field": {
			Alias{UserId: tr.UserId, PreviousId: "previous-1", Context: tr.Context, MessageId: "m1", OriginalTimestamp: tr.OriginalTimestamp},
			func(m Message) Message { a := m.(Alias); a.PreviousId = ""; return a },
		},
		"alias_identity": {
			Alias{UserId: tr.UserId, PreviousId: "previous-1", Context: tr.Context, MessageId: "m1", OriginalTimestamp: tr.OriginalTimestamp},
			func(m Message) Message { a := m.(Alias); a.UserId = ""; return a },
		},
		"identify_identity": {
			Identify{UserId: tr.UserId, Traits: Traits{"marker": beforeEnqueueMarker}, Context: tr.Context, MessageId: "m1", OriginalTimestamp: tr.OriginalTimestamp},
			func(m Message) Message { i := m.(Identify); i.UserId, i.AnonymousId = "", ""; return i },
		},
		"page_identity": {
			Page{UserId: tr.UserId, AnonymousId: tr.AnonymousId, Name: "page", Properties: tr.Properties, Context: tr.Context, MessageId: "m1", OriginalTimestamp: tr.OriginalTimestamp},
			func(m Message) Message { p := m.(Page); p.UserId, p.AnonymousId = "", ""; return p },
		},
		"screen_identity": {
			Screen{UserId: tr.UserId, AnonymousId: tr.AnonymousId, Name: "screen", Properties: tr.Properties, Context: tr.Context, MessageId: "m1", OriginalTimestamp: tr.OriginalTimestamp},
			func(m Message) Message { s := m.(Screen); s.UserId, s.AnonymousId = "", ""; return s },
		},
	}
	for name, tt := range tests {
		for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
			t.Run(fmt.Sprintf("%s/policy=%d", name, policy), func(t *testing.T) {
				baseline, baselineSent, _ := newBeforeEnqueueWireClient(t, Config{})
				if err := baseline.Enqueue(tt.msg); err != nil {
					t.Fatal(err)
				}
				baseline.Close()
				c, sent, log := newBeforeEnqueueWireClient(t, Config{BeforeEnqueue: tt.hook, BeforeEnqueueOnError: policy, Callback: failOnCallback(t)})
				if err := c.Enqueue(tt.msg); err != nil {
					t.Fatal(err)
				}
				c.Close()
				want := baselineSent()
				if policy == BeforeEnqueueOnErrorDrop {
					want = nil
				}
				assertBeforeEnqueueRequests(t, sent(), want, log)
				if !log.has(log.errors, "beforeEnqueue failed") {
					t.Error("missing validation error")
				}
			})
		}
	}
}

// GO-BE-24: complete two-event request, including distinct restored IDs.
func TestBeforeEnqueueRetainedConformance(t *testing.T) {
	first, second := beforeEnqueueFixtureTrack(), beforeEnqueueFixtureTrack()
	second.MessageId = "m2"
	baseline, baselineSent, _ := newBeforeEnqueueWireClient(t, Config{BatchSize: 2})
	baseline.Enqueue(first)
	baseline.Enqueue(second)
	baseline.Close()
	var retained *Track
	c, sent, log := newBeforeEnqueueWireClient(t, Config{BatchSize: 2, BeforeEnqueue: func(m Message) Message {
		if retained == nil {
			tr := m.(Track)
			tr.Context.Library = LibraryInfo{}
			retained = &tr
		}
		return retained
	}, Callback: failOnCallback(t)})
	c.Enqueue(first)
	c.Enqueue(second)
	c.Close()
	assertBeforeEnqueueRequests(t, sent(), baselineSent(), log)
	if retained.MessageId != "m1" || retained.Context.Library != (LibraryInfo{}) {
		t.Error("SDK wrote into retained result")
	}
}

// GO-BE-14: compare every full retry request, not just the final event.
func TestBeforeEnqueueRetryConformance(t *testing.T) {
	baseline, baselineSent, _ := newBeforeEnqueueWireClient(t, Config{})
	baseline.Enqueue(beforeEnqueueFixtureTrack())
	baseline.Close()
	want := baselineSent()
	want[0].body.(map[string]interface{})["batch"].([]interface{})[0].(map[string]interface{})["properties"].(map[string]interface{})["total"] = float64(99)
	var attempts int32
	var calls int
	done := make(chan struct{}, 1)
	c, sent, log := newBeforeEnqueueWireClient(t, Config{
		BeforeEnqueue: func(m Message) Message { calls++; tr := m.(Track); tr.Properties["total"] = 99; return tr },
		RetryAfter:    func(int) time.Duration { return time.Millisecond },
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if atomic.AddInt32(&attempts, 1) == 1 {
				return testTransportError.RoundTrip(r)
			}
			return testTransportOK.RoundTrip(r)
		}),
		Callback: testCallback{func(Message) { done <- struct{}{} }, func(_ Message, err error) { t.Error(err); done <- struct{}{} }},
	})
	if err := c.Enqueue(beforeEnqueueFixtureTrack()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("timed out waiting for retry")
	}
	c.Close()
	assertBeforeEnqueueRequests(t, sent(), []beforeEnqueueRequest{want[0], want[0]}, log)
	if calls != 1 {
		t.Errorf("hook called %d times", calls)
	}
}

// SDK-BEFORE-ENQUEUE-025: application-defined replacement type names are data.
type BE_PAYLOAD_MARKER_5704 struct{}

func (BE_PAYLOAD_MARKER_5704) Validate() error { return nil }

func TestBeforeEnqueueReplacementTypeNotLogged(t *testing.T) {
	baseline, baselineSent, _ := newBeforeEnqueueWireClient(t, Config{})
	baseline.Enqueue(beforeEnqueueFixtureTrack())
	baseline.Close()
	c, sent, log := newBeforeEnqueueWireClient(t, Config{BeforeEnqueue: func(Message) Message { return BE_PAYLOAD_MARKER_5704{} }})
	c.Enqueue(beforeEnqueueFixtureTrack())
	c.Close()
	assertBeforeEnqueueRequests(t, sent(), baselineSent(), log)
}

type BE_PAYLOAD_MARKER_5704_func func()

func TestBeforeEnqueueCopyTypeNotLogged(t *testing.T) {
	msg := beforeEnqueueFixtureTrack()
	msg.Properties["bad"] = BE_PAYLOAD_MARKER_5704_func(func() {})
	c, sent, log := newBeforeEnqueueWireClient(t, Config{
		BeforeEnqueue:        func(m Message) Message { t.Error("hook called on uncopyable input"); return m },
		BeforeEnqueueOnError: BeforeEnqueueOnErrorDrop,
	})
	c.Enqueue(msg)
	c.Close()
	assertBeforeEnqueueRequests(t, sent(), nil, log)
}

type beforeEnqueueWrappedErrorMarshaler struct{}

func (beforeEnqueueWrappedErrorMarshaler) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("%s: %w", beforeEnqueueMarker, ErrMessageTooBig)
}

func TestBeforeEnqueueWrappedMarshalErrorNotLogged(t *testing.T) {
	msg := beforeEnqueueFixtureTrack()
	msg.Properties["bad"] = beforeEnqueueWrappedErrorMarshaler{}
	c, sent, log := newBeforeEnqueueWireClient(t, Config{})
	c.Enqueue(msg)
	c.Close()
	assertBeforeEnqueueRequests(t, sent(), nil, log)
}
