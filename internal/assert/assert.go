// Package assert provides the small testify-style subset of test helpers this
// module's tests need, built on the standard library only.
//
// Helpers whose result guards a later dereference or index (NoError, Len,
// NotNil, Contains, ErrorAs) fail the test with t.Fatalf; the others report
// with t.Errorf. Every helper accepts an optional trailing message, formatted
// like testify: a single string, or a format string followed by arguments.
package assert

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"encoding/json/v2"
)

// Equal fails the test when got and want are not deeply equal.
func Equal(t *testing.T, got, want any, msgAndArgs ...any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v%s", got, want, message(msgAndArgs))
	}
}

// NotEqual fails the test when got and want are equal.
func NotEqual[T comparable](t *testing.T, got, want T, msgAndArgs ...any) {
	t.Helper()
	if got == want {
		t.Errorf("got and want are both %#v%s", got, message(msgAndArgs))
	}
}

// NoError fails the test when err is not nil.
func NoError(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v%s", err, message(msgAndArgs))
	}
}

// Error fails the test when err is nil.
func Error(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	if err == nil {
		t.Errorf("expected an error, got nil%s", message(msgAndArgs))
	}
}

// ErrorAs fails the test when err does not match target per errors.As.
func ErrorAs(t *testing.T, err error, target any, msgAndArgs ...any) {
	t.Helper()
	if !errors.As(err, target) {
		t.Fatalf("error %v does not match %T%s", err, target, message(msgAndArgs))
	}
}

// Len fails the test when v does not have the given length.
func Len(t *testing.T, v any, n int, msgAndArgs ...any) {
	t.Helper()
	if got := reflect.ValueOf(v).Len(); got != n {
		t.Fatalf("length is %d, want %d%s", got, n, message(msgAndArgs))
	}
}

// Empty fails the test when v is not empty (nil, "", empty slice/map, 0,
// false, ...).
func Empty(t *testing.T, v any, msgAndArgs ...any) {
	t.Helper()
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return // a nil interface is empty
	}
	var empty bool
	switch rv.Kind() {
	case reflect.Slice, reflect.Map, reflect.String, reflect.Array, reflect.Chan:
		empty = rv.Len() == 0
	default:
		empty = rv.IsZero()
	}
	if !empty {
		t.Errorf("expected empty, got %#v%s", v, message(msgAndArgs))
	}
}

// Nil fails the test when v is not nil.
func Nil(t *testing.T, v any, msgAndArgs ...any) {
	t.Helper()
	if !isNil(v) {
		t.Errorf("expected nil, got %#v%s", v, message(msgAndArgs))
	}
}

// NotNil fails the test when v is nil.
func NotNil(t *testing.T, v any, msgAndArgs ...any) {
	t.Helper()
	if isNil(v) {
		t.Fatalf("unexpected nil%s", message(msgAndArgs))
	}
}

// True fails the test when cond is false.
func True(t *testing.T, cond bool, msgAndArgs ...any) {
	t.Helper()
	if !cond {
		t.Errorf("expected true%s", message(msgAndArgs))
	}
}

// False fails the test when cond is true.
func False(t *testing.T, cond bool, msgAndArgs ...any) {
	t.Helper()
	if cond {
		t.Errorf("expected false%s", message(msgAndArgs))
	}
}

// InDelta fails the test when got and want differ by more than delta.
func InDelta(t *testing.T, got, want, delta float64, msgAndArgs ...any) {
	t.Helper()
	if d := math.Abs(got - want); d > delta {
		t.Errorf("got %v, want %v, |difference| %v exceeds delta %v%s", got, want, d, delta, message(msgAndArgs))
	}
}

// Contains fails the test when container (a string, map, or slice) does not
// contain element (a substring, key, or value).
func Contains(t *testing.T, container, element any, msgAndArgs ...any) {
	t.Helper()
	if !contains(container, element) {
		t.Fatalf("%#v does not contain %#v%s", container, element, message(msgAndArgs))
	}
}

// NotContains fails the test when s contains substr.
func NotContains(t *testing.T, s, substr string, msgAndArgs ...any) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("%q contains %q%s", s, substr, message(msgAndArgs))
	}
}

// Same fails the test when got and want are not the same pointer.
func Same[T comparable](t *testing.T, got, want T, msgAndArgs ...any) {
	t.Helper()
	if got != want {
		t.Errorf("got and want are not the same pointer%s", message(msgAndArgs))
	}
}

// WithinDuration fails the test when got and want differ by more than
// tolerance.
func WithinDuration(t *testing.T, got, want time.Time, tolerance time.Duration, msgAndArgs ...any) {
	t.Helper()
	if d := got.Sub(want); d < -tolerance || d > tolerance {
		t.Errorf("got %v, want %v, |difference| %v exceeds tolerance %v%s", got, want, d, tolerance, message(msgAndArgs))
	}
}

// JSONEq fails the test when got and want are not the same JSON value.
func JSONEq(t *testing.T, got, want string, msgAndArgs ...any) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("decode got: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("decode want: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("JSON not equal:\n got: %s\nwant: %s%s", got, want, message(msgAndArgs))
	}
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return rv.IsZero()
	}
}

func contains(container, element any) bool {
	if s, ok := container.(string); ok {
		return strings.Contains(s, fmt.Sprint(element))
	}
	switch rv := reflect.ValueOf(container); rv.Kind() {
	case reflect.Map:
		return rv.MapIndex(reflect.ValueOf(element)).IsValid()
	case reflect.Slice, reflect.Array:
		for i := range rv.Len() {
			if reflect.DeepEqual(rv.Index(i).Interface(), element) {
				return true
			}
		}
	}
	return false
}

func message(msgAndArgs []any) string {
	switch {
	case len(msgAndArgs) == 0:
		return ""
	case len(msgAndArgs) == 1:
		return ": " + fmt.Sprint(msgAndArgs[0])
	default:
		if format, ok := msgAndArgs[0].(string); ok {
			return ": " + fmt.Sprintf(format, msgAndArgs[1:]...)
		}
		return ": " + fmt.Sprint(msgAndArgs...)
	}
}
