package cli

import "testing"

func TestParseOutputFormat(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    OutputFormat
		wantErr bool
	}{
		{name: "prompt", input: "prompt", want: FormatPrompt},
		{name: "brief upper", input: "BRIEF", want: FormatBrief},
		{name: "json", input: "json", want: FormatJSON},
		{name: "proto", input: "proto", want: FormatProto},
		{name: "invalid", input: "xml", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseOutputFormat(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
