package login

import (
	"encoding/base64"
	"testing"
)

func TestNormaliseSkinResourcePatch(t *testing.T) {
	t.Parallel()

	valid := base64.StdEncoding.EncodeToString([]byte(`{"geometry":{"default":"geometry.test"}}`))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "valid JSON", input: valid, want: valid},
		{name: "truncated JSON", input: base64.StdEncoding.EncodeToString(nil), want: defaultSkinResourcePatch},
		{name: "invalid base64", input: "%%%", want: "%%%"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data := ClientData{SkinResourcePatch: test.input}
			normaliseSkinResourcePatch(&data)
			if data.SkinResourcePatch != test.want {
				t.Fatalf("SkinResourcePatch = %q, want %q", data.SkinResourcePatch, test.want)
			}
		})
	}
}
