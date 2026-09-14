package cache

import "testing"

func BenchmarkSHA256Hex(b *testing.B) {
	b.ReportAllocs()
	tok := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIzNTU2NDYyNTUyMzUzMzgwNDIifQ.sig"
	for i := 0; i < b.N; i++ {
		_ = SHA256Hex(tok)
	}
}
