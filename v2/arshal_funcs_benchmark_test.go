package json

import "testing"

// marshalFuncsBenchmarkPayload has a string field so that the marshal function
// below is selected for a nested value.
type marshalFuncsBenchmarkPayload struct {
	Value string `json:"value"`
}

func BenchmarkMarshalWithMarshalers(b *testing.B) {
	payload := &marshalFuncsBenchmarkPayload{Value: "original"}
	newMarshalers := func() *Marshalers {
		return MarshalFunc(func(string) ([]byte, error) {
			return []byte(`"custom"`), nil
		})
	}

	b.Run("Reused", func(b *testing.B) {
		opt := WithMarshalers(newMarshalers())
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := Marshal(payload, opt); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("PerCall", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := Marshal(payload, WithMarshalers(newMarshalers())); err != nil {
				b.Fatal(err)
			}
		}
	})
}
