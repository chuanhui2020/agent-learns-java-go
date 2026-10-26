package exercises

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestMakeCounters(t *testing.T) {
	for _, n := range []int{-1, 0, 1, 3} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			counters := MakeCounters(n)
			if len(counters) != max(n, 0) {
				t.Fatalf("got %d counters, want %d", len(counters), max(n, 0))
			}
			for i, next := range counters {
				if next == nil {
					t.Fatalf("counter %d is nil", i)
				}
				for want := 1; want <= i+2; want++ {
					if got := next(); got != want {
						t.Fatalf("counter %d: got %d, want %d", i, got, want)
					}
				}
			}
			for i := len(counters) - 1; i >= 0; i-- {
				if got := counters[i](); got != i+3 {
					t.Errorf("counter %d lost independent state: got %d, want %d", i, got, i+3)
				}
			}
		})
	}
}

func TestCaptureMethods(t *testing.T) {
	c := &Counter{N: 10}
	snapshot, live := CaptureMethods(c)
	if snapshot == nil || live == nil {
		t.Fatal("both method values must be non-nil")
	}
	for _, n := range []int{20, -5, 0} {
		c.N = n
		if snapshot() != 10 || live() != n {
			t.Fatalf("after N=%d: snapshot=%d, live=%d", n, snapshot(), live())
		}
	}
}

func TestConfigure(t *testing.T) {
	name, limit := WithName("first"), WithLimit(7)
	if name == nil || limit == nil {
		t.Fatal("WithName and WithLimit must return callable options")
	}
	for _, tc := range []struct {
		name    string
		options []Option
		want    Config
	}{
		{"zero", nil, Config{}},
		{"values", []Option{name, limit}, Config{"first", 7}},
		{"last wins", []Option{name, limit, WithName("last"), WithLimit(0)}, Config{"last", 0}},
		{"nil option", []Option{nil, name, nil}, Config{Name: "first"}},
		{"reuse", []Option{name, limit}, Config{"first", 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Configure(tc.options...); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	var calls []int
	Configure(func(*Config) { calls = append(calls, 1) }, func(*Config) { calls = append(calls, 2) })
	if !reflect.DeepEqual(calls, []int{1, 2}) {
		t.Fatalf("options must run once in caller order: %v", calls)
	}
}

func TestLoadPositive(t *testing.T) {
	failure := errors.New("load failed")
	for _, tc := range []struct {
		name  string
		value int
		err   error
		want  int
	}{
		{"positive", 7, nil, 7},
		{"zero", 0, nil, 0},
		{"negative", -1, nil, 0},
		{"error", 0, failure, 0},
		{"value with error", 9, failure, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			value, err := LoadPositive(func() (int, error) {
				calls++
				return tc.value, tc.err
			})
			if calls != 1 || value != tc.want || err != tc.err {
				t.Fatalf("calls=%d result=(%d, %v), want (1, %d, %v)", calls, value, err, tc.want, tc.err)
			}
		})
	}
}
