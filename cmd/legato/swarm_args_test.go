package main

import "testing"

func TestParseMessageArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantID  string
		wantTxt string
		wantUrg bool
		wantErr bool
	}{
		{
			name:    "text before urgent",
			args:    []string{"st-123", "hello", "--urgent"},
			wantID:  "st-123",
			wantTxt: "hello",
			wantUrg: true,
		},
		{
			name:    "urgent before text",
			args:    []string{"st-123", "--urgent", "hello"},
			wantID:  "st-123",
			wantTxt: "hello",
			wantUrg: true,
		},
		{
			name:    "no urgent",
			args:    []string{"st-123", "hello"},
			wantID:  "st-123",
			wantTxt: "hello",
			wantUrg: false,
		},
		{
			name:    "multiword text",
			args:    []string{"st-123", "hello", "world"},
			wantID:  "st-123",
			wantTxt: "hello world",
			wantUrg: false,
		},
		{
			name:    "too few args",
			args:    []string{"st-123"},
			wantErr: true,
		},
		{
			name:    "urgent only text",
			args:    []string{"st-123", "--urgent"},
			wantErr: true,
		},
		{
			name:    "duplicate urgent flags",
			args:    []string{"st-123", "--urgent", "--urgent", "hello"},
			wantID:  "st-123",
			wantTxt: "hello",
			wantUrg: true,
		},
		{
			name:    "text contains urgent literal",
			args:    []string{"st-123", "use", "--urgent", "flag"},
			wantID:  "st-123",
			wantTxt: "use flag",
			wantUrg: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotTxt, gotUrg, err := parseMessageArgs(tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseMessageArgs(%v) error = %v, wantErr %v", tc.args, err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if gotID != tc.wantID {
				t.Errorf("id = %q, want %q", gotID, tc.wantID)
			}
			if gotTxt != tc.wantTxt {
				t.Errorf("text = %q, want %q", gotTxt, tc.wantTxt)
			}
			if gotUrg != tc.wantUrg {
				t.Errorf("urgent = %v, want %v", gotUrg, tc.wantUrg)
			}
		})
	}
}
