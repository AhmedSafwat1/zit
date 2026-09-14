package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDatabase_DSN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		db   Database
		want string
	}{
		{name: "full", db: Database{Host: "localhost", Port: 5434, User: "identity", Password: "identity", Name: "identity", SSLMode: "disable"},
			want: "postgres://identity:identity@localhost:5434/identity?sslmode=disable"},
		{name: "password with special characters is escaped", db: Database{Host: "db", Port: 5432, User: "u", Password: "p@ss:w/rd", Name: "n", SSLMode: "require"},
			want: "postgres://u:p%40ss%3Aw%2Frd@db:5432/n?sslmode=require"},
		{name: "no password", db: Database{Host: "db", Port: 5432, User: "u", Name: "n", SSLMode: "disable"},
			want: "postgres://u@db:5432/n?sslmode=disable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.db.DSN())
		})
	}
}

func TestDatabase_Enabled(t *testing.T) {
	t.Parallel()
	assert.False(t, Database{}.Enabled())
	assert.True(t, Database{Host: "x"}.Enabled())
}
