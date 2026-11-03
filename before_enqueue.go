package analytics

import (
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
func (c *client) beforeEnqueue(msg Message) (Message, error) {
	out, err := c.runBeforeEnqueue(msg)
	if err != nil {
		v := reflect.ValueOf(msg)
		typ, id := v.FieldByName("Type").String(), v.FieldByName("MessageId").String()
		if c.BeforeEnqueueOnError == BeforeEnqueueOnErrorDrop {
			c.errorf("beforeEnqueue failed for %s event %s, dropping the event. %v", typ, id, err)
			return nil, nil
		}
		c.errorf("beforeEnqueue failed for %s event %s, sending the original event. %v", typ, id, err)
		return msg, c.validateBeforeEnqueue(msg)
	}
	if out == nil {
		v := reflect.ValueOf(msg)
		c.debugf("beforeEnqueue dropped %s event %s", v.FieldByName("Type").String(), v.FieldByName("MessageId").String())
	}
	return out, nil
}

func (c *client) runBeforeEnqueue(msg Message) (out Message, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("%v", r)
		}
	}()
	out = dereferenceMessage(c.BeforeEnqueue(deepCopyValue(reflect.ValueOf(msg)).Interface().(Message)))
	if out == nil {
		return nil, nil
	}
	// The public Message signature excludes primitive results at compile time.
	// Custom Message implementations cannot be sent by the SDK.
	switch out.(type) {
	case Alias, Group, Identify, Page, Screen, Track:
	default:
		return nil, fmt.Errorf("beforeEnqueue must return an SDK message or nil, got %T", out)
	}
	if err = c.validateBeforeEnqueue(out); err != nil {
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
	if ctx == nil {
		ctx = &Context{}
		v.FieldByName("Context").Set(reflect.ValueOf(ctx))
	}
	ctx.Library = library
	if v.FieldByName("MessageId").String() == "" {
		v.FieldByName("MessageId").Set(pre.FieldByName("MessageId"))
	}
	return v.Interface().(Message), nil
}

// Use the normal field validation and serialization. Oversize remains the
// queue's existing ErrMessageTooBig handling, rather than a hook error.
func (c *client) validateBeforeEnqueue(msg Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	_, err := makeMessage(msg, c.MaxMessageBytes)
	if err == ErrMessageTooBig {
		return nil
	}
	return err
}

// Copy the enriched input without serialization. Unexported struct fields
// retain their normal value semantics. Like ordinary SDK inputs, the input
// must be acyclic.
func deepCopyValue(src reflect.Value) reflect.Value {
	switch src.Kind() {
	case reflect.Ptr, reflect.Interface:
		if src.IsNil() {
			return src
		}
		var dst reflect.Value
		if src.Kind() == reflect.Ptr {
			dst = reflect.New(src.Type().Elem())
			dst.Elem().Set(deepCopyValue(src.Elem()))
		} else {
			dst = reflect.New(src.Type()).Elem()
			dst.Set(deepCopyValue(src.Elem()))
		}
		return dst
	case reflect.Map:
		if src.IsNil() {
			return src
		}
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		for it := src.MapRange(); it.Next(); {
			dst.SetMapIndex(deepCopyValue(it.Key()), deepCopyValue(it.Value()))
		}
		return dst
	case reflect.Slice, reflect.Array:
		var dst reflect.Value
		if src.Kind() == reflect.Slice {
			if src.IsNil() {
				return src
			}
			dst = reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		} else {
			dst = reflect.New(src.Type()).Elem()
		}
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(deepCopyValue(src.Index(i)))
		}
		return dst
	case reflect.Struct:
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src)
		for i := 0; i < src.NumField(); i++ {
			if src.Type().Field(i).PkgPath == "" {
				dst.Field(i).Set(deepCopyValue(src.Field(i)))
			}
		}
		return dst
	default:
		return src
	}
}
