package provider

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDeleteNullValueFields(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		fields []string
		want   string
	}{
		{
			name:   "delete null field",
			input:  `{"a":null,"b":1}`,
			fields: []string{"a"},
			want:   `{"b":1}`,
		},
		{
			name:   "field not null",
			input:  `{"a":1,"b":2}`,
			fields: []string{"a"},
			want:   `{"a":1,"b":2}`,
		},
		{
			name:   "field not exist",
			input:  `{"a":1}`,
			fields: []string{"b"},
			want:   `{"a":1}`,
		},
		{
			name:   "multiple fields",
			input:  `{"a":null,"b":null,"c":3}`,
			fields: []string{"a", "b"},
			want:   `{"c":3}`,
		},
		{
			name:   "null value not in fields",
			input:  `{"a":null,"b":1,"c":null}`,
			fields: []string{"b", "c"},
			want:   `{"a":null,"b":1}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := deleteNullValueFields([]byte(c.input), c.fields)
			if err != nil {
				t.Errorf("%s: unexpected error: %v", c.name, err)
			}
			var gotMap, wantMap map[string]interface{}
			if err := json.Unmarshal(got, &gotMap); err != nil {
				t.Errorf("%s: failed to unmarshal got: %v", c.name, err)
			}
			if err := json.Unmarshal([]byte(c.want), &wantMap); err != nil {
				t.Errorf("%s: failed to unmarshal want: %v", c.name, err)
			}
			if !reflect.DeepEqual(gotMap, wantMap) {
				t.Errorf("%s: got %s, want %s", c.name, got, c.want)
			}
		})
	}
}

func TestExpandExtraBodyField(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no extra_body",
			input: `{"a":1}`,
			want:  `{"a":1}`,
		},
		{
			name:  "extra_body not object",
			input: `{"a":1,"extra_body":123}`,
			want:  `{"a":1,"extra_body":123}`,
		},
		{
			name:  "expand extra_body",
			input: `{"a":1,"extra_body":{"b":2,"c":3}}`,
			want:  `{"a":1,"b":2,"c":3}`,
		},
		{
			name:  "extra_body overwrite",
			input: `{"a":1,"b":9,"extra_body":{"b":2,"c":3}}`,
			want:  `{"a":1,"b":2,"c":3}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandExtraBodyField([]byte(c.input))
			var gotMap, wantMap map[string]interface{}
			if err := json.Unmarshal(got, &gotMap); err != nil {
				t.Errorf("%s: failed to unmarshal got: %v", c.name, err)
			}
			if err := json.Unmarshal([]byte(c.want), &wantMap); err != nil {
				t.Errorf("%s: failed to unmarshal want: %v", c.name, err)
			}
			if !reflect.DeepEqual(gotMap, wantMap) {
				t.Errorf("%s: got %s, want %s", c.name, got, c.want)
			}
		})
	}
}
