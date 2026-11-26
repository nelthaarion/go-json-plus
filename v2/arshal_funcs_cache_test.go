package json_test

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/nelthaarion/go-json-plus/jsontext"
	json "github.com/nelthaarion/go-json-plus/v2"
)

type marshalFuncsCacheValue struct {
	Value string `json:"value"`
}

// TestMarshalFuncsCacheUsesCurrentFunctions verifies that the shared opcode shape of functions with the same
// signature calls the function of the current call, not the one which compiled the shape first.
func TestMarshalFuncsCacheUsesCurrentFunctions(t *testing.T) {
	type payload struct {
		Value string `json:"value"`
	}
	tests := []struct {
		name string
		fn   func(string) ([]byte, error)
		want string
	}{
		{"first", func(string) ([]byte, error) { return []byte(`"first"`), nil }, `{"value":"first"}`},
		{"second", func(string) ([]byte, error) { return []byte(`"second"`), nil }, `{"value":"second"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(payload{Value: "original"}, json.WithMarshalers(json.MarshalFunc(tt.fn)))
			if err != nil || string(got) != tt.want {
				t.Fatalf("got %s, %v, want %s", got, err, tt.want)
			}
		})
	}
}

// TestMarshalFuncsCacheSeparatesKinds verifies that a function which returns bytes and a function which writes to an
// encoder do not share an opcode shape, even when their argument type is the same.
func TestMarshalFuncsCacheSeparatesKinds(t *testing.T) {
	type payload struct {
		Value string `json:"value"`
	}
	got, err := json.Marshal(payload{Value: "original"}, json.WithMarshalers(
		json.MarshalFunc(func(string) ([]byte, error) { return []byte(`"bytes"`), nil }),
	))
	if err != nil || string(got) != `{"value":"bytes"}` {
		t.Fatalf("MarshalFunc: got %s, %v", got, err)
	}
	got, err = json.Marshal(payload{Value: "original"}, json.WithMarshalers(
		json.MarshalToFunc(func(enc *jsontext.Encoder, _ string) error {
			return enc.WriteToken(jsontext.String("encoder"))
		}),
	))
	if err != nil || string(got) != `{"value":"encoder"}` {
		t.Fatalf("MarshalToFunc: got %s, %v", got, err)
	}
}

// TestMarshalFuncsCacheKeepsPrecedence verifies that the order of the functions is part of the cached shape.
func TestMarshalFuncsCacheKeepsPrecedence(t *testing.T) {
	type payload struct {
		Value string `json:"value"`
	}
	got, err := json.Marshal(payload{Value: "original"}, json.WithMarshalers(json.JoinMarshalers(
		json.MarshalFunc(func(string) ([]byte, error) { return []byte(`"first"`), nil }),
		json.MarshalFunc(func(string) ([]byte, error) { return []byte(`"second"`), nil }),
	)))
	if err != nil || string(got) != `{"value":"first"}` {
		t.Fatalf("got %s, %v", got, err)
	}
}

// TestMarshalFuncsCacheConcurrentCalls verifies that calls which use different implementations of the same shape stay
// isolated when they run concurrently.
func TestMarshalFuncsCacheConcurrentCalls(t *testing.T) {
	type payload struct {
		Value string `json:"value"`
	}
	const goroutines = 32
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value := `"first"`
			want := `{"value":"first"}`
			if i%2 != 0 {
				value = `"second"`
				want = `{"value":"second"}`
			}
			fn := func(string) ([]byte, error) { return []byte(value), nil }
			got, err := json.Marshal(payload{Value: "original"}, json.WithMarshalers(json.MarshalFunc(fn)))
			if err != nil {
				errs <- err
				return
			}
			if string(got) != want {
				errs <- fmt.Errorf("marshaled value belongs to another call: got %s, want %s", got, want)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// TestMarshalFuncsCacheAllocations verifies that a Marshalers value built for every call does not compile the
// opcodes again. The allocations are the function, its Marshalers value, the option, and the output.
func TestMarshalFuncsCacheAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("the runtime context is dropped from its pool at random")
	}
	type payload struct {
		Value string `json:"value"`
	}
	newMarshalers := func() *json.Marshalers {
		return json.MarshalFunc(func(string) ([]byte, error) { return []byte(`"custom"`), nil })
	}
	// Compile the shape before measuring the steady-state path.
	if _, err := json.Marshal(payload{}, json.WithMarshalers(newMarshalers())); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		_, _ = json.Marshal(payload{}, json.WithMarshalers(newMarshalers()))
	})
	if allocs > 8 {
		t.Fatalf("Marshal with a per-call Marshalers value: %v allocations", allocs)
	}
}

// TestMarshalFuncsCacheDoesNotKeepFunctions verifies that the global cache of the shapes of functions keeps no
// function, closure, or value captured by one.
func TestMarshalFuncsCacheDoesNotKeepFunctions(t *testing.T) {
	collected := make(chan struct{}, 1)
	func() {
		captured := new([64]byte)
		runtime.SetFinalizer(captured, func(*[64]byte) { collected <- struct{}{} })
		for range 10 {
			_, err := json.Marshal(marshalFuncsCacheValue{Value: "original"}, json.WithMarshalers(
				json.MarshalFunc(func(string) ([]byte, error) {
					_ = captured
					return []byte(`"custom"`), nil
				}),
			))
			if err != nil {
				t.Fatal(err)
			}
		}
	}()
	runtime.GC()
	select {
	case <-collected:
	case <-time.After(10 * time.Second):
		t.Fatal("the function cache keeps a captured value alive")
	}
}
