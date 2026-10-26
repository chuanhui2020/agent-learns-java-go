// Package exercises contains unfinished Ch04 drills. Implement TODOs; keep the
// tests unchanged. Initially go test ./ch04-functions-closures/go/exercises fails.
package exercises

// MakeCounters returns n independent counters, each starting at 0 and returning
// 1 on its first call. n <= 0 returns an empty slice (nil is allowed).
// The returned functions need not support concurrent calls.
func MakeCounters(n int) []func() int {
	// TODO: create independent state per counter.
	return nil
}

type Counter struct{ N int }

func (c Counter) Value() int    { return c.N }
func (c *Counter) Pointer() int { return c.N }

// CaptureMethods saves method values now. snapshot observes the original N;
// live observes subsequent changes to c.N. Precondition: c is non-nil.
func CaptureMethods(c *Counter) (snapshot, live func() int) {
	// TODO: use method values, not hard-coded results.
	return nil, nil
}

// Config's zero value is valid. Options run in caller order; later ones win.
type Config struct {
	Name  string
	Limit int
}

type Option func(*Config)

func WithName(name string) Option {
	// TODO: capture the provided name.
	return nil
}

func WithLimit(limit int) Option {
	// TODO: capture the provided limit; do not add validation to this drill.
	return nil
}

// Configure starts from a zero Config and applies each non-nil option once.
func Configure(options ...Option) Config {
	// TODO: preserve application order and skip nil options.
	return Config{}
}

// LoadPositive calls load exactly once. On failure it returns (0, err), keeping
// the error unchanged. A successful result <= 0 becomes (0, nil). load is non-nil.
func LoadPositive(load func() (int, error)) (value int, err error) {
	// TODO: fix the shadowing bug without changing this contract.
	if value, err := load(); err == nil && value > 0 {
		return value, nil
	}
	return
}
