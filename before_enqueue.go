package analytics

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// BeforeEnqueueErrorPolicy selects what the client does with an event when
// Config.BeforeEnqueue fails: panics, returns a message of another type, or
// returns a message that no longer validates or exceeds MaxMessageBytes.
type BeforeEnqueueErrorPolicy int

const (
	// BeforeEnqueueOnErrorSend sends the original, unmodified event. Default.
	BeforeEnqueueOnErrorSend BeforeEnqueueErrorPolicy = iota

	// BeforeEnqueueOnErrorDrop drops the event. Use it when the hook redacts
	// PII, so a broken hook cannot leak an unredacted event.
	BeforeEnqueueOnErrorDrop
)

// beforeEnqueue runs Config.BeforeEnqueue on a deep copy of msg. It returns
// the message to queue, or false when the event must not be queued.
func (c *client) beforeEnqueue(msg Message) (Message, bool) {
	out, err := c.runBeforeEnqueue(msg)
	if err != nil {
		if c.BeforeEnqueueOnError == BeforeEnqueueOnErrorDrop {
			c.errorf("beforeEnqueue failed, dropping %T messageId=%s - %s", msg, messageIdOf(msg), err)
			return nil, false
		}
		c.errorf("beforeEnqueue failed, sending the original %T messageId=%s - %s", msg, messageIdOf(msg), err)
		return msg, true
	}
	if out == nil {
		c.debugf("beforeEnqueue dropped %T messageId=%s", msg, messageIdOf(msg))
		return nil, false
	}
	return out, true
}

func (c *client) runBeforeEnqueue(msg Message) (out Message, err error) {
	returned := false
	defer func() {
		if r := recover(); !returned {
			// Type only: the panic value can quote the event.
			// Before Go 1.21, recover returns nil for panic(nil).
			out, err = nil, fmt.Errorf("panic: %T", r)
		}
	}()
	out, err = c.prepareBeforeEnqueue(msg)
	returned = true
	return
}

func (c *client) prepareBeforeEnqueue(msg Message) (Message, error) {
	cp, err := deepCopyMessage(msg)
	if err != nil {
		return nil, err
	}
	out := dereferenceMessage(c.BeforeEnqueue(cp))
	if out == nil {
		return nil, nil
	}
	if reflect.TypeOf(out) != reflect.TypeOf(msg) {
		return nil, errors.New("returned a different message type")
	}

	switch m := out.(type) {
	case Alias:
		o := msg.(Alias)
		err = c.restoreReserved(m, reservedOf(&o), reservedOf(&m))
		out = m
	case Group:
		o := msg.(Group)
		err = c.restoreReserved(m, reservedOf(&o), reservedOf(&m))
		out = m
	case Identify:
		o := msg.(Identify)
		err = c.restoreReserved(m, reservedOf(&o), reservedOf(&m))
		out = m
	case Page:
		o := msg.(Page)
		err = c.restoreReserved(m, reservedOf(&o), reservedOf(&m))
		out = m
	case Screen:
		o := msg.(Screen)
		err = c.restoreReserved(m, reservedOf(&o), reservedOf(&m))
		out = m
	case Track:
		o := msg.(Track)
		err = c.restoreReserved(m, reservedOf(&o), reservedOf(&m))
		out = m
	}
	if err != nil {
		return nil, err
	}
	if err = out.Validate(); err != nil {
		return nil, err
	}

	// Oversize is a hook error so the policy, not push, decides the outcome.
	// Context.MarshalJSON starts a fresh encoder, so encoding/json alone
	// cannot detect cycles that cross Context boundaries.
	if hasCycle(reflect.ValueOf(out), map[copyKey]uint8{}) {
		return nil, errors.New("hook output contains a cycle")
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, errors.New(marshalFailure(err))
	}
	if len(b) > c.MaxMessageBytes {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrMessageTooBig, len(b), c.MaxMessageBytes)
	}
	return out, nil
}

// Pointers to the fields the hook may not change.
type reserved struct {
	typ, messageId, channel *string
	ctx                     **Context
}

func reservedOf(m interface{}) reserved {
	switch v := m.(type) {
	case *Alias:
		return reserved{&v.Type, &v.MessageId, &v.Channel, &v.Context}
	case *Group:
		return reserved{&v.Type, &v.MessageId, &v.Channel, &v.Context}
	case *Identify:
		return reserved{&v.Type, &v.MessageId, &v.Channel, &v.Context}
	case *Page:
		return reserved{&v.Type, &v.MessageId, &v.Channel, &v.Context}
	case *Screen:
		return reserved{&v.Type, &v.MessageId, &v.Channel, &v.Context}
	case *Track:
		return reserved{&v.Type, &v.MessageId, &v.Channel, &v.Context}
	}
	panic(fmt.Sprintf("unsupported message type %T", m))
}

// restoreReserved rejects a changed type and puts back the pre-hook
// messageId, context.library and channel, warning when the hook changed them.
func (c *client) restoreReserved(m Message, pre, out reserved) error {
	if *out.typ != *pre.typ {
		// Fixed text: the replacement Type is application data.
		return errors.New("changed reserved field type")
	}
	if *out.messageId != *pre.messageId {
		c.logf("beforeEnqueue changed reserved field messageId on %T messageId=%s, overwriting it", m, *pre.messageId)
		*out.messageId = *pre.messageId
	}
	lib := (*pre.ctx).Library
	if *out.ctx == nil {
		c.logf("beforeEnqueue changed reserved field context.library on %T messageId=%s, overwriting it", m, *pre.messageId)
		*out.ctx = &Context{Library: lib}
	} else if (*out.ctx).Library != lib {
		c.logf("beforeEnqueue changed reserved field context.library on %T messageId=%s, overwriting it", m, *pre.messageId)
		// Copy: the hook may retain the Context it returned.
		ctx := **out.ctx
		ctx.Library = lib
		*out.ctx = &ctx
	}
	if *out.channel != *pre.channel {
		c.logf("beforeEnqueue changed reserved field channel on %T messageId=%s, overwriting it", m, *pre.messageId)
		*out.channel = *pre.channel
	}
	return nil
}

