package lab

import "testing"

func TestClosureState(t *testing.T) {
	a, b := NewCounter(10), NewCounter(20)
	if a() != 11 || b() != 21 || a() != 12 {
		t.Fatal("counter state must persist and remain independent")
	}
	payload := Payload{7}
	read := NewReader(payload)
	payload[0] = 9
	if got := read(); got != 7 {
		t.Fatalf("array argument must be copied: got %d, want 7", got)
	}
	for _, input := range []int{-7, 0, 42} {
		if Immediate(input) != input+1 || NewAdder(input)() != input+1 {
			t.Fatalf("closure result differs from direct addition for %d", input)
		}
	}
}

var saved func() int
var result int

func BenchmarkDirect(b *testing.B) {
	x := 42
	var value int
	b.ReportAllocs()
	for b.Loop() {
		value = x + 1
	}
	result = value
}

func BenchmarkImmediate(b *testing.B) {
	var value int
	b.ReportAllocs()
	for b.Loop() {
		value = Immediate(42)
	}
	result = value
}

func BenchmarkReturnedLocal(b *testing.B) {
	var value int
	b.ReportAllocs()
	for b.Loop() {
		add := NewAdder(42)
		value = add()
	}
	result = value
}

func BenchmarkSavedValue(b *testing.B) {
	b.Cleanup(func() { saved = nil })
	var value int
	b.ReportAllocs()
	for b.Loop() {
		saved = NewAdder(42)
		value = saved()
	}
	result = value
}

func BenchmarkSavedMutable(b *testing.B) {
	b.Cleanup(func() { saved = nil })
	var value int
	b.ReportAllocs()
	for b.Loop() {
		saved = NewCounter(42)
		value = saved()
	}
	result = value
}

func BenchmarkSavedLarge(b *testing.B) {
	b.Cleanup(func() { saved = nil })
	payload := Payload{43}
	var value int
	b.ReportAllocs()
	for b.Loop() {
		saved = NewReader(payload)
		value = saved()
	}
	result = value
}
