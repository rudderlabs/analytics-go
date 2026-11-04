package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type beforeEnqueueLog struct {
	mu            sync.Mutex
	infos, errors []string
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

func hookLogCount(lines []string, text string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, text) {
			n++
		}
	}
	return n
}

type hookResult struct {
	wire      []map[string]interface{}
	log       *beforeEnqueueLog
	err       error
	successes int
	failures  []error
	attempts  int
}

// Close joins transport and callback work before assertions read the result.
func runHook(t *testing.T, cfg Config, msg Message) hookResult {
	t.Helper()
	r := hookResult{log: &beforeEnqueueLog{}}
	var mu sync.Mutex
	transport := cfg.Transport
	cfg.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Batch []map[string]interface{} `json:"batch"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		r.attempts++
		r.wire = append(r.wire, body.Batch...)
		mu.Unlock()
		if transport != nil {
			return transport.RoundTrip(req)
		}
		return testTransportOK.RoundTrip(req)
	})
	cfg.Callback = testCallback{
		func(Message) { mu.Lock(); defer mu.Unlock(); r.successes++ },
		func(_ Message, err error) { mu.Lock(); defer mu.Unlock(); r.failures = append(r.failures, err) },
	}
	cfg.Logger = r.log
	cfg.Verbose, cfg.DisableGzip, cfg.NoProxySupport = true, true, true
	cfg.BatchSize = 1
	cfg.now, cfg.uid = mockTime, mockId
	c, err := NewWithConfig(WRITE_KEY, cfg)
	if err != nil {
		t.Fatal(err)
	}
	r.err = c.Enqueue(msg)
	if transport != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.(*client).Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	return r
}

func hookTrack() Track {
	return Track{UserId: "u", AnonymousId: "a1", Event: "purchase", MessageId: "m1", OriginalTimestamp: mockTime(),
		Properties: Properties{"total": 1, "nested": map[string]interface{}{"items": []interface{}{1}}},
		Context:    &Context{Traits: Traits{"plan": "pro"}, Extra: map[string]interface{}{"region": "eu"}}}
}

func editHook(edit func(*Track)) func(Message) Message {
	return func(m Message) Message { tr := m.(Track); edit(&tr); return tr }
}

func assertHookSent(t *testing.T, r hookResult, count int) {
	t.Helper()
	if r.err != nil || len(r.wire) != count || r.successes != count || len(r.failures) != 0 {
		t.Fatalf("enqueue=%v wire=%d success=%d failures=%v", r.err, len(r.wire), r.successes, r.failures)
	}
}

type hookUser struct{ ID string }
type hookUserRef *hookUser

func TestBeforeEnqueueCopyKeepsDefinedPointerTypes(t *testing.T) {
	in := Properties{"user": hookUserRef(&hookUser{ID: "u1"})}
	out := deepCopyValue(reflect.ValueOf(in)).Interface().(Properties)
	ref, ok := out["user"].(hookUserRef)
	if !ok || ref == in["user"].(hookUserRef) || ref.ID != "u1" {
		t.Fatalf("got %T %v, want a copied hookUserRef", out["user"], out["user"])
	}
}

func TestBeforeEnqueueEdit(t *testing.T) {
	r := runHook(t, Config{BeforeEnqueue: editHook(func(tr *Track) { tr.Properties["total"] = 99 })}, hookTrack())
	assertHookSent(t, r, 1)
	if r.wire[0]["properties"].(map[string]interface{})["total"] != float64(99) {
		t.Fatal("edit missing")
	}
}

func TestBeforeEnqueueNilDrop(t *testing.T) {
	for _, out := range []Message{nil, (*Track)(nil)} {
		r := runHook(t, Config{BeforeEnqueue: func(Message) Message { return out }}, hookTrack())
		assertHookSent(t, r, 0)
		if hookLogCount(r.log.infos, "beforeEnqueue dropped track event m1") != 1 {
			t.Fatal("missing drop log")
		}
	}
}

func TestBeforeEnqueueClosedClient(t *testing.T) {
	calls := 0
	c, err := NewWithConfig(WRITE_KEY, Config{BeforeEnqueue: func(Message) Message { calls++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if err = c.Enqueue(hookTrack()); !errors.Is(err, ErrClosed) || calls != 0 {
		t.Fatalf("got err=%v calls=%d, want ErrClosed and no hook call", err, calls)
	}
}

func TestBeforeEnqueuePanicNil(t *testing.T) {
	// Go < 1.21 behaviour: recover() returns nil for panic(nil)
	t.Setenv("GODEBUG", "panicnil=1")
	r := runHook(t, Config{BeforeEnqueue: func(Message) Message { panic(nil) }}, hookTrack()) //nolint:govet
	assertHookSent(t, r, 1)
	if hookLogCount(r.log.errors, "sending the original event") != 1 {
		t.Fatal("panic(nil) was treated as a drop")
	}
}

func TestBeforeEnqueuePanicSend(t *testing.T) {
	original := hookTrack()
	r := runHook(t, Config{BeforeEnqueue: func(m Message) Message {
		tr := m.(Track)
		tr.Properties["nested"].(map[string]interface{})["items"].([]interface{})[0] = 99
		tr.Context.Traits["plan"] = "changed"
		panic(errors.New("hook boom"))
	}}, original)
	assertHookSent(t, r, 1)
	baseline := runHook(t, Config{}, hookTrack())
	if !reflect.DeepEqual(r.wire, baseline.wire) {
		t.Fatal("fallback changed")
	}
	if hookLogCount(r.log.errors, "beforeEnqueue failed for track event m1, sending the original event. hook boom") != 1 {
		t.Fatal(r.log.errors)
	}
}

func TestBeforeEnqueuePanicDrop(t *testing.T) {
	r := runHook(t, Config{BeforeEnqueue: func(Message) Message { panic("boom") }, BeforeEnqueueOnError: BeforeEnqueueOnErrorDrop}, hookTrack())
	assertHookSent(t, r, 0)
	if hookLogCount(r.log.errors, "dropping the event. boom") != 1 {
		t.Fatal(r.log.errors)
	}
}

func TestBeforeEnqueueValidationFailure(t *testing.T) {
	for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
		for _, input := range []Track{hookTrack(), {UserId: "u"}} {
			r := runHook(t, Config{BeforeEnqueue: editHook(func(tr *Track) { tr.Event = "" }), BeforeEnqueueOnError: policy}, input)
			if hookLogCount(r.log.errors, "beforeEnqueue failed") != 1 {
				t.Fatal(r.log.errors)
			}
			if policy == BeforeEnqueueOnErrorDrop {
				assertHookSent(t, r, 0)
			} else if input.Event == "" {
				if r.err == nil || len(r.wire) != 0 || len(r.failures) != 0 {
					t.Fatal("invalid original did not surface normally")
				}
			} else {
				assertHookSent(t, r, 1)
			}
		}
	}
}

func TestBeforeEnqueueFixesInvalidCall(t *testing.T) {
	r := runHook(t, Config{BeforeEnqueue: editHook(func(tr *Track) { tr.Event = "fixed" })}, Track{UserId: "u"})
	assertHookSent(t, r, 1)
	if r.wire[0]["event"] != "fixed" || len(r.log.errors) != 0 {
		t.Fatal("invalid call was not fixed")
	}
}

func TestBeforeEnqueueRestoresSDKFields(t *testing.T) {
	r := runHook(t, Config{BeforeEnqueue: editHook(func(tr *Track) { tr.Channel = "browser"; tr.Context = nil })}, hookTrack())
	assertHookSent(t, r, 1)
	if r.wire[0]["channel"] != "server" || r.wire[0]["context"].(map[string]interface{})["library"].(map[string]interface{})["name"] != "analytics-go" {
		t.Fatal("SDK fields not restored")
	}
	if hookLogCount(r.log.infos, "beforeEnqueue changed channel or context.library; they were restored.") != 1 {
		t.Fatal(r.log.infos)
	}
}

func TestBeforeEnqueueMessageID(t *testing.T) {
	for _, id := range []string{"m2", ""} {
		r := runHook(t, Config{BeforeEnqueue: editHook(func(tr *Track) { tr.MessageId = id })}, hookTrack())
		assertHookSent(t, r, 1)
		want := id
		if want == "" {
			want = "m1"
		}
		if r.wire[0]["messageId"] != want || hookLogCount(r.log.infos, "beforeEnqueue changed") != 0 {
			t.Fatal("unexpected messageId restoration")
		}
	}
}

func TestBeforeEnqueueOversizeUsesNormalHandling(t *testing.T) {
	for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
		r := runHook(t, Config{BeforeEnqueue: editHook(func(tr *Track) { tr.Properties["large"] = strings.Repeat("x", defMaxMessageBytes) }), BeforeEnqueueOnError: policy}, hookTrack())
		if r.err != nil || len(r.wire) != 0 || len(r.failures) != 1 || r.failures[0] != ErrMessageTooBig {
			t.Fatal("normal size handling changed")
		}
		if hookLogCount(r.log.errors, "beforeEnqueue failed") != 0 || hookLogCount(r.log.errors, ErrMessageTooBig.Error()) != 1 {
			t.Fatal(r.log.errors)
		}
	}
}

func retryHookConfig(calls *int) Config {
	attempt := 0
	return Config{BeforeEnqueue: func(m Message) Message { *calls++; return m },
		RetryAfter: func(int) time.Duration { return time.Millisecond },
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			attempt++
			if attempt == 1 {
				return testTransportError.RoundTrip(req)
			}
			return testTransportOK.RoundTrip(req)
		})}
}

func TestBeforeEnqueueOnceAcrossRetries(t *testing.T) {
	calls := 0
	r := runHook(t, retryHookConfig(&calls), hookTrack())
	if r.err != nil || r.attempts != 2 || calls != 1 || r.successes != 1 || len(r.failures) != 0 {
		t.Fatal("retry reran hook or failed")
	}
	if !reflect.DeepEqual(r.wire[0], r.wire[1]) {
		t.Fatal("retry changed event")
	}
}

func TestBeforeEnqueueOptionValidation(t *testing.T) {
	if c, err := NewWithConfig(WRITE_KEY, Config{BeforeEnqueueOnError: BeforeEnqueueErrorPolicy(99)}); err == nil || c != nil {
		t.Fatal("invalid policy accepted")
	}
	for _, policy := range []BeforeEnqueueErrorPolicy{BeforeEnqueueOnErrorSend, BeforeEnqueueOnErrorDrop} {
		assertHookSent(t, runHook(t, Config{BeforeEnqueueOnError: policy}, hookTrack()), 1)
	}
}

func TestBeforeEnqueueNoHookUnchanged(t *testing.T) {
	baseline := runHook(t, Config{}, hookTrack())
	identity := runHook(t, Config{BeforeEnqueue: func(m Message) Message { return m }}, hookTrack())
	assertHookSent(t, baseline, 1)
	assertHookSent(t, identity, 1)
	if !reflect.DeepEqual(baseline.wire, identity.wire) || hookLogCount(identity.log.infos, "beforeEnqueue") != 0 || len(identity.log.errors) != 0 {
		t.Fatal("identity differs from no hook")
	}
	invalid := runHook(t, Config{}, Track{UserId: "u"})
	if invalid.err == nil || len(invalid.wire) != 0 {
		t.Fatal("no-hook validation changed")
	}
}

type hookNonObject int

func (hookNonObject) Validate() error { return nil }

func TestBeforeEnqueueNonObjectResult(t *testing.T) {
	r := runHook(t, Config{BeforeEnqueue: func(Message) Message { return hookNonObject(42) }}, hookTrack())
	assertHookSent(t, r, 1)
	if hookLogCount(r.log.errors, "beforeEnqueue must return an SDK message or nil") != 1 {
		t.Fatal(r.log.errors)
	}
}

func TestBeforeEnqueueAllowsMessageTypeChange(t *testing.T) {
	r := runHook(t, Config{BeforeEnqueue: func(Message) Message {
		return Identify{UserId: "new-user", Traits: Traits{"plan": "new"}}
	}}, hookTrack())
	assertHookSent(t, r, 1)
	if r.wire[0]["type"] != "identify" || r.wire[0]["messageId"] != "m1" || r.wire[0]["userId"] != "new-user" {
		t.Fatal("type change rejected")
	}
}

func TestBeforeEnqueueConformance(t *testing.T) {
	tests := []struct {
		id     string
		hook   func(Message) Message
		policy BeforeEnqueueErrorPolicy
		skip   string
	}{
		{id: "enriched_input", hook: func(m Message) Message {
			tr := m.(Track)
			if tr.Type != "track" || tr.MessageId != "m1" || tr.Channel != "server" || tr.Context.Library != (LibraryInfo{Name: "analytics-go", Version: Version}) || tr.AnonymousId == "" || tr.OriginalTimestamp != mockTime() || tr.SentAt != tr.OriginalTimestamp {
				t.Error("input not enriched")
			}
			return m
		}},
		{id: "edit_reaches_wire", hook: editHook(func(tr *Track) { tr.Properties["total"] = 99 })},
		{id: "null_drop", hook: func(Message) Message { return nil }},
		{id: "throw_send_original", hook: func(Message) Message { panic("boom") }},
		{id: "throw_drop_none", hook: func(Message) Message { panic("boom") }, policy: BeforeEnqueueOnErrorDrop},
		{id: "mutate_then_throw_isolated", hook: func(m Message) Message { m.(Track).Properties["total"] = 99; panic("boom") }},
		{id: "non_object_hook_error", hook: func(Message) Message { return hookNonObject(42) }},
		{id: "required_field_hook_error", hook: editHook(func(tr *Track) { tr.Event = "" })},
		{id: "required_field_hook_error_drop", hook: editHook(func(tr *Track) { tr.Event = "" }), policy: BeforeEnqueueOnErrorDrop},
		{id: "hook_fixes_invalid_call", hook: editHook(func(tr *Track) { tr.Event = "fixed" })},
		{id: "restore_channel", hook: editHook(func(tr *Track) { tr.Channel = "browser" })},
		{id: "restore_context_library", hook: editHook(func(tr *Track) { tr.Context.Library = LibraryInfo{} })},
		{id: "keep_messageid_change", hook: editHook(func(tr *Track) { tr.MessageId = "m2" })},
		{id: "restore_removed_messageid", hook: editHook(func(tr *Track) { tr.MessageId = "" })},
		{id: "post_hook_oversize_normal_handling", hook: editHook(func(tr *Track) { tr.Properties["large"] = strings.Repeat("x", defMaxMessageBytes) })},
		{id: "cyclic_output_hook_error", hook: editHook(func(tr *Track) { tr.Properties["self"] = tr.Properties })},
		{id: "once_across_retries"},
		{id: "no_hook_unchanged"},
		{id: "invalid_hook_config", skip: "Config.BeforeEnqueue is a function field; non-function values cannot compile"},
		{id: "invalid_policy_config"},
	}
	baseline := runHook(t, Config{}, hookTrack())
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if tt.skip != "" {
				t.Skip(tt.skip)
			}
			if tt.id == "invalid_policy_config" {
				if c, err := NewWithConfig(WRITE_KEY, Config{BeforeEnqueueOnError: BeforeEnqueueErrorPolicy(99)}); err == nil || c != nil {
					t.Fatal("invalid policy accepted")
				}
				return
			}
			cfg := Config{BeforeEnqueue: tt.hook, BeforeEnqueueOnError: tt.policy}
			calls := 0
			if tt.id == "once_across_retries" {
				cfg = retryHookConfig(&calls)
			}
			msg := hookTrack()
			if tt.id == "hook_fixes_invalid_call" {
				msg.Event = ""
			}
			r := runHook(t, cfg, msg)
			want := runHookExpected(baseline.wire[0])
			wantErrors, wantWarnings := 0, 0
			switch tt.id {
			case "null_drop":
				want = nil
				if hookLogCount(r.log.infos, "beforeEnqueue dropped") != 1 {
					t.Fatal("missing drop debug log")
				}
			case "throw_drop_none", "required_field_hook_error_drop":
				want = nil
				wantErrors = 1
			case "throw_send_original", "mutate_then_throw_isolated", "required_field_hook_error", "cyclic_output_hook_error", "non_object_hook_error":
				wantErrors = 1
			case "edit_reaches_wire":
				want[0]["properties"].(map[string]interface{})["total"] = float64(99)
			case "hook_fixes_invalid_call":
				want[0]["event"] = "fixed"
			case "restore_channel", "restore_context_library":
				wantWarnings = 1
			case "keep_messageid_change":
				want[0]["messageId"] = "m2"
			case "post_hook_oversize_normal_handling":
				// Go rejects oversize through the normal queue, unlike Node's warning-and-send path.
				if r.err != nil || len(r.wire) != 0 || len(r.failures) != 1 || r.failures[0] != ErrMessageTooBig || hookLogCount(r.log.errors, "beforeEnqueue failed") != 0 || hookLogCount(r.log.errors, ErrMessageTooBig.Error()) != 1 {
					t.Fatal("normal Go size handling changed")
				}
				return
			case "once_across_retries":
				if calls != 1 || r.attempts != 2 || r.successes != 1 || len(r.failures) != 0 || !reflect.DeepEqual(r.wire[0], r.wire[1]) {
					t.Fatal("retry changed hook behavior")
				}
				r.wire = r.wire[:1]
			}
			assertHookSent(t, r, len(want))
			if !reflect.DeepEqual(r.wire, want) {
				t.Fatalf("wire mismatch: got %v want %v", r.wire, want)
			}
			if hookLogCount(r.log.errors, "beforeEnqueue failed") != wantErrors || hookLogCount(r.log.infos, "beforeEnqueue changed") != wantWarnings {
				t.Fatalf("logs: %v %v", r.log.errors, r.log.infos)
			}
			if wantErrors == 0 && tt.id != "once_across_retries" && len(r.log.errors) != 0 {
				t.Fatal(r.log.errors)
			}
		})
	}
}

func runHookExpected(event map[string]interface{}) []map[string]interface{} {
	// Expected wire data is JSON-native; keep each fixture's expectation independent.
	b, _ := json.Marshal(event)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return []map[string]interface{}{out}
}
