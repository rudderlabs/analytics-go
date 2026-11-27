package analytics

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// BeforeEnqueueErrorPolicy selects what happens when the hook panics or
// returns an invalid message.
type BeforeEnqueueErrorPolicy int

const (
	// BeforeEnqueueOnErrorSend validates and sends the original event. Default.
	BeforeEnqueueOnErrorSend BeforeEnqueueErrorPolicy = iota

	// BeforeEnqueueOnErrorDrop drops the event on a hook error.
	BeforeEnqueueOnErrorDrop
)

// beforeEnqueue returns the message to queue, or nil to drop it.
func (c *client) beforeEnqueue(msg, original Message) (Message, error) {
	out, err := c.runBeforeEnqueue(msg)
	if err != nil {
		v := reflect.ValueOf(msg)
		typ, id := v.FieldByName("Type").String(), v.FieldByName("MessageId").String()
		if c.BeforeEnqueueOnError == BeforeEnqueueOnErrorDrop {
			c.errorf("beforeEnqueue failed for %s event %s, dropping the event. %v", typ, id, err)
			return nil, nil
		}
		c.errorf("beforeEnqueue failed for %s event %s, sending the original event. %v", typ, id, err)
		if err := original.Validate(); err != nil {
			return nil, err
		}
		return msg, c.validateBeforeEnqueue(msg, false)
	}
	if out == nil {
		v := reflect.ValueOf(msg)
		c.debugf("beforeEnqueue dropped %s event %s", v.FieldByName("Type").String(), v.FieldByName("MessageId").String())
	}
	return out, nil
}

func (c *client) runBeforeEnqueue(msg Message) (out Message, err error) {
	// Go < 1.21 recovers panic(nil) as nil, so track completion separately
	completed := false
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("%v", r)
		} else if !completed {
			out, err = nil, fmt.Errorf("beforeEnqueue panicked with nil")
		}
	}()
	out = dereferenceMessage(c.BeforeEnqueue(deepCopyValue(reflect.ValueOf(msg)).Interface().(Message)))
	if out == nil {
		completed = true
		return nil, nil
	}
	// The public Message signature excludes primitive results at compile time.
	// Custom Message implementations cannot be sent by the SDK.
	var msgType string
	switch out.(type) {
	case Alias:
		msgType = "alias"
	case Group:
		msgType = "group"
	case Identify:
		msgType = "identify"
	case Page:
		msgType = "page"
	case Screen:
		msgType = "screen"
	case Track:
		msgType = "track"
	default:
		err = fmt.Errorf("beforeEnqueue must return an SDK message or nil, got %T", out)
		completed = true
		return nil, err
	}
	if err = c.validateBeforeEnqueue(out, true); err != nil {
		completed = true
		return nil, err
	}

	pre := reflect.ValueOf(msg)
	v := reflect.New(reflect.TypeOf(out)).Elem()
	v.Set(reflect.ValueOf(out))
	ctx := v.FieldByName("Context").Interface().(*Context)
	library := pre.FieldByName("Context").Interface().(*Context).Library
	channel := pre.FieldByName("Channel").String()
	if v.FieldByName("Channel").String() != channel || ctx == nil || ctx.Library != library {
		c.logf("beforeEnqueue changed channel or context.library; they were restored.")
	}
	v.FieldByName("Channel").SetString(channel)
	// As in Enqueue, the message's Go type decides its type field
	v.FieldByName("Type").SetString(msgType)
	if ctx == nil {
		ctx = &Context{}
		v.FieldByName("Context").Set(reflect.ValueOf(ctx))
	}
	ctx.Library = library
	if v.FieldByName("MessageId").String() == "" {
		v.FieldByName("MessageId").Set(pre.FieldByName("MessageId"))
	}
	out = v.Interface().(Message)
	completed = true
	return out, nil
}

// Use the normal field validation and serialization. Oversize remains the
// queue's existing ErrMessageTooBig handling, rather than a hook error.
func (c *client) validateBeforeEnqueue(msg Message, hookResult bool) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	serialized, err := makeMessage(msg, c.MaxMessageBytes)
	// encoding/json can swallow panic(nil) and return incomplete JSON.
	if hookResult && (err == nil || err == ErrMessageTooBig) && !json.Valid(serialized.json) {
		return fmt.Errorf("beforeEnqueue result serialization did not complete")
	}
	if err == ErrMessageTooBig {
		return nil
	}
	return err
}

type copiedReference struct {
	typ    reflect.Type
	ptr    uintptr
	length int
}

type valueCopies map[copiedReference]reflect.Value

// Copy the enriched input without serialization. Unexported struct fields
// retain their normal value semantics.
func deepCopyValue(src reflect.Value) reflect.Value {
	return make(valueCopies).copyValue(src)
}

func (copies valueCopies) copyValue(src reflect.Value) reflect.Value {
	var ref copiedReference
	switch src.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice:
		if src.IsNil() {
			return src
		}
		ref = copiedReference{typ: src.Type(), ptr: src.Pointer()}
		if src.Kind() == reflect.Ptr {
			// Named and unnamed pointers to the same type can share a copy.
			ref.typ = reflect.PointerTo(src.Type().Elem())
		}
		if src.Kind() == reflect.Slice {
			ref.length = src.Len()
		}
		if dst, ok := copies[ref]; ok {
			return dst.Convert(src.Type())
		}
	}

	switch src.Kind() {
	case reflect.Ptr:
		dst := reflect.New(src.Type().Elem()).Convert(src.Type())
		copies[ref] = dst
		dst.Elem().Set(copies.copyValue(src.Elem()))
		return dst
	case reflect.Interface:
		if src.IsNil() {
			return src
		}
		dst := reflect.New(src.Type()).Elem()
		dst.Set(copies.copyValue(src.Elem()))
		return dst
	case reflect.Map:
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		copies[ref] = dst
		for it := src.MapRange(); it.Next(); {
			dst.SetMapIndex(copies.copyValue(it.Key()), copies.copyValue(it.Value()))
		}
		return dst
	case reflect.Slice, reflect.Array:
		var dst reflect.Value
		if src.Kind() == reflect.Slice {
			dst = reflect.MakeSlice(src.Type(), src.Len(), src.Len())
			copies[ref] = dst
		} else {
			dst = reflect.New(src.Type()).Elem()
		}
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(copies.copyValue(src.Index(i)))
		}
		return dst
	case reflect.Struct:
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src)
		for i := 0; i < src.NumField(); i++ {
			if src.Type().Field(i).PkgPath == "" {
				dst.Field(i).Set(copies.copyValue(src.Field(i)))
			}
		}
		return dst
	default:
		return src
	}
}
