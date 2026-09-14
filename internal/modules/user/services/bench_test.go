package services

import (
	"strings"
	"testing"
)

func BenchmarkValidateToken(b *testing.B) {
	tokens := map[string]string{
		"jwt_1k":  strings.Repeat("eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.", 26),
		"opaque":  "Vz5uQ7Xk1F3p9aB2cD4eF6gH8iJ0kL2mN4oP6qR8sT0uV2wX4yZ6",
		"max_8k":  strings.Repeat("a", 8*1024),
		"invalid": "tok<script>",
	}
	for name, tok := range tokens {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ValidateToken(tok)
			}
		})
	}
}
