package cardcheck

import "testing"

func TestParseCard(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Card
		wantOK  bool
	}{
		{
			name:   "pipe four fields",
			input:  "4111111111111111|09|26|123",
			want:   Card{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"},
			wantOK: true,
		},
		{
			name:   "pipe expiry with slash",
			input:  "4111111111111111|09/26|123",
			want:   Card{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"},
			wantOK: true,
		},
		{
			name:   "space four fields short year",
			input:  "5500000000000004 11 29 456",
			want:   Card{Number: "5500000000000004", Month: "11", Year: "2029", CVV: "456"},
			wantOK: true,
		},
		{
			name:   "key value style",
			input:  "card=4111111111111111 exp=09/26 cvv=123",
			want:   Card{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"},
			wantOK: true,
		},
		{
			name:   "labeled lines",
			input:  "name:John\ncard:4111111111111111\ndate:09/26\ncvv:123",
			want:   Card{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"},
			wantOK: true,
		},
		{
			name:   "json",
			input:  `{"card_number":"4111111111111111","expiration":"09/26","cvv":"123"}`,
			want:   Card{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"},
			wantOK: true,
		},
		{
			name:   "four digit year",
			input:  "4111111111111111|09|2026|123",
			want:   Card{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"},
			wantOK: true,
		},
		{
			name:   "tab separated",
			input:  "4111111111111111\t09\t26\t123",
			want:   Card{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"},
			wantOK: true,
		},
		{
			name:   "amex four digit cvv",
			input:  "371449635398431|09|26|1234",
			want:   Card{Number: "371449635398431", Month: "09", Year: "2026", CVV: "1234"},
			wantOK: true,
		},
		{
			name:   "missing cvv",
			input:  "4111111111111111|09|26",
			wantOK: false,
		},
		{
			name:   "invalid month",
			input:  "4111111111111111|13|26|123",
			wantOK: false,
		},
		{
			name:   "empty input",
			input:  "",
			wantOK: false,
		},
		{
			name:   "junk text",
			input:  "hello world this is not a card",
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ParseCard(test.input)
			if ok != test.wantOK {
				t.Fatalf("ParseCard(%q) ok = %v, want %v", test.input, ok, test.wantOK)
			}
			if !ok {
				return
			}
			if got != test.want {
				t.Fatalf("ParseCard(%q) = %+v, want %+v", test.input, got, test.want)
			}
		})
	}
}

func TestCardFormat(t *testing.T) {
	card := Card{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"}
	if got := card.Format(); got != "4111111111111111|09|26|123" {
		t.Fatalf("Format() = %q", got)
	}
}

func TestParseVerdict(t *testing.T) {
	tests := []struct {
		verdict string
		want    Status
	}{
		{"Live", StatusLive},
		{"live", StatusLive},
		{"Dead", StatusDead},
		{"Unknown", StatusUnknown},
		{"", ""},
		{"maintenance", ""},
	}
	for _, test := range tests {
		if got := parseVerdict(test.verdict); got != test.want {
			t.Fatalf("parseVerdict(%q) = %q, want %q", test.verdict, got, test.want)
		}
	}
}

func TestMatchResults(t *testing.T) {
	cards := []Card{
		{Number: "4111111111111111", Month: "09", Year: "2026", CVV: "123"},
		{Number: "5500000000000004", Month: "11", Year: "2029", CVV: "456"},
		{Number: "371449635398431", Month: "01", Year: "2027", CVV: "1234"},
	}

	// 完整卡号 + 掩码（前4后4）+ 未匹配兜底顺序。
	items := []ResultItem{
		{Card: "5500000000000004", Verdict: "Dead", Raw: "x"},
		{Card: "4111****1111", Verdict: "Live", Raw: "y"},
		{Card: "junk", Verdict: "Unknown", Raw: "z"},
	}
	got := matchResults(items, cards)
	want := []Result{
		{Card: cards[0], Status: StatusLive, Raw: "y"},
		{Card: cards[1], Status: StatusDead, Raw: "x"},
		{Card: cards[2], Status: StatusUnknown, Raw: "z"},
	}
	if len(got) != len(want) {
		t.Fatalf("matchResults len = %d, want %d: %+v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("matchResults[%d] = %+v, want %+v", index, got[index], want[index])
		}
	}
}

func TestMergeItemsDeduplicates(t *testing.T) {
	current := []ResultItem{{Card: "4111****1111", Verdict: "Live"}}
	incoming := []ResultItem{
		{Card: "4111****1111", Verdict: "Live"},
		{Card: "5500****0004", Verdict: "Dead"},
	}
	got := mergeItems(current, incoming)
	if len(got) != 2 {
		t.Fatalf("mergeItems len = %d, want 2: %+v", len(got), got)
	}
}