// marshalFailure names a marshal error by Go type only: a custom MarshalJSON
// can return an error that quotes the event.
func marshalFailure(err error) string {
	if err == ErrMessageTooBig {
		return err.Error()
	}
	return fmt.Sprintf("marshalling the message failed: %T", err)
}

// messageIdOf returns the message id for logs, which must never carry payloads.
func messageIdOf(m Message) string {
	v := reflect.Indirect(reflect.ValueOf(m))
	if v.Kind() == reflect.Struct {
		if f := v.FieldByName("MessageId"); f.Kind() == reflect.String {
			return f.String()
		}
	}
	return ""
}

// deepCopyMessage gives the hook its own copy, so a hook that mutates and then
// fails cannot alter the original kept for the send fallback. Values reachable
// only through unexported struct fields (user-defined types) stay shared.
// Channels, funcs and unsafe pointers cannot be copied and are an error.
func deepCopyMessage(m Message) (Message, error) {
	v, err := deepCopyValue(reflect.ValueOf(m), map[copyKey]reflect.Value{})
	if err != nil {
		return nil, err
	}
	return v.Interface().(Message), nil
}

// copyKey identifies an already-copied pointer, map or slice so shared and
// cyclic references are copied once instead of recursing forever.
type copyKey struct {
	ptr uintptr
	len int
	typ reflect.Type
}

// hasCycle tracks active and completed references separately, allowing shared
// acyclic data. Inspect exported fields, including map keys, without invoking
// application serializers.
func hasCycle(v reflect.Value, seen map[copyKey]uint8) bool {
	switch v.Kind() {
	case reflect.Interface:
		return !v.IsNil() && hasCycle(v.Elem(), seen)
	case reflect.Ptr, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return false
		}
		k := copyKey{ptr: v.Pointer(), typ: v.Type()}
		if v.Kind() == reflect.Slice {
			k.len = v.Len()
		}
		if state := seen[k]; state != 0 {
			return state == 1
		}
		seen[k] = 1
		defer func() { seen[k] = 2 }()
	}
	switch v.Kind() {
	case reflect.Ptr:
		return hasCycle(v.Elem(), seen)
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			if hasCycle(it.Key(), seen) || hasCycle(it.Value(), seen) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if hasCycle(v.Index(i), seen) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath == "" && hasCycle(v.Field(i), seen) {
				return true
			}
		}
	}
	return false
}

func deepCopyValue(src reflect.Value, seen map[copyKey]reflect.Value) (reflect.Value, error) {
	switch src.Kind() {
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return src, fmt.Errorf("cannot copy %s value for the hook", src.Kind())

	case reflect.Ptr:
		if src.IsNil() {
			return src, nil
		}
		k := copyKey{src.Pointer(), 0, src.Type()}
		if dst, ok := seen[k]; ok {
			return dst, nil
		}
		dst := reflect.New(src.Type().Elem())
		seen[k] = dst
		e, err := deepCopyValue(src.Elem(), seen)
		if err != nil {
			return src, err
		}
		dst.Elem().Set(e)
		return dst, nil

	case reflect.Map:
		if src.IsNil() {
			return src, nil
		}
		k := copyKey{src.Pointer(), 0, src.Type()}
		if dst, ok := seen[k]; ok {
			return dst, nil
		}
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		seen[k] = dst
		for it := src.MapRange(); it.Next(); {
			key, err := deepCopyValue(it.Key(), seen)
			if err != nil {
				return src, err
			}
			e, err := deepCopyValue(it.Value(), seen)
			if err != nil {
				return src, err
			}
			dst.SetMapIndex(key, e)
		}
		return dst, nil

	case reflect.Slice:
		if src.IsNil() {
			return src, nil
		}
		k := copyKey{src.Pointer(), src.Len(), src.Type()}
		if dst, ok := seen[k]; ok {
			return dst, nil
		}
		dst := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		seen[k] = dst
		for i := 0; i < src.Len(); i++ {
			e, err := deepCopyValue(src.Index(i), seen)
			if err != nil {
				return src, err
			}
			dst.Index(i).Set(e)
		}
		return dst, nil

	case reflect.Array:
		dst := reflect.New(src.Type()).Elem()
		for i := 0; i < src.Len(); i++ {
			e, err := deepCopyValue(src.Index(i), seen)
			if err != nil {
				return src, err
			}
			dst.Index(i).Set(e)
		}
		return dst, nil

	case reflect.Interface:
		if src.IsNil() {
			return src, nil
		}
		dst := reflect.New(src.Type()).Elem()
		e, err := deepCopyValue(src.Elem(), seen)
		if err != nil {
			return src, err
		}
		dst.Set(e)
		return dst, nil

	case reflect.Struct:
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src) // keeps unexported fields, e.g. time.Time internals
		for i := 0; i < src.NumField(); i++ {
			if !dst.Field(i).CanSet() {
				continue
			}
			e, err := deepCopyValue(src.Field(i), seen)
			if err != nil {
				return src, err
			}
			dst.Field(i).Set(e)
		}
		return dst, nil
	}
	return src, nil
}
